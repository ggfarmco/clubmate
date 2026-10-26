import Foundation

@MainActor
public final class CoreClient {
    public nonisolated let notifications: AsyncStream<RPCNotification>

    private let connection: RPCConnection

    public init(connection: RPCConnection) {
        self.connection = connection
        notifications = connection.notifications
    }

    public static func connect(paths: ClubmatePaths) async throws -> CoreClient {
        CoreClient(connection: try await RPCConnection.connect(paths: paths))
    }

    public func close() async {
        await connection.close()
    }

    public func ping() async throws -> PingResult {
        try await connection.call("core.ping", NoParams())
    }

    public func listProjects() async throws -> [Project] {
        try await connection.call("project.list", NoParams())
    }

    public func createProject(name: String, repos: [String] = []) async throws -> Project {
        try await connection.call("project.create", CreateProjectParams(name: name, repos: repos))
    }

    public func uiSections() async throws -> [Section] {
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
        try await connection.call("ui.event", event)
    }

    public func commandList() async throws -> [Command] {
        try await connection.call("command.list", NoParams())
    }

    public func commandRun(id: String, projectID: String, args: JSONValue = .null) async throws -> JSONValue {
        try await connection.call("command.run", CommandRunParams(id: id, projectId: projectID, args: args))
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
}

private struct NoParams: Codable, Sendable {}

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
