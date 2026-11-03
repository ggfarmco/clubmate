import Foundation
import XCTest

import CompanionKit

final class GraphsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    func testRunEntriesNeedYouAsRunRows() throws {
        let json = #"""
        [{"id":"gr_step","kind":"run","workspace":"ws","title":"Review gate","status":"waiting","run":{"id":"gr_step","title":"Review gate"},"pending":["step-squash"],"pendingSummary":{"kind":"permission","tool":"execute","text":"git merge --squash fix","at":"2027-01-15T08:00:00Z"},"needsYou":true,"needsYouSince":"2027-01-15T08:00:00Z","group":"needsYou"},
         {"id":"gr_cap","kind":"run","workspace":"ws","title":"Review gate","status":"waiting","pending":["cap-1"],"pendingSummary":{"kind":"question","text":"Tests → Coder ran 2 of 2 rounds","at":"2027-01-15T07:00:00Z"},"needsYou":true,"needsYouSince":"2027-01-15T07:00:00Z","group":"needsYou"}]
        """#
        let sessions = try JSONDecoder().decode([AgentSession].self, from: Data(json.utf8))
        XCTAssertEqual(sessions.map(\.glyph), [.step, .cap])
        XCTAssertEqual(AgentSession.grouped(sessions).map { $0.group }, [.needsInput])
        XCTAssertEqual(AgentSession.grouped(sessions).first?.sessions.map(\.id), ["gr_cap", "gr_step"])
        XCTAssertEqual(sessions[0].stateLine(now: now).lead, "Wants to run")
        XCTAssertEqual(sessions[1].stateLine(now: now), SessionStateLine(lead: "Tests → Coder ran 2 of 2 rounds ·", text: "continue or stop"))
        let items = NeedsYouItem.collect([(computer: "mac", mac: nil, workspace: Workspace(id: "ws", name: "clubmate"), sessions: sessions)])
        XCTAssertEqual(items.map(\.title), ["Tests → Coder ran 2 of 2 rounds", "Run git merge --squash fix?"])
    }

    func testShowDecodesAStepWaitingForApproval() throws {
        let json = #"""
        {"id":"gr_1","graph":"review-gate","title":"Review gate","status":"running","input":"Fix the flaky switcher test","turns":4,"maxTurns":12,"startedAt":"2027-01-15T08:00:00.123456789Z",
         "edges":[{"from":"coder","to":"tests","on":"done"}],"moves":[],"held":[],
         "nodes":[{"id":"coder","role":"Coder","start":true,"session":"agt_1","title":"Review gate · Coder","status":"done"},
                  {"id":"tests","role":"Tests","step":true,"session":"","status":"pass","last":{"status":"finished","outcome":"pass","exitCode":0,"command":"make test","durationMs":70400}},
                  {"id":"squash","role":"Squash into main","step":true,"session":"","status":"waiting","last":{"status":"waiting","command":"git merge --squash fix","waitingAt":"2027-01-15T08:20:00Z"},
                   "pending":{"command":"git merge --squash fix","dir":"/Users/me/src/clubmate","main":"/Users/me/src/clubmate","rule":"Bash(git merge --squash fix)"}}]}
        """#
        let run = try JSONDecoder.clubmate.decode(GraphRun.self, from: Data(json.utf8))
        let entry = AgentSession(id: "gr_1", status: .waiting, pending: ["step-squash"], needsYou: true, kind: "run")
        XCTAssertEqual(run.inputLine, "Fix the flaky switcher test · turn 4 of 12")
        XCTAssertEqual(run.lines(now: Date.parseRFC3339("2027-01-15T08:24:00Z")!).map { [$0.detail, $0.word, $0.time ?? ""] }, [["agent", "done", ""], ["make test", "pass", "1 min 10 s"], ["git merge --squash fix", "waiting", "4 min"]])
        XCTAssertEqual(run.request(entry), .step(node: "squash", role: "Squash into main", command: "git merge --squash fix", place: "in ~/src/clubmate · the main checkout · not in the allow list", rule: "Bash(git merge --squash fix)"))
    }
}
