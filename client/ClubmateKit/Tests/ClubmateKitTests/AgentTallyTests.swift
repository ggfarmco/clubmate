import XCTest

import ClubmateKit

final class AgentTallyTests: XCTestCase {
    func testCountsActiveSessionsAndOnesThatNeedYou() {
        let tally = AgentTally([
            AgentSession(id: "a", status: .running, active: true),
            AgentSession(id: "b", status: .waiting, active: true),
            AgentSession(id: "c", parentSession: "a", status: .waiting, needsYou: true, active: true),
            AgentSession(id: "d", status: .done, asks: true),
            AgentSession(id: "e", parentSession: "a", status: .done, asks: true),
            AgentSession(id: "f", status: .running, service: true),
        ])
        XCTAssertEqual(tally.active, 3)
        XCTAssertEqual(tally.needYou, 3)
    }
}
