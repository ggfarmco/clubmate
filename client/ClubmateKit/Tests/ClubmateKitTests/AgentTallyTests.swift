import XCTest

import ClubmateKit

final class AgentTallyTests: XCTestCase {
    func testCountsActiveSessionsAndTopLevelOnesThatNeedYou() {
        let tally = AgentTally([
            AgentSession(id: "a", status: .running),
            AgentSession(id: "b", status: .waiting),
            AgentSession(id: "c", parentSession: "a", status: .waiting),
            AgentSession(id: "d", status: .done, asks: true),
            AgentSession(id: "e", status: .done),
            AgentSession(id: "f", status: .closed),
        ])
        XCTAssertEqual(tally.active, 3)
        XCTAssertEqual(tally.needYou, 2)
    }
}
