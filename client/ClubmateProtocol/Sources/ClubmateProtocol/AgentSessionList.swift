import Foundation

public typealias AgentSessionList = SyncedList<AgentSession>

extension AgentSession: SyncedItem {
    public static let field = "sessions"
}
