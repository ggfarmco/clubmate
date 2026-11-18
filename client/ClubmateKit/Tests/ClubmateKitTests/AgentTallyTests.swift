import XCTest

import ClubmateKit

final class AgentTallyTests: XCTestCase {
    func testCountsWhatTheChipsCount() {
        let tally = AgentTally([
            AgentSession(id: "a", status: .waiting, needsYou: true, active: true, openCount: 1, listFilters: ["all", "needs", "open"], listOpen: 3),
            AgentSession(id: "b", parentSession: "a", status: .waiting, needsYou: true, active: true, openCount: 2),
            AgentSession(id: "c", status: .running, active: true, listFilters: ["all", "running"]),
            AgentSession(id: "d", status: .done, needsYou: true, group: .closed),
        ])
        XCTAssertEqual(tally.active, 3)
        XCTAssertEqual(tally.needYou, 1)
        XCTAssertEqual(tally.open, 3)
    }

    func testRunsAloneReadAsRuns() {
        let needs = ["needs"]
        let run = AgentSession(id: "gr_1", status: .waiting, needsYou: true, kind: "run", listFilters: needs)
        let session = AgentSession(id: "a", status: .waiting, needsYou: true, listFilters: ["all", "needs"])
        XCTAssertEqual(AgentTally([run, AgentSession(id: "gr_2", status: .waiting, needsYou: true, kind: "run", listFilters: needs)]).needYouLabel, "2 runs need you")
        XCTAssertEqual(AgentTally([run]).needYouLabel, "1 run needs you")
        XCTAssertEqual(AgentTally([session]).needYouLabel, "1 needs you")
        XCTAssertEqual(AgentTally([run, session]).needYouLabel, "2 need you")
    }
}
