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

    @ObservationIgnored private weak var runner: (any CommandRunner)?
    @ObservationIgnored private var inflight = false
    @ObservationIgnored private var again = false

    public init(workspace: String, runner: any CommandRunner) {
        self.workspace = workspace
        self.runner = runner
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
        try await run(BacklogCommand.get, .object(["id": .number(Double(id))]))
    }

    @discardableResult
    public func add(_ draft: BacklogDraft) async throws -> BacklogTask? {
        let task = try await run(BacklogCommand.add, draft.addArgs())
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
            var all: [BacklogTask] = []
            for (status, limit) in [("open", 0), (BacklogStatus.notNow.rawValue, 0), (BacklogStatus.done.rawValue, Self.doneLimit)] {
                var args: [String: JSONValue] = ["status": .string(status)]
                if limit > 0 { args["limit"] = .number(Double(limit)) }
                all += try await runner.command(BacklogCommand.list, workspace: workspace, args: .object(args)).decode(TaskList.self).tasks
            }
            tasks = all
            error = nil
        } catch {
            self.error = (error as? RemoteError)?.message ?? error.localizedDescription
        }
        loaded = true
    }

    private struct TaskList: Decodable {
        let tasks: [BacklogTask]
    }
}
