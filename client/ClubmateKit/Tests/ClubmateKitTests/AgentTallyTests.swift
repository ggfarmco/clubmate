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

    func testRunsAloneReadAsRuns() {
        let run = AgentSession(id: "gr_1", status: .waiting, needsYou: true, kind: "run")
        XCTAssertEqual(AgentTally([run, AgentSession(id: "gr_2", status: .waiting, needsYou: true, kind: "run")]).needYouLabel, "2 runs need you")
        XCTAssertEqual(AgentTally([run]).needYouLabel, "1 run needs you")
        XCTAssertEqual(AgentTally([AgentSession(id: "a", status: .waiting, needsYou: true)]).needYouLabel, "1 needs you")
        XCTAssertEqual(AgentTally([run, AgentSession(id: "a", status: .waiting, needsYou: true)]).needYouLabel, "2 need you")
    }

    func testOpenSumsEveryItem() {
        XCTAssertEqual(AgentTally([AgentSession(id: "a", openCount: 3), AgentSession(id: "b", openCount: 2)]).open, 5)
    }
}
