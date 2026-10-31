import Foundation

import ClubmateProtocol

public struct AgentTally: Sendable, Equatable {
    public let active: Int
    public let needYou: Int

    public init(_ sessions: [AgentSession]) {
        active = sessions.count { $0.status == .running || $0.status == .waiting }
        needYou = sessions.count { $0.parentSession == nil && ($0.status == .waiting || $0.status == .done && $0.asks) }
    }
}
