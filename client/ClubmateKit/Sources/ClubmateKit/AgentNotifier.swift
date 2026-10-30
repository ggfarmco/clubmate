import Foundation

public struct AgentSessionState: Sendable, Equatable {
    public let workspace: String
    public let session: String
    public let agent: String
    public let title: String
    public let status: String
    public var requests: [String]

    public init(workspace: String, session: String, agent: String, title: String, status: String, requests: [String]) {
        self.workspace = workspace
        self.session = session
        self.agent = agent
        self.title = title
        self.status = status
        self.requests = requests
    }

    public static func list(_ sessions: JSONValue) -> [AgentSessionState] {
        (sessions.arrayValue ?? []).compactMap { value in
            guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let workspace = fields["workspace"]?.stringValue else { return nil }
            return AgentSessionState(
                workspace: workspace,
                session: id,
                agent: fields["agent"]?.stringValue ?? "",
                title: fields["title"]?.stringValue ?? "",
                status: fields["status"]?.stringValue ?? "",
                requests: fields["pending"]?.arrayValue?.compactMap(\.stringValue) ?? []
            )
        }
    }
}

public struct AgentAlert: Sendable, Equatable {
    public let workspace: String
    public let session: String
    public let request: String
    public let title: String
    public let body: String
}

public struct AgentNotifier: Sendable {
    public static let section = "agents"
    public static let sidebarSlot = "agents.sidebar"
    public static let sessionsNode = "sessions"
    public static let changedTopic = "agents.changed"
    public static let sessionsCommand = "agents.sessions"
    public static let waiting = "waiting"

    private var posted: Set<String> = []

    public init() {}

    public func pending(_ sessions: [AgentSessionState], shown: (AgentSessionState) -> Bool) -> [AgentAlert] {
        var alerts: [AgentAlert] = []
        for session in sessions where session.status == Self.waiting && !shown(session) {
            for request in session.requests where !posted.contains(session.session + "/" + request) {
                alerts.append(AgentAlert(
                    workspace: session.workspace,
                    session: session.session,
                    request: request,
                    title: "\(session.agent.isEmpty ? "Agent" : session.agent.capitalized) is waiting for permission",
                    body: session.title.isEmpty ? "New session" : session.title
                ))
            }
        }
        return alerts
    }

    public func isPosted(_ alert: AgentAlert) -> Bool {
        posted.contains(alert.session + "/" + alert.request)
    }

    public mutating func markPosted(_ alert: AgentAlert) {
        posted.insert(alert.session + "/" + alert.request)
    }

    public static func selectEvent(workspace: String, session: String) -> UIEvent {
        UIEvent(projectId: workspace, slot: sidebarSlot, nodeId: sessionsNode, action: "select", value: .string(session))
    }
}
