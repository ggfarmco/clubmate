import Foundation
import Observation

@MainActor
@Observable
public final class BacklogStore {
    public static let doneLimit = 50

    public let workspace: String
    public private(set) var tasks: [BacklogTask] = []
    public private(set) var loaded = false
    public private(set) var error: String?
    public private(set) var syncedAt: Date?
    public private(set) var fetched = false
    public var newDraft = BacklogDraft()

    @ObservationIgnored private weak var runner: (any CommandRunner)?
    @ObservationIgnored private let cache: ListCache
    @ObservationIgnored private let key: ListCache.Key
    @ObservationIgnored private let saves = SaveThrottle()
    @ObservationIgnored private var raw: JSONValue = .array([])
    @ObservationIgnored private var inflight = false
    @ObservationIgnored private var again = false

    public init(workspace: String, runner: any CommandRunner, cache: ListCache = ListCache(dir: nil), computer: String = "") {
        self.workspace = workspace
        self.runner = runner
        self.cache = cache
        key = ListCache.Key(computer: computer, workspace: workspace, collection: "backlog")
        if let saved = cache.load(key, as: Snapshot.self), let tasks = try? saved.tasks.decode([BacklogTask].self) {
            self.tasks = tasks
            syncedAt = saved.at
            loaded = true
        }
    }

    public func ageLine(now: Date = .now) -> String? {
        ListCache.ageLine(syncedAt: syncedAt, error: error, empty: tasks.isEmpty, now: now)
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
        let task = try await run(BacklogCommand.add, newDraft.addArgs())
        newDraft = BacklogDraft()
        await refresh()
        return task
    }

    public func update(_ id: Int, _ changes: [String: JSONValue]) async throws {
        guard !changes.isEmpty else { return }
        try await mutate(BacklogCommand.update, id, changes)
    }

    public func move(_ id: Int, to status: BacklogStatus) async throws {
        try await update(id, ["status": .string(status.rawValue)])
    }

    public func claim(_ id: Int) async throws {
        try await mutate(BacklogCommand.claim, id)
    }

    @discardableResult
    public func start(_ id: Int, agent: String? = nil, mode: String? = nil, where place: String? = nil, prompt: String? = nil) async throws -> String? {
        var args: [String: JSONValue] = ["id": .number(Double(id))]
        for (key, value) in [("agent", agent), ("mode", mode), ("where", place), ("prompt", prompt)] {
            if let value, !value.isEmpty { args[key] = .string(value) }
        }
        let result = try await runner?.command(BacklogCommand.start, workspace: workspace, args: .object(args))
        await refresh()
        return result?["session"]?.stringValue
    }

    public func takeBack(_ id: Int) async throws {
        try await mutate(BacklogCommand.takeBack, id)
    }

    public func release(_ id: Int) async throws {
        try await mutate(BacklogCommand.release, id)
    }

    public func done(_ id: Int) async throws {
        try await mutate(BacklogCommand.done, id)
    }

    private func mutate(_ command: String, _ id: Int, _ args: [String: JSONValue] = [:]) async throws {
        var args = args
        args["id"] = .number(Double(id))
        try await run(command, .object(args))
        await refresh()
    }

    @discardableResult
    private func run(_ command: String, _ args: JSONValue) async throws -> BacklogTask? {
        guard let runner else { return nil }
        let result = try await runner.command(command, workspace: workspace, args: args)
        return try? result.decode(BacklogTask.self)
    }

    private func load() async {
        guard let runner else { return }
        do {
            var raw: [JSONValue] = []
            for (status, limit) in [("open", 0), (BacklogStatus.notNow.rawValue, 0), (BacklogStatus.done.rawValue, Self.doneLimit)] {
                var args: [String: JSONValue] = ["status": .string(status), "slim": .bool(true)]
                if limit > 0 { args["limit"] = .number(Double(limit)) }
                guard let tasks = try await runner.command(BacklogCommand.list, workspace: workspace, args: .object(args))["tasks"]?.arrayValue else { throw RemoteError.badResponse }
                raw += tasks
            }
            tasks = try JSONValue.array(raw).decode([BacklogTask].self)
            self.raw = .array(raw)
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
