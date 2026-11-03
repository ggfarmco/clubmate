import Foundation

import ClubmateProtocol

public struct AgentTally: Sendable, Equatable {
    public let active: Int
    public let needYou: Int

    public init(_ sessions: [AgentSession]) {
        active = sessions.count(where: \.active)
        needYou = sessions.count(where: \.needsYou)
    }
}
