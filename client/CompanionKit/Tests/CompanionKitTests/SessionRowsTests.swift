import Foundation
import XCTest

import CompanionKit

final class SessionRowsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    func testStateLineSaysWhatTheSessionWants() {
        let ago = { (seconds: Double) in self.now.addingTimeInterval(-seconds) }
        let cases: [(AgentSession, SessionGlyph, SessionStateLine)] = [
            (AgentSession(id: "a", status: .waiting, updatedAt: ago(5), pendingSummary: PendingSummary(kind: .permission, text: "go test ./...", tool: "execute", at: ago(120))),
             .permission, SessionStateLine(lead: "Wants to run", text: "go test ./...", mono: true, tone: .primary, age: "2 min")),
            (AgentSession(id: "b", status: .waiting, pendingSummary: PendingSummary(kind: .permission, text: "Edit cart.go", tool: "edit", at: ago(30))),
             .permission, SessionStateLine(lead: "Wants:", text: "Edit cart.go", tone: .primary, age: "30 s")),
            (AgentSession(id: "c", status: .waiting, updatedAt: ago(60), pending: ["permission-1"]),
             .permission, SessionStateLine(text: "Waiting for you", tone: .needsYou, age: "1 min")),
            (AgentSession(id: "d", status: .done, asks: true, pendingSummary: PendingSummary(kind: .question, text: "Add retries?", at: ago(1)), question: "Add retries?"),
             .question, SessionStateLine(lead: "Asks:", text: "Add retries?", tone: .primary)),
            (AgentSession(id: "e", status: .done, asks: true), .question, SessionStateLine(text: "Asking you", tone: .needsYou)),
            (AgentSession(id: "f", status: .running, updatedAt: ago(1), turnStartedAt: ago(240)), .running, SessionStateLine(text: "Running", age: "4 min")),
            (AgentSession(id: "f2", status: .running, turnStartedAt: ago(600), activity: SessionActivity(title: "Editing reserve.go", at: ago(240))), .running, SessionStateLine(text: "Editing reserve.go", age: "4 min")),
            (AgentSession(id: "g", status: .done, childStatus: .running), .running, SessionStateLine(text: "Waiting for subagents")),
            (AgentSession(id: "h", status: .done, childStatus: .waiting), .childWaiting, SessionStateLine(text: "1 subagent waiting")),
            (AgentSession(id: "i", status: .error, error: "boom"), .stopped, SessionStateLine(text: "Stopped", tone: .danger)),
            (AgentSession(id: "i2", status: .error, failure: Self.exit1), .stopped, SessionStateLine(text: "exit 1", tone: .danger)),
            (AgentSession(id: "j", status: .done, updatedAt: ago(3_700)), .done, SessionStateLine(text: "Done", age: "1 h")),
            (AgentSession(id: "j2", status: .done, updatedAt: ago(3_700), review: true), .review, SessionStateLine(text: "Review", age: "1 h")),
            (AgentSession(id: "k", status: .closed, updatedAt: ago(200_000)), .closed, SessionStateLine(text: "Closed", age: "2 d")),
            (AgentSession(id: "k2", status: .closed, updatedAt: ago(200_000), autoClosed: true), .closed, SessionStateLine(text: "Auto-closed", age: "2 d")),
        ]
        for (session, glyph, line) in cases {
            XCTAssertEqual(session.glyph, glyph, session.id)
            XCTAssertEqual(session.stateLine(now: now), line, session.id)
        }
        XCTAssertEqual(AgentSession(id: "i", status: .error).headerState, "Stopped · error")
        XCTAssertEqual(AgentSession(id: "i2", status: .error, failure: Self.exit1).headerState, "Stopped · exit 1")
    }

    func testReviewOnlyOutranksDone() {
        let review = [
            AgentSession(id: "closed", status: .closed, review: true),
            AgentSession(id: "asks", status: .done, asks: true, review: true),
            AgentSession(id: "child", status: .done, childStatus: .waiting, review: true),
            AgentSession(id: "running", status: .running, review: true),
            AgentSession(id: "stopped", status: .error, review: true),
        ]
        XCTAssertEqual(review.map(\.glyph), [.closed, .question, .childWaiting, .running, .stopped])
        XCTAssertFalse(SessionGlyph.review.needsYou)
    }

    private static let exit1 = SessionFailure(word: "exit 1", badge: "Stopped · exit 1", line: "Claude exited with status 1.")

    func testFailedSessionOffersTheMacsRemoteActionsAndNamesTheRest() throws {
        let json = #"{"id":"e","status":"error","failure":{"word":"Stopped","badge":"Stopped · error","line":"Sign in to continue using Claude.","actions":[{"id":"signin","label":"Sign in","icon":"person.badge.key"},{"id":"retry","label":"Retry","icon":"arrow.clockwise","command":"agents.resume","args":{"session":"e"}},{"id":"continue","label":"Hand off to a new session","icon":"arrow.turn.down.right","command":"agents.handoff","args":{"session":"e"}}]}}"#
        let session = try JSONDecoder().decode(AgentSession.self, from: Data(json.utf8))
        XCTAssertEqual(session.failureButtons { $0 == "agents.resume" }.map(\.id), ["retry"])
        XCTAssertEqual(session.failureButtons { $0 == "agents.resume" }.first?.args, ["session": "e"])
        XCTAssertEqual(session.failureOnMac("Mac Studio"), ["Sign in on Mac Studio"])
        XCTAssertEqual(AgentSession(id: "old", status: .error, failure: Self.exit1).failureButtons { _ in true }, [])
    }

    func testSubagentsFoldUnlessTheyNeedYou() throws {
        let tree = try XCTUnwrap(AgentSession.grouped([
            AgentSession(id: "p", status: .done),
            AgentSession(id: "c", parentSession: "p", status: .waiting),
            AgentSession(id: "g", parentSession: "c", status: .done),
            AgentSession(id: "r", parentSession: "p", status: .running),
        ]).first?.sessions.first)
        let folded = tree.rows(expanded: [])
        XCTAssertEqual(folded.map(\.id), ["p", "c"])
        XCTAssertEqual(folded.map(\.toggleCount), [1, 1])
        XCTAssertEqual(tree.rows(expanded: ["p", "c"]).map(\.depth), [0, 1, 2, 1])
        XCTAssertEqual(tree.rows(expanded: ["p", "c"]).first?.toggleCount, 3)
        XCTAssertEqual(tree.session.glyph(waitingChildren: tree.waitingDescendants), .childWaiting)
        XCTAssertEqual(tree.session.stateLine(now: now, waitingChildren: tree.waitingDescendants), SessionStateLine(text: "1 subagent waiting"))
    }

    func testNeedsYouGroupIsOldestFirstBySubtree() {
        let ago = { (seconds: Double) in self.now.addingTimeInterval(-seconds) }
        let needs = AgentSession.grouped([
            AgentSession(id: "a", status: .waiting, updatedAt: ago(1), needsYouSince: ago(60)),
            AgentSession(id: "b", status: .done, updatedAt: ago(500)),
            AgentSession(id: "c", parentSession: "b", status: .waiting, updatedAt: ago(500), needsYouSince: ago(300)),
        ]).first { $0.group == .needsInput }?.sessions
        XCTAssertEqual(needs?.map(\.id), ["b", "a"])
    }

    func testWorkspaceActivityCountsWaitingRunningAndClosed() {
        let activity = WorkspaceActivity([
            AgentSession(id: "a", status: .waiting),
            AgentSession(id: "b", parentSession: "a", status: .done, asks: true),
            AgentSession(id: "c", status: .running),
            AgentSession(id: "d", status: .closed),
            AgentSession(id: "e", status: .done),
        ])
        XCTAssertEqual([activity.waitingText, activity.restText], ["1 waiting", "1 running · 1 closed"])
        XCTAssertEqual(WorkspaceActivity([]).restText, "No sessions")
        XCTAssertEqual(WorkspaceActivity([]).offlineText(asOf: nil), "nothing waiting")
    }

    func testNeedsYouListsRequestsAcrossMacsOldestFirst() {
        let shop = Workspace(id: "w1", name: "shop")
        let api = Workspace(id: "w2", name: "api")
        let items = NeedsYouItem.collect([
            ("mac1", MacTag(name: "Mac Studio", colour: "blue"), shop, [
                AgentSession(id: "a", title: "Refactor the cart total", status: .waiting, pendingSummary: PendingSummary(kind: .permission, text: "go test ./...", tool: "execute", at: now.addingTimeInterval(-60))),
                AgentSession(id: "b", parentSession: "a", title: "Explain the checkout flow", status: .waiting, afk: true, pending: ["elicitation-1"], pendingSummary: PendingSummary(kind: .question, text: "Add retries?", at: now.addingTimeInterval(-30))),
                AgentSession(id: "c", status: .running),
            ]),
            ("mac2", MacTag(name: "MacBook Pro", colour: "magenta"), api, [AgentSession(id: "d", title: "Docs", status: .waiting, pendingSummary: PendingSummary(kind: .permission, text: "Edit README.md", tool: "edit", at: now.addingTimeInterval(-90)))]),
        ])
        XCTAssertEqual(items.map(\.id), ["mac2|d", "mac1|a", "mac1|b"])
        XCTAssertEqual(items.map(\.title), ["Edit README.md?", "Run go test ./...?", "Add retries?"])
        XCTAssertEqual(items.map(\.command), [nil, "go test ./...", nil])
        XCTAssertEqual(items.map(\.mac?.name), ["MacBook Pro", "Mac Studio", "Mac Studio"])
        XCTAssertEqual(items.map(\.subtitle), ["api · Docs", "shop · Refactor the cart total", "shop · Explain the checkout flow"])
        XCTAssertEqual(items.map(\.bannerDetail), ["api · wants: Edit README.md", "shop · wants to run go test ./...", "shop · asks: Add retries?"])
    }
}
