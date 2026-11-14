import Foundation
import Observation

@MainActor
@Observable
public final class BacklogStore {
    public static let doneLimit = 50

    public let workspace: String
    public let queue: ActionQueue
    private var fetchedTasks: [BacklogTask] = []
    private var settling: [Landed] = []
    public private(set) var loaded = false
    public private(set) var error: String?
    public private(set) var syncedAt: Date?
    public private(set) var fetched = false
    public var newDraft = BacklogDraft()
    public var arrival: Arrival?

    public struct Arrival: Equatable, Sendable {
        public let task: Int
        public let session: String
    }

    @ObservationIgnored private weak var runner: (any CommandRunner)?
    @ObservationIgnored private let cache: ListCache
    @ObservationIgnored private let key: ListCache.Key
    @ObservationIgnored private let computer: String
    @ObservationIgnored private let queues: @MainActor () -> Bool
    @ObservationIgnored private let retry: @MainActor () -> Void
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private let saves = SaveThrottle()
    @ObservationIgnored private var raw: JSONValue = .array([])
    @ObservationIgnored private var inflight = false
    @ObservationIgnored private var again = false

    public init(workspace: String, runner: any CommandRunner, queue: ActionQueue = ActionQueue(file: nil), cache: ListCache = ListCache(dir: nil), computer: String = "", queues: @escaping @MainActor () -> Bool = { false }, retry: @escaping @MainActor () -> Void = {}, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.workspace = workspace
        self.runner = runner
        self.queue = queue
        self.cache = cache
        self.computer = computer
        self.queues = queues
        self.retry = retry
        self.sleep = sleep
        key = ListCache.Key(computer: computer, workspace: workspace, collection: "backlog")
        if let saved = cache.load(key, as: Snapshot.self), let tasks = try? saved.tasks.decode([BacklogTask].self) {
            fetchedTasks = tasks
            raw = saved.tasks
            syncedAt = saved.at
            loaded = true
        }
    }

    public func ageLine(now: Date = .now) -> String? {
        ListCache.ageLine(syncedAt: syncedAt, error: error, empty: tasks.isEmpty, now: now)
    }

    public var tasks: [BacklogTask] {
        let edits = writes.filter { $0.command == BacklogCommand.update }
        guard !edits.isEmpty else { return fetchedTasks }
        return fetchedTasks.map { task in
            let mine = edits.filter { $0.target == Self.target(task.id) }
            guard !mine.isEmpty, let base = raw.arrayValue?.first(where: { $0["id"]?.numberValue == Double(task.id) }) else { return task }
            let merged = mine.reduce(base) { $0.overlaid($1.args) }
            return (try? merged.decode(BacklogTask.self)) ?? task
        }
    }

    public var outbox: [QueuedAction] {
        queue.items.filter { $0.computer == computer && $0.workspace == workspace && $0.kind == .command && $0.command.hasPrefix(Self.prefix) }
    }

    public func queued(_ id: Int) -> [QueuedAction] {
        outbox.filter { $0.target == Self.target(id) }
    }

    public func landed(_ landed: Landed) {
        guard landed.item.workspace == workspace, landed.item.command.hasPrefix(Self.prefix) else { return }
        settling.append(landed)
        if landed.item.command == BacklogCommand.start, let id = landed.item.args?["id"]?.numberValue, let session = landed.result["session"]?.stringValue {
            arrival = Arrival(task: Int(id), session: session)
        }
    }

    private static let prefix = "backlog."

    private var writes: [QueuedAction] {
        outbox.filter { $0.failure == nil } + settling.map(\.item)
    }

    static func target(_ id: Int) -> String {
        "task/\(id)"
    }

    public var groups: [(status: BacklogStatus, tasks: [BacklogTask])] {
        BacklogTask.grouped(tasks)
    }

    public var inProgressCount: Int {
        tasks.filter { $0.status == .inProgress }.count
    }

    public func task(_ id: Int) -> BacklogTask? {
        tasks.first { $0.id == id }
    }

    public func handle(_ signal: ConnectionSignal) {
        switch signal {
        case .online:
            Task { await refresh() }
        case .event(let event) where event.topic == Topic.backlogChanged:
            let target = event.payload["workspace"]?.stringValue ?? event.workspace
            guard target.isEmpty || target == workspace else { return }
            Task { await refresh() }
        case .event:
            break
        }
    }

    public func refresh() async {
        guard !inflight else {
            again = true
            return
        }
        inflight = true
        defer { inflight = false }
        repeat {
            again = false
            await load()
        } while again
    }

    public func get(_ id: Int) async throws -> BacklogTask? {
        guard let runner else { return nil }
        let result = try await runner.command(BacklogCommand.get, workspace: workspace, args: .object(["id": .number(Double(id))]))
        guard let task = try? result.decode(BacklogTask.self) else { return nil }
        cache.save(result, taskKey(id))
        return task
    }

