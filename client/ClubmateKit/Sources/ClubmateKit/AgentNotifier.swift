import Foundation

import ClubmateProtocol

extension AgentSession {
    public static func list(_ sessions: JSONValue) -> [AgentSession] {
        (sessions.arrayValue ?? []).compactMap { try? $0.decode(AgentSession.self, using: JSONDecoder()) }
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

    public static func requests(_ sessions: [AgentSession]) -> [AgentAlert] {
        sessions.filter { $0.status == .waiting }.flatMap { session in
            session.pending.map { request in
                AgentAlert(
                    workspace: session.workspace,
                    session: session.id,
                    request: request,
                    title: "\(session.agent.isEmpty ? "Agent" : session.agent.capitalized) is waiting for permission",
                    body: session.title.isEmpty ? "New session" : session.title
                )
            }
        }
    }

    public func pending(_ sessions: [AgentSession], shown: (AgentSession) -> Bool) -> [AgentAlert] {
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
