import Foundation

@MainActor
public final class CoreClient {
    private let connection: RPCConnection
    private var subscribers: [UUID: AsyncStream<RPCNotification>.Continuation] = [:]
    private var pumpFinished = false
    public private(set) var stoppedIntentionally = false
    public var confirmHandler: (@MainActor (ConfirmRequest) async -> Bool)?

    var isAlive: Bool { !pumpFinished }

    public init(connection: RPCConnection) {
        self.connection = connection
        Task { [weak self, notifications = connection.notifications] in
            for await notification in notifications {
                self?.broadcast(notification)
            }
            self?.finishSubscribers()
        }
        Task { [weak self, requests = connection.requests] in
            for await request in requests {
                Task { await self?.answer(request) }
            }
        }
    }

    public static func connect(paths: ClubmatePaths) async throws -> CoreClient {
        CoreClient(connection: try await RPCConnection.connect(paths: paths))
    }

    public func close() async {
        await connection.close()
    }

    public func subscribeNotifications() -> AsyncStream<RPCNotification> {
        let (stream, continuation) = AsyncStream.makeStream(of: RPCNotification.self)
        guard !pumpFinished else {
            continuation.finish()
            return stream
        }
        let id = UUID()
        subscribers[id] = continuation
        continuation.onTermination = { [weak self] _ in
            Task { @MainActor in self?.subscribers.removeValue(forKey: id) }
        }
        return stream
    }

    public func projectOpenRequests() -> AsyncStream<String> {
        let notifications = subscribeNotifications()
        let (stream, continuation) = AsyncStream.makeStream(of: String.self)
        let pump = Task {
            for await notification in notifications where notification.method == "project.open" {
                if let id = notification.params.objectValue?["id"]?.stringValue { continuation.yield(id) }
            }
            continuation.finish()
        }
        continuation.onTermination = { _ in pump.cancel() }
        return stream
    }

    public func call(method: String, params: JSONValue = .object([:]), timeout: Duration = .seconds(10)) async throws -> JSONValue {
        try await connection.call(method, params, timeout: timeout)
    }

    public func register(handles: [String]) async throws {
        let _: NoParams = try await connection.call("client.register", RegisterParams(handles: handles))
    }

    public func ping() async throws -> PingResult {
        try await connection.call("core.ping", NoParams())
    }

    public func liveSessionCount() async throws -> Int {
        let sessions: [SessionState] = try await connection.call("session.list", NoParams())
        return sessions.filter { $0.state == "running" }.count
    }

    public func listProjects() async throws -> [Project] {
        try await connection.call("project.list", NoParams())
    }

    public func createProject(name: String, repos: [String] = []) async throws -> Project {
        try await connection.call("project.create", CreateProjectParams(name: name, repos: repos))
    }

    public func touchWorkspace(_ id: String) async throws {
        let _: NoParams = try await connection.call("workspace.touch", IDParams(id: id))
    }

    public func openFolders(_ folders: [String]) async throws -> String {
        let result: WorkspaceResult = try await connection.call("workspace.open", FoldersParams(folders: folders))
        return result.id
    }

    public func homeWorkspace() async throws -> String {
        let result: WorkspaceResult = try await connection.call("workspace.home", NoParams())
        return result.id
    }

    public func renameProject(_ id: String, name: String) async throws {
        let _: Project = try await connection.call("project.rename", RenameParams(id: id, name: name))
    }

    public func addRepo(_ id: String, path: String) async throws {
        let _: Project = try await connection.call("project.addRepo", AddRepoParams(id: id, path: path))
    }

    public func deleteProject(_ id: String) async throws {
        let _: NoParams = try await connection.call("project.delete", IDParams(id: id), timeout: .seconds(60))
    }

    public func uiSections() async throws -> [SectionInfo] {
        try await connection.call("ui.sections", NoParams())
    }

    public func uiGet(projectID: String) async throws -> [SlotView] {
        try await connection.call("ui.get", ProjectParams(projectId: projectID))
    }

