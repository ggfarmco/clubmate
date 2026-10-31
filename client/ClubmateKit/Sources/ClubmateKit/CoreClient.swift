import Foundation

@MainActor
public final class CoreClient {
    private let connection: RPCConnection
    private var subscribers: [UUID: AsyncStream<RPCNotification>.Continuation] = [:]
    private var pumpFinished = false
    public private(set) var stoppedIntentionally = false
    public var confirmHandler: (@MainActor (ConfirmRequest, ConfirmExpiry) async -> Bool)?
    public var snapshotHandler: (@MainActor (SnapshotRequest) async throws -> SnapshotResult)?
    public var presentHandler: (@MainActor (PresentRequest) async throws -> Bool)?
    public var keyHandler: (@MainActor (KeyRequest) async throws -> [String])?
    public var browserHandler: (@MainActor (BrowserCall) async throws -> JSONValue)?

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

    public func workspaceOpenRequests() -> AsyncStream<String> {
        let notifications = subscribeNotifications()
        let (stream, continuation) = AsyncStream.makeStream(of: String.self)
        let pump = Task {
            for await notification in notifications where notification.method == "workspace.show" {
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

    public func listWorkspaces() async throws -> [Workspace] {
        try await connection.call("workspace.list", NoParams())
    }

    public func createWorkspace(name: String, repos: [String] = []) async throws -> Workspace {
        try await connection.call("workspace.create", CreateWorkspaceParams(name: name, repos: repos))
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

    public func renameWorkspace(_ id: String, name: String) async throws {
        let _: Workspace = try await connection.call("workspace.rename", RenameParams(id: id, name: name))
    }

    public func addRepo(_ id: String, path: String) async throws {
        let _: Workspace = try await connection.call("workspace.folder.add", AddRepoParams(id: id, path: path))
    }

    public func deleteWorkspace(_ id: String) async throws {
        let _: NoParams = try await connection.call("workspace.delete", IDParams(id: id), timeout: .seconds(60))
    }

    public func uiSections() async throws -> [SectionInfo] {
        try await connection.call("ui.sections", NoParams())
    }

    public func uiGet(workspaceID: String) async throws -> [SlotView] {
        try await connection.call("ui.get", WorkspaceParams(workspaceId: workspaceID))
    }

    public func uiSubscribe(workspaceID: String) async throws -> String {
        let result: SubscriptionParams = try await connection.call("ui.subscribe", UISubscribeParams(workspaceId: workspaceID, delta: true))
        return result.subscription
    }

    public func uiUnsubscribe(_ subscription: String) async throws {
        let _: NoParams = try await connection.call("ui.unsubscribe", SubscriptionParams(subscription: subscription))
    }

    public func uiEvent(_ event: UIEvent) async throws -> JSONValue {
        try await connection.call("ui.event", event, timeout: .seconds(8))
    }

    public func pluginList() async throws -> [PluginStatus] {
        try await connection.call("plugin.list", NoParams())
    }

    public func pluginRestart(id: String) async throws {
        let _: NoParams = try await connection.call("plugin.restart", IDParams(id: id))
    }

    public func commandList() async throws -> [Command] {
        try await connection.call("command.list", NoParams())
    }

    public func commandRun(id: String, workspaceID: String, args: JSONValue = .null) async throws -> JSONValue {
        try await connection.call("command.run", CommandRunParams(id: id, workspaceId: workspaceID, args: args), timeout: .seconds(8))
    }

    public func collectDiagnostics(workspaceID: String?, appVersion: String, extra: [String: Data]) async throws -> DiagnosticsResult {
        try await connection.call("diagnostics.collect", DiagnosticsParams(workspaceId: workspaceID, appVersion: appVersion, extra: extra), timeout: .seconds(60))
    }

    public func uiStateGet(workspaceID: String) async throws -> JSONValue? {
        let result: StateResult = try await connection.call("ui.state.get", WorkspaceParams(workspaceId: workspaceID))
        return result.state == .null ? nil : result.state
    }

    public func uiStateSet(workspaceID: String, state: JSONValue) async throws {
        let _: NoParams = try await connection.call("ui.state.set", StateSetParams(workspaceId: workspaceID, state: state))
    }

    public func uiStatePatch(workspaceID: String, key: String, value: JSONValue) async throws {
        let _: NoParams = try await connection.call("ui.state.patch", StatePatchParams(workspaceId: workspaceID, key: key, value: value))
    }

    public func busSubscribe(pattern: String, after: Int64 = 0) async throws -> String {
        let result: SubscriptionParams = try await connection.call("bus.subscribe", BusSubscribeParams(pattern: pattern, after: after))
        return result.subscription
    }

    public func busPublish<P: Encodable & Sendable>(topic: String, payload: P) async throws {
        let _: JSONValue = try await connection.call("bus.publish", BusPublishParams(topic: topic, payload: payload))
    }

    public func busUnsubscribe(_ subscription: String) async throws {
        let _: NoParams = try await connection.call("bus.unsubscribe", SubscriptionParams(subscription: subscription))
    }

    private func answer(_ request: RPCRequest) async {
        switch request.method {
        case "ui.snapshot":
            await answerTest(request, snapshotHandler)
            return
        case "ui.present":
            var present: (@MainActor (PresentRequest) async throws -> [String: Bool])?
            if let handler = presentHandler {
                present = { request in ["shown": try await handler(request)] }
            }
            await answerTest(request, present)
            return
        case "ui.key":
            var key: (@MainActor (KeyRequest) async throws -> [String: [String]])?
            if let handler = keyHandler {
                key = { request in ["routes": try await handler(request)] }
            }
            await answerTest(request, key)
            return
        case "browser.call":
            await answerTest(request, browserHandler)
            return
        default:
            break
        }
        guard request.method == "ui.confirm" else {
            try? await connection.replyError(id: request.id, code: -32601, message: "method not found: \(request.method)")
            return
        }
        guard let confirm = try? request.params.decode(ConfirmRequest.self) else {
            try? await connection.replyError(id: request.id, code: -32602, message: "invalid ui.confirm params")
            return
        }
        let handler = confirmHandler
        let reply = await ConfirmRequest.answer(confirm) { await handler?($0, $1) ?? false }
        try? await connection.reply(id: request.id, result: reply)
    }

    private func answerTest<P: Decodable, R: Encodable & Sendable>(_ request: RPCRequest, _ handler: (@MainActor (P) async throws -> R)?) async {
        guard let handler else {
            try? await connection.replyError(id: request.id, code: -32601, message: "method not found: \(request.method)")
            return
        }
        guard let params = try? request.params.decode(P.self) else {
            try? await connection.replyError(id: request.id, code: -32602, message: "invalid \(request.method) params")
            return
        }
        do {
            try await connection.reply(id: request.id, result: try await handler(params))
        } catch let error as TestModeError {
            try? await connection.replyError(id: request.id, code: -32000, message: error.message)
        } catch let error as BrowserError {
            try? await connection.replyError(id: request.id, code: -32000, message: error.message)
        } catch {
            try? await connection.replyError(id: request.id, code: -32000, message: "\(error)")
        }
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

private struct WorkspaceParams: Encodable, Sendable {
    let workspaceId: String
}

private struct UISubscribeParams: Encodable, Sendable {
    let workspaceId: String
    let delta: Bool
}

private struct CreateWorkspaceParams: Encodable, Sendable {
    let name: String
    let repos: [String]
}

private struct SubscriptionParams: Codable, Sendable {
    let subscription: String
}

private struct CommandRunParams: Encodable, Sendable {
    let id: String
    let workspaceId: String
    let args: JSONValue
}

private struct DiagnosticsParams: Encodable, Sendable {
    let workspaceId: String?
    let appVersion: String
    let extra: [String: Data]
}

private struct StateResult: Decodable, Sendable {
    let state: JSONValue
}

private struct StateSetParams: Encodable, Sendable {
    let workspaceId: String
    let state: JSONValue
}

private struct StatePatchParams: Encodable, Sendable {
    let workspaceId: String
    let key: String
    let value: JSONValue
}

private struct BusSubscribeParams: Encodable, Sendable {
    let pattern: String
    let after: Int64
}

private struct BusPublishParams<P: Encodable & Sendable>: Encodable, Sendable {
    let topic: String
    let payload: P
}
