import Foundation
import XCTest

@testable import CompanionKit

final class SessionJumpsTests: XCTestCase {
    private static let list = #"""
    [
      {"id":"m1","worktree":"/r/m","status":"done","group":"done","listKey":"t1 m1","listGroupKey":"0","listGroupLabel":"main","listFilters":["all"],"numbered":true},
      {"id":"a1","worktree":"/r/m","parentSession":"m1","status":"waiting","needsYou":true,"needsRank":2,"group":"needsYou","listKey":"t2 a1","listGroupKey":"0","listGroupLabel":"main"},
      {"id":"m2","worktree":"/r/m","status":"waiting","needsYou":true,"needsRank":1,"group":"needsYou","listKey":"t3 m2","listGroupKey":"0","listGroupLabel":"main","listFilters":["all","needs"],"inboxKey":"0 t1 m2","numbered":true},
      {"id":"h1","worktree":"/r/m","status":"waiting","needsYou":true,"group":"needsYou","hidden":true,"listKey":"t4 h1","listGroupKey":"0","listGroupLabel":"main"}
    ]
    """#

    private func shown(_ sessions: [AgentSession]) -> [ShownSection] {
        AgentsTree.sections(sessions).map { ShownSection(section: $0, rows: $0.rows(expanded: ["m1"], folded: [], unfolded: [])) }
    }

    func testNumbersAndStepsFollowTheShownRowsAndThePluginsFields() throws {
        let sessions = try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(Self.list.utf8))
        let shown = shown(sessions)

        XCTAssertEqual(SessionJumps.shown(shown).map(\.id).prefix(3), ["m1", "a1", "m2"])
        XCTAssertEqual(SessionJumps.numbered(shown), ["m1", "m2"])
        let needs = SessionJumps.needs(sessions)
        XCTAssertEqual(needs, ["m2", "a1"])
        XCTAssertEqual(SessionJumps.step(needs, from: "m1", by: 1), "m2")
        XCTAssertEqual(SessionJumps.step(needs, from: "m1", by: -1), "a1")
        XCTAssertEqual(SessionJumps.step(needs, from: "a1", by: 1), "m2")
    }

    func testARunThatDoesNotNeedYouTakesItsNumberFromTheRunsReply() throws {
        let rows = try JSONDecoder.clubmate.decode([JSONValue].self, from: Data(#"[{"id":"s1","worktree":"/r/m","status":"running","run":{"id":"gr_1","title":"Pair"},"listKey":"t1 s1","listGroupKey":"0","listGroupLabel":"main","listFilters":["all","running"]}]"#.utf8))
        let runs = try JSONDecoder.clubmate.decode([AgentRunRow].self, from: Data(#"[{"id":"gr_1","graph":"pair","title":"Pair","state":"running","detail":"turn 1","sessions":["s1"],"home":"group","group":"running","status":"running","numbered":true}]"#.utf8))

        let sessions = RunRows.sessions(rows, runs: runs)

        XCTAssertEqual(sessions.filter(\.numbered).map(\.id), ["gr_1"])
    }
}
