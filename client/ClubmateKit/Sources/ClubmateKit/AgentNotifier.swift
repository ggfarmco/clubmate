import Foundation

import ClubmateProtocol

extension AgentSession {
    public static func list(_ sessions: JSONValue) -> [AgentSession] {
        (sessions.arrayValue ?? []).compactMap { try? $0.decode(AgentSession.self) }
    }
}

public struct AgentAlert: Sendable, Equatable, Identifiable {
    public let workspace: String
    public let session: String
    public let request: String
    public let title: String
    public let body: String
    public let date: Date

    public var id: String { "\(session)/\(request)" }
    public var kind: NotificationKind { AgentSession.isQuestion(request) ? .question : .permission }

    static func title(question: Bool, summary: PendingSummary?) -> String {
        let heading = question ? "Question" : "Permission"
        guard let summary, !summary.text.isEmpty, (summary.kind == .question) == question else { return heading }
        return heading + " · " + (summary.isCommand ? "Run " + summary.text : summary.text)
    }
}

public struct AgentNotifier: Sendable {
    public static let section = "agents"
    public static let sidebarSlot = "agents.sidebar"
    public static let sessionsNode = "sessions"

    private var posted: Set<String> = []

    public init() {}

    public static func requests(_ sessions: [AgentSession]) -> [AgentAlert] {
        sessions.filter { $0.status == .waiting }.flatMap { session in
            session.pending.enumerated().map { index, request in
                let summary = index == 0 ? session.pendingSummary : nil
                return AgentAlert(
                    workspace: session.workspace,
                    session: session.id,
                    request: request,
                    title: AgentAlert.title(question: AgentSession.isQuestion(request), summary: summary),
                    body: session.title.isEmpty ? "New session" : session.title,
                    date: session.needsYouSince ?? summary?.at ?? session.updatedAt
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
        UIEvent(workspaceId: workspace, slot: sidebarSlot, nodeId: sessionsNode, action: "select", value: .string(session))
    }
}
