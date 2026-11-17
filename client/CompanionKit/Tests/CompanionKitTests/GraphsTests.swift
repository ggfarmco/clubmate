import Foundation
import XCTest

import CompanionKit

final class GraphsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func runPage() throws -> (rows: [JSONValue], runs: [AgentRunRow], dock: [AgentDockRow]) {
        let json = #"""
        {"sessions":[{"id":"agt_c","workspace":"ws","title":"Review gate · Coder","status":"idle","run":{"id":"gr_1","title":"Review gate"}},
                     {"id":"agt_x","workspace":"ws","title":"Solo","status":"done"},
                     {"id":"gr_1","kind":"run","workspace":"ws","title":"Review gate","status":"waiting","needsYou":true,"run":{"id":"gr_1","title":"Review gate","number":4}}],
         "runs":[{"id":"gr_1","graph":"review-gate","title":"Review gate","number":4,"state":"running","detail":"turn 2","sessions":["agt_c"],"home":"group","group":"running","status":"running"},
                 {"id":"gr_9","graph":"nightly","title":"Nightly","state":"emptied","detail":"no sessions","sessions":[],"home":"closed","group":"closed","status":"closed"}],
         "dock":[{"id":"service","label":"Service","icon":"gearshape.2","detail":"asks you","phoneDetail":"1 asks","attention":true},
                 {"id":"gates","label":"Land gates","icon":"checkmark.shield","detail":"1 asking","phoneDetail":"1 asking"},
                 {"id":"graphs","label":"Graphs","icon":"point.3.connected.trianglepath.dotted","detail":"1 to trust","phoneDetail":"1 to trust","attention":true}]}
        """#
        let page = try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
        return (page["sessions"]?.arrayValue ?? [], try page["runs"]!.decode([AgentRunRow].self), try page["dock"]!.decode([AgentDockRow].self))
    }

    func testRunRowsNestNodeSessions() throws {
        let page = try runPage()
        let sessions = RunRows.sessions(page.rows, runs: page.runs)
        let groups = AgentSession.grouped(sessions)
        let run = try XCTUnwrap(groups.flatMap(\.sessions).first { $0.id == "gr_1" })
        XCTAssertEqual(run.children.map(\.id), ["agt_c"])
        XCTAssertEqual(run.session.displayTitle, "Review gate · #4")
        XCTAssertEqual(sessions.filter { $0.id == "gr_1" }.count, 1)
    }

    func testDockRowsEndTheListPlain() throws {
        let rows = RunRows.endRows(try runPage().dock)
        XCTAssertEqual(rows.map(\.label), ["Land gates", "Graphs"])
        XCTAssertEqual(rows.map(\.detail), ["1 asking", "1 to trust"])
        let order = SessionGroup.allCases
        XCTAssertLessThan(try XCTUnwrap(order.firstIndex(of: .closed)), try XCTUnwrap(order.firstIndex(of: .service)))
    }

    func testRunRowKeepsThePluginsNeedsYouEntry() throws {
        let json = #"""
        [{"id":"gr_step","kind":"run","workspace":"ws","title":"Review gate","status":"waiting","run":{"id":"gr_step","title":"Review gate","number":4},"pending":["step-squash"],"pendingSummary":{"kind":"permission","tool":"execute","role":"Squash into main","text":"git merge --squash fix","at":"2027-01-15T08:00:00Z"},"needsYou":true,"needsYouSince":"2027-01-15T08:00:00Z","group":"needsYou"}]
        """#
        let rows = try JSONDecoder().decode([JSONValue].self, from: Data(json.utf8))
        let runs = try JSONDecoder().decode([AgentRunRow].self, from: Data(#"[{"id":"gr_step","graph":"review-gate","title":"Review gate","number":4,"state":"asks","detail":"asks you","needsYou":true,"request":"step-squash","sessions":[],"home":"group"}]"#.utf8))
        let session = try XCTUnwrap(RunRows.sessions(rows, runs: runs).first { $0.id == "gr_step" })
        XCTAssertEqual(session.stateLine(now: now).lead, "Squash into main wants to run")
    }

    func testGateRunAskNestsUnderItsLanderInNeedsYou() throws {
        let json = #"""
        [{"id":"agt_l","workspace":"ws","title":"Fix login","status":"done","group":"needsYou","landing":{"round":1,"of":2,"verdict":"reviewing","words":"asking","tone":"warning","into":"main","badge":"reviewing","asking":true}},
         {"id":"gr_g","kind":"run","workspace":"ws","title":"Round 1 review","status":"waiting","run":{"id":"gr_g","title":"where"},"rowUnder":"agt_l","pending":["step-where"],"pendingSummary":{"kind":"permission","tool":"execute","role":"where","text":"pwd","at":"2027-01-15T08:00:00Z"},"needsYou":true,"needsYouSince":"2027-01-15T08:00:00Z","group":"needsYou"}]
        """#
        let rows = try JSONDecoder().decode([JSONValue].self, from: Data(json.utf8))
        let groups = AgentSession.grouped(RunRows.sessions(rows, runs: []))
        let lander = try XCTUnwrap(groups.first { $0.group == .needsInput }?.sessions.first)
        XCTAssertEqual(lander.id, "agt_l")
        XCTAssertEqual(lander.session.stateLine(now: now).text, "asking")
        let ask = try XCTUnwrap(lander.children.first)
        XCTAssertEqual(ask.id, "gr_g")
        XCTAssertEqual(ask.session.stateLine(now: now).lead, "where wants to run")
    }

    func testRunRowRendersThePluginsFields() throws {
        let runs = try JSONDecoder().decode([AgentRunRow].self, from: Data(#"[{"id":"gr_2","graph":"g","title":"Gate","state":"failed","detail":"failed","sessions":["agt_x"],"home":"group","group":"done","status":"error"}]"#.utf8))
        let rows = try JSONDecoder().decode([JSONValue].self, from: Data(#"[{"id":"agt_x","workspace":"ws","status":"done"}]"#.utf8))
        let run = try XCTUnwrap(RunRows.sessions(rows, runs: runs).first { $0.id == "gr_2" })
        XCTAssertEqual(run.group, .idle)
        XCTAssertEqual(run.status, .error)
        XCTAssertEqual(run.stateLine(now: now).text, "failed")
    }

    func testRunningRunWithoutTheNeedsYouFieldDoesNotNeedYou() throws {
        let runs = try JSONDecoder().decode([AgentRunRow].self, from: Data(#"[{"id":"gr_3","graph":"nightly","title":"Nightly","number":11,"state":"running","detail":"turn 1","sessions":[],"home":"group","group":"running","status":"running"}]"#.utf8))
        let run = try XCTUnwrap(RunRows.sessions([], runs: runs).first { $0.id == "gr_3" })
        XCTAssertEqual(run.glyph, .running)
        XCTAssertEqual(run.stateLine(now: now).tone, .secondary)
        let workspace = Workspace(id: "ws", name: "clubmate")
        XCTAssertTrue(NeedsYouItem.collect([(computer: "m", mac: nil, workspace: workspace, sessions: [run])]).isEmpty)
    }

    func testEmptiedRunOpensRunScreen() throws {
        let page = try runPage()
        let emptied = try XCTUnwrap(RunRows.sessions(page.rows, runs: page.runs).first { $0.id == "gr_9" })
        XCTAssertTrue(GraphRun.isRun(emptied.id))
        XCTAssertEqual(AgentSession.grouped([emptied]).first?.group, .closed)
    }

    func testGraphCommandFallsBackOnOldMac() {
        let old = Hello(computer: ComputerInfo(id: "m", name: "M"), version: "1", commands: ["graphs.show"], topics: [])
        let new = Hello(computer: ComputerInfo(id: "m", name: "M"), version: "1", commands: ["agents.graph-show", "graphs.show"], topics: [])
        XCTAssertEqual(GraphCommand.id(GraphCommand.show, supports: old.commands.contains), "graphs.show")
        XCTAssertEqual(GraphCommand.id(GraphCommand.show, supports: new.commands.contains), "agents.graph-show")
    }

    func testRunEntriesNeedYouAsRunRows() throws {
        let json = #"""
        [{"id":"gr_step","kind":"run","workspace":"ws","title":"Review gate","status":"waiting","run":{"id":"gr_step","title":"Review gate","number":4},"pending":["step-squash"],"pendingSummary":{"kind":"permission","tool":"execute","role":"Squash into main","text":"git merge --squash fix","at":"2027-01-15T08:00:00Z"},"needsYou":true,"needsYouSince":"2027-01-15T08:00:00Z","group":"needsYou"},
         {"id":"gr_cap","kind":"run","workspace":"ws","title":"Review gate","status":"waiting","pending":["cap-1"],"pendingSummary":{"kind":"question","text":"Tests → Coder ran 2 of 2 rounds","at":"2027-01-15T07:00:00Z"},"needsYou":true,"needsYouSince":"2027-01-15T07:00:00Z","group":"needsYou"}]
        """#
        let sessions = try JSONDecoder().decode([AgentSession].self, from: Data(json.utf8))
        XCTAssertEqual(sessions.map(\.glyph), [.step, .cap])
        XCTAssertEqual(AgentSession.grouped(sessions).map { $0.group }, [.needsInput])
        XCTAssertEqual(AgentSession.grouped(sessions).first?.sessions.map(\.id), ["gr_cap", "gr_step"])
        XCTAssertEqual(sessions[0].stateLine(now: now).lead, "Squash into main wants to run")
        XCTAssertEqual(sessions.map(\.displayTitle), ["Review gate · #4", "Review gate"])
        XCTAssertEqual(sessions[1].stateLine(now: now), SessionStateLine(lead: "Tests → Coder ran 2 of 2 rounds ·", text: "continue or stop"))
        let items = NeedsYouItem.collect([(computer: "mac", mac: nil, workspace: Workspace(id: "ws", name: "clubmate"), sessions: sessions)])
        XCTAssertEqual(items.map(\.title), ["Tests → Coder ran 2 of 2 rounds", "Run git merge --squash fix?"])
    }

    func testShowDecodesAStepWaitingForApproval() throws {
        let json = #"""
        {"id":"gr_1","graph":"review-gate","title":"Review gate","number":4,"status":"running","input":"Fix the flaky switcher test","turns":4,"maxTurns":12,"startedAt":"2027-01-15T08:00:00.123456789Z",
         "edges":[{"from":"coder","to":"tests","on":"done"}],"moves":[],"held":[],
         "nodes":[{"id":"coder","role":"Coder","start":true,"session":"agt_1","title":"Review gate · Coder","status":"done","outcome":"approved","durationMs":480000},
                  {"id":"ship","role":"Ship","step":true,"session":"","status":"not started","command":"make ship"},
                  {"id":"tests","role":"Tests","step":true,"session":"","status":"pass","last":{"status":"finished","outcome":"pass","exitCode":0,"command":"make test","durationMs":70400}},
                  {"id":"squash","role":"Squash into main","step":true,"session":"","status":"waiting","last":{"status":"waiting","command":"git merge --squash fix","waitingAt":"2027-01-15T08:20:00Z"},
                   "pending":{"command":"git merge --squash fix","dir":"/Users/me/src/clubmate","main":"/Users/me/src/clubmate","rule":"Bash(git merge --squash fix)"}}]}
        """#
        let run = try JSONDecoder.clubmate.decode(GraphRun.self, from: Data(json.utf8))
        let entry = AgentSession(id: "gr_1", status: .waiting, pending: ["step-squash"], needsYou: true, kind: "run")
        XCTAssertEqual(run.displayTitle, "Review gate · #4")
        XCTAssertEqual(run.inputLine, "Fix the flaky switcher test · turn 4 of 12")
        XCTAssertEqual(run.lines(now: Date.parseRFC3339("2027-01-15T08:24:00Z")!).map { [$0.detail, $0.word, $0.time ?? ""] }, [["agent", "approved", "8 min"], ["make ship", "not started", ""], ["make test", "pass", "1 min 10 s"], ["git merge --squash fix", "waiting", "4 min"]])
        XCTAssertEqual(run.request(entry), .step(node: "squash", role: "Squash into main", command: "git merge --squash fix", place: "in ~/src/clubmate · the main checkout · not in the allow list", rule: "Bash(git merge --squash fix)"))
    }
}
