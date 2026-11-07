import Foundation

import ClubmateProtocol

public struct AgentTally: Sendable, Equatable {
    public let active: Int
    public let needYou: Int
    public let runsNeedYou: Int
    public let open: Int

    public init(_ sessions: [AgentSession]) {
        active = sessions.count(where: \.active)
        needYou = sessions.count(where: \.needsYou)
        runsNeedYou = sessions.count { $0.needsYou && $0.kind == "run" }
        open = sessions.reduce(0) { $0 + $1.openCount }
    }

    public var needYouLabel: String {
        guard needYou > 0, runsNeedYou == needYou else { return needYou == 1 ? "1 needs you" : "\(needYou) need you" }
        return needYou == 1 ? "1 run needs you" : "\(needYou) runs need you"
    }
}