    public func saved(_ id: Int) -> BacklogTask? {
        cache.load(taskKey(id), as: JSONValue.self).flatMap { try? $0.decode(BacklogTask.self) }
    }

    private func taskKey(_ id: Int) -> ListCache.Key {
        ListCache.Key(computer: key.computer, workspace: workspace, collection: "task", scope: String(id))
    }

    @discardableResult
    public func addNew() async throws -> BacklogTask? {
        let draft = newDraft
        let result = try await submit(BacklogCommand.add, draft.addArgs(), target: "add/" + UUID().uuidString, title: "Add “\(draft.trimmedTitle)”", text: draft.typed)
        newDraft = BacklogDraft()
        guard let result else { return nil }
        await refresh()
        return try? result.decode(BacklogTask.self)
    }

    public func update(_ id: Int, _ changes: [String: JSONValue]) async throws {
        guard !changes.isEmpty else { return }
        let typed = ["title", "description"].compactMap { changes[$0]?.stringValue }.joined(separator: "\n\n")
        try await mutate(BacklogCommand.update, id, changes, title: "Edit #\(id)", text: typed.isEmpty ? nil : typed)
    }

    public func move(_ id: Int, to status: BacklogStatus) async throws {
        try await update(id, ["status": .string(status.rawValue)])
    }

    public func claim(_ id: Int) async throws {
        try await mutate(BacklogCommand.claim, id, title: "Start #\(id)")
    }

    @discardableResult
    public func start(_ id: Int, agent: String? = nil, mode: String? = nil, where place: String? = nil, prompt: String? = nil) async throws -> String? {
        var args: [String: JSONValue] = [:]
        for (key, value) in [("agent", agent), ("mode", mode), ("where", place), ("prompt", prompt)] {
            if let value, !value.isEmpty { args[key] = .string(value) }
        }
        let result = try await mutate(BacklogCommand.start, id, args, title: "Start an agent on #\(id)", text: prompt.flatMap { $0.isEmpty ? nil : $0 })
        return result?["session"]?.stringValue
    }

    public func takeBack(_ id: Int) async throws {
        try await mutate(BacklogCommand.takeBack, id, title: "Take back #\(id)")
    }

    public func release(_ id: Int) async throws {
        try await mutate(BacklogCommand.release, id, title: "Release #\(id)")
    }

    public func done(_ id: Int) async throws {
        try await mutate(BacklogCommand.done, id, title: "Mark #\(id) done")
    }

    @discardableResult
    private func mutate(_ command: String, _ id: Int, _ args: [String: JSONValue] = [:], title: String, text: String? = nil) async throws -> JSONValue? {
        var args = args
        args["id"] = .number(Double(id))
        let result = try await submit(command, .object(args), target: Self.target(id), title: title, text: text)
        if result != nil { await refresh() }
        return result
    }

    private func submit(_ command: String, _ args: JSONValue, target: String, title: String, text: String?) async throws -> JSONValue? {
        guard let runner else { return nil }
        let item = QueuedAction(computer: computer, workspace: workspace, command: command, args: args, target: target, title: title, text: text)
        let offline = queues()
        guard let result = try await queue.submit(item, runner: runner, queues: offline) else {
            if !offline {
                Task { [sleep, retry] in
                    try? await sleep(ActionQueue.retryDelay)
                    retry()
                }
            }
            return nil
        }
        settling.append(Landed(item: item, result: result, at: .now))
        return result
    }

    private func load() async {
        guard let runner else { return }
        let started = Date.now
        do {
            var raw: [JSONValue] = []
            for (status, limit) in [("open", 0), (BacklogStatus.notNow.rawValue, 0), (BacklogStatus.done.rawValue, Self.doneLimit)] {
                var args: [String: JSONValue] = ["status": .string(status), "slim": .bool(true)]
                if limit > 0 { args["limit"] = .number(Double(limit)) }
                guard let tasks = try await runner.command(BacklogCommand.list, workspace: workspace, args: .object(args))["tasks"]?.arrayValue else { throw RemoteError.badResponse }
                raw += tasks
            }
            fetchedTasks = try JSONValue.array(raw).decode([BacklogTask].self)
            self.raw = .array(raw)
            settling.removeAll { $0.at <= started }
            error = nil
            fetched = true
            syncedAt = .now
            saves.request { [weak self] in
                guard let self, let at = self.syncedAt else { return }
                self.cache.save(Snapshot(tasks: self.raw, at: at), self.key)
            }
        } catch {
            self.error = (error as? RemoteError)?.message ?? error.localizedDescription
        }
        loaded = true
    }

    private struct Snapshot: Codable {
        let tasks: JSONValue
        let at: Date
    }
}
