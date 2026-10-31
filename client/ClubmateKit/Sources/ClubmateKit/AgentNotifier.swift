import Foundation

import ClubmateProtocol

public struct AgentSessionState: Sendable, Equatable {
    public let workspace: String
    public let session: String
    public let agent: String
    public let title: String
    public let status: AgentSessionStatus
    public var requests: [String]
    public let policy: NotificationPolicy

    public init(workspace: String, session: String, agent: String, title: String, status: AgentSessionStatus, requests: [String], policy: NotificationPolicy = .all) {
        self.workspace = workspace
        self.session = session
        self.agent = agent
        self.title = title
        self.status = status
        self.requests = requests
        self.policy = policy
    }

    public static func list(_ sessions: JSONValue) -> [AgentSessionState] {
        (sessions.arrayValue ?? []).compactMap { value in
            guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let workspace = fields["workspace"]?.stringValue else { return nil }
            return AgentSessionState(
                workspace: workspace,
                session: id,
                agent: fields["agent"]?.stringValue ?? "",
                title: fields["title"]?.stringValue ?? "",
                status: AgentSessionStatus(fields["status"]?.stringValue),
                requests: fields["pending"]?.arrayValue?.compactMap(\.stringValue) ?? [],
                policy: fields["notifyEffective"]?.stringValue.flatMap(NotificationPolicy.init(rawValue:)) ?? .all
            )
        }
    }
}

public struct AgentAlert: Sendable, Equatable, Identifiable {
    public let workspace: String
    public let session: String
    public let request: String
    public let title: String
    public let body: String

    public var id: String { "\(session)/\(request)" }
}

public struct AgentNotifier: Sendable {
    public static let section = "agents"
    public static let sidebarSlot = "agents.sidebar"
    public static let sessionsNode = "sessions"

    private var posted: Set<String> = []

    public init() {}

    public static func requests(_ sessions: [AgentSessionState]) -> [AgentAlert] {
        sessions.filter { $0.status == .waiting }.flatMap { session in
            session.requests.map { request in
                AgentAlert(
                    workspace: session.workspace,
                    session: session.session,
                    request: request,
                    title: "\(session.agent.isEmpty ? "Agent" : session.agent.capitalized) is waiting for permission",
                    body: session.title.isEmpty ? "New session" : session.title
                )
            }
        }
    }

    public func pending(_ sessions: [AgentSessionState], shown: (AgentSessionState) -> Bool) -> [AgentAlert] {
        Self.requests(sessions.filter { !shown($0) }).filter { !isPosted($0) }
    }

    public func isPosted(_ alert: AgentAlert) -> Bool {
        posted.contains(alert.id)
    }

    public mutating func markPosted(_ alert: AgentAlert) {
        posted.insert(alert.id)
    }

    public static func selectEvent(workspace: String, session: String) -> UIEvent {
        UIEvent(projectId: workspace, slot: sidebarSlot, nodeId: sessionsNode, action: "select", value: .string(session))
    }
}