    public func uiSubscribe(projectID: String) async throws -> String {
        let result: SubscriptionParams = try await connection.call("ui.subscribe", ProjectParams(projectId: projectID))
        return result.subscription
    }

    public func uiUnsubscribe(_ subscription: String) async throws {
        let _: NoParams = try await connection.call("ui.unsubscribe", SubscriptionParams(subscription: subscription))
    }

    public func uiEvent(_ event: UIEvent) async throws -> JSONValue {
        try await connection.call("ui.event", event, timeout: .seconds(8))
    }

    public func commandList() async throws -> [Command] {
        try await connection.call("command.list", NoParams())
    }

    public func commandRun(id: String, projectID: String, args: JSONValue = .null) async throws -> JSONValue {
        try await connection.call("command.run", CommandRunParams(id: id, projectId: projectID, args: args), timeout: .seconds(8))
    }

    public func uiStateGet(projectID: String) async throws -> JSONValue? {
        let result: StateResult = try await connection.call("ui.state.get", ProjectParams(projectId: projectID))
        return result.state == .null ? nil : result.state
    }

    public func uiStateSet(projectID: String, state: JSONValue) async throws {
        let _: NoParams = try await connection.call("ui.state.set", StateSetParams(projectId: projectID, state: state))
    }

    public func busSubscribe(pattern: String, after: Int64 = 0) async throws -> String {
        let result: SubscriptionParams = try await connection.call("bus.subscribe", BusSubscribeParams(pattern: pattern, after: after))
        return result.subscription
    }

    public func busUnsubscribe(_ subscription: String) async throws {
        let _: NoParams = try await connection.call("bus.unsubscribe", SubscriptionParams(subscription: subscription))
    }

    private func answer(_ request: RPCRequest) async {
        guard request.method == "ui.confirm" else {
            try? await connection.replyError(id: request.id, code: -32601, message: "method not found: \(request.method)")
            return
        }
        guard let confirm = try? request.params.decode(ConfirmRequest.self) else {
            try? await connection.replyError(id: request.id, code: -32602, message: "invalid ui.confirm params")
            return
        }
        let handler = confirmHandler
        let reply = await ConfirmRequest.answer(confirm) { await handler?($0) ?? false }
        try? await connection.reply(id: request.id, result: reply)
    }

    private func broadcast(_ notification: RPCNotification) {
        if notification.method == "core.stopping" { stoppedIntentionally = true }
        for continuation in subscribers.values {
            continuation.yield(notification)
        }
    }

    private func finishSubscribers() {
        pumpFinished = true
        let finished = subscribers
        subscribers = [:]
        for continuation in finished.values {
            continuation.finish()
        }
    }
}

private struct NoParams: Codable, Sendable {}

private struct SessionState: Decodable, Sendable {
    let state: String
}

private struct RegisterParams: Encodable, Sendable {
    let handles: [String]
}

private struct IDParams: Encodable, Sendable {
    let id: String
}

private struct FoldersParams: Encodable, Sendable {
    let folders: [String]
}

private struct WorkspaceResult: Decodable, Sendable {
    let id: String
}

private struct RenameParams: Encodable, Sendable {
    let id: String
    let name: String
}

private struct AddRepoParams: Encodable, Sendable {
    let id: String
    let path: String
}

private struct ProjectParams: Encodable, Sendable {
    let projectId: String
}

private struct CreateProjectParams: Encodable, Sendable {
    let name: String
    let repos: [String]
}

private struct SubscriptionParams: Codable, Sendable {
    let subscription: String
}

private struct CommandRunParams: Encodable, Sendable {
    let id: String
    let projectId: String
    let args: JSONValue
}

private struct StateResult: Decodable, Sendable {
    let state: JSONValue
}

private struct StateSetParams: Encodable, Sendable {
    let projectId: String
    let state: JSONValue
}

private struct BusSubscribeParams: Encodable, Sendable {
    let pattern: String
    let after: Int64
}
