import Foundation

import ClubmateProtocol

public struct AgentTally: Sendable, Equatable {
    public let active: Int
    public let needYou: Int
    public let runsNeedYou: Int
    public let open: Int

    public init(_ sessions: [AgentSession]) {
        active = sessions.count(where: \.active)
        needYou = sessions.listed(in: .needs)
        runsNeedYou = sessions.filter { $0.kind == "run" }.listed(in: .needs)
        open = sessions.listedOpen
    }

    public var needYouLabel: String {
        guard needYou > 0, runsNeedYou == needYou else { return needYou == 1 ? "1 needs you" : "\(needYou) need you" }
        return needYou == 1 ? "1 run needs you" : "\(needYou) runs need you"
    }
}
