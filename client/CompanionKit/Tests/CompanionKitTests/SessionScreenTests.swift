import Foundation
import XCTest

import CompanionKit

final class SessionScreenTests: XCTestCase {
    func testPendingPermissionDocksAndToolsGroupAndPlanProseFolds() throws {
        let transcript = try JSONDecoder.clubmate.decode(AgentTranscript.self, from: fixture("agents-transcript"))
        let session = try JSONDecoder.clubmate.decode([AgentSession].self, from: fixture("agents-sessions"))[0]
        let screen = SessionScreen(entries: transcript.entries, session: session)
        XCTAssertEqual(session.state, .waitingForYou)
        XCTAssertEqual(session.headerDetail, "shop")
        XCTAssertEqual(screen.rows.map(\.id), ["user/prompt-1", "thought/thought-1", "agent/agent-2", "plan/plan-1", "tool/read1", "tool/sub-1", "compaction/compact1"])
        guard case .agent(_, let blocks, _, _) = screen.rows[2], case .tools(_, let tools) = screen.rows[4],
              case .tools(_, let subagent) = screen.rows[5],
              case .permission(let request, 0) = screen.dock
        else { return XCTFail("unexpected screen \(screen)") }
        XCTAssertEqual(blocks.first, .paragraph("I'll move the total into `cartTotal` and keep **behaviour** the same."))
        XCTAssertFalse(blocks.contains { if case .list = $0 { true } else { false } })
        XCTAssertEqual(tools.title, "3 actions")
        XCTAssertEqual(tools.detail, "read cart.go · edit cart.go · write checkout-retries.md")
        XCTAssertEqual(subagent.calls.map(\.stoppable), [true])
        XCTAssertEqual(request.id, "permission-3")
        XCTAssertEqual(request.question, "Run a command?")
        XCTAssertEqual(request.alwaysConfirm("allow_always")?.rule, "Bash(go test:*)")
        XCTAssertEqual(request.toolCall.intent, "Run the shop tests")
    }

    func testAnsweredPermissionBecomesOneSummaryRow() throws {
        let json = #"""
        [{"kind":"permission","id":"p1","toolCall":{"kind":"execute","rawInput":{"command":"go vet ./..."}},
          "options":[{"optionId":"a","name":"Allow","kind":"allow_once"},{"optionId":"r","name":"Reject","kind":"reject_once"}],
          "outcome":{"outcome":"selected","optionId":"a"},"at":"2026-10-31T09:41:00Z","answeredAt":"2026-10-31T09:42:00Z"}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        let screen = SessionScreen(entries: entries, session: AgentSession(id: "s", status: .running))
        guard case .answered(let request) = screen.rows.first else { return XCTFail("unexpected rows \(screen.rows)") }
        XCTAssertEqual(request.summary, "Allowed once · go vet ./...")
        XCTAssertEqual(request.answeredAt, Date.parseRFC3339("2026-10-31T09:42:00Z"))
        XCTAssertNil(screen.dock)
    }

    func testPendingQuestionDocksAndAnsweredOneIsARow() throws {
        let json = #"""
        [{"kind":"elicitation","id":"elicitation-1","elicitation":{"mode":"form","message":"Which store?","fields":[]},"response":{"action":"decline"}},
         {"kind":"elicitation","id":"elicitation-2","elicitation":{"mode":"url","message":"Sign in","url":"https://example.com"}}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        let screen = SessionScreen(entries: entries, session: AgentSession(id: "s", status: .waiting, pending: ["elicitation-2"]))
        XCTAssertEqual(screen.rows.map(\.id), ["elicitation/elicitation-1"])
        guard case .elicitation(let question) = screen.dock else { return XCTFail("unexpected dock \(String(describing: screen.dock))") }
        XCTAssertEqual(question.request.url, "https://example.com")
    }

    func testThoughtRowSaysHowLongItTookUntilTheNextEntry() throws {
        let json = #"""
        [{"kind":"thought","id":"t1","text":"a","at":"2026-10-31T09:41:00Z"},{"kind":"agent","id":"a1","text":"b","at":"2026-10-31T09:41:03.4Z"},
         {"kind":"thought","id":"t2","text":"c","at":"2026-10-31T09:41:05Z"},{"kind":"thought","id":"t3","text":"d"},{"kind":"agent","id":"a2","text":"e"}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        let rows = SessionScreen(entries: entries, session: AgentSession(id: "s", status: .running)).rows
        XCTAssertEqual(rows.compactMap { if case .thought(_, _, let title) = $0 { title } else { nil } }, ["Thought for 3 s", "Thought", "Thought"])
        XCTAssertEqual(rows.first?.id, "thought/t1")
    }

    func testAlwaysRuleIsReadFromTheAdaptersLabel() throws {
        let cases: [(kind: String, command: String, name: String, rule: String, session: Bool)] = [
            ("execute", "go test ./...", "Yes, and don't ask again for go test and npm run commands", "Bash(go test:*)\nBash(npm run:*)", false),
            ("execute", "make app && make lint", "Yes, and don't ask again for similar commands", "Bash(make:*)", false),
            ("execute", "  go vet ./...", "Always Allow", "Bash(go:*)", false),
            ("fetch", "", "Yes, and don't ask again for example.com", "WebFetch(domain:example.com)", false),
            ("edit", "", "Yes, allow all edits during this session", "Allow all edits during this session", true),
        ]
        for c in cases {
            let json = #"{"kind":"permission","id":"p","toolCall":{"kind":"\#(c.kind)","rawInput":{"command":"\#(c.command)"}},"options":[{"optionId":"x","name":"\#(c.name)","kind":"allow_always"}]}"#
            guard case .permission(let request) = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.utf8)).content else { return XCTFail("not a permission") }
            XCTAssertEqual(request.alwaysConfirm("x")?.rule, c.rule, c.name)
            XCTAssertEqual(request.alwaysConfirm("x")?.forSession, c.session, c.name)
        }
    }

    func testAlwaysConfirmIsTheOptionThatAsked() throws {
        let json = #"""
        {"kind":"permission","id":"p","toolCall":{"kind":"execute","rawInput":{"command":"go test ./..."}},
         "options":[{"optionId":"a1","name":"Yes, and don't ask again for go test commands","kind":"allow_always"},{"optionId":"a2","name":"Yes, and don't ask again for go commands","kind":"allow_always"},{"optionId":"o","name":"Yes","kind":"allow_once"}],
         "card":{"title":"Run a command?","command":"go test ./...","options":[
           {"id":"o","label":"Yes","kind":"allow_once","shortcut":"opt+1"},
           {"id":"a1","label":"Yes, and don't ask again for go test commands","kind":"allow_always","shortcut":"opt+2","rule":"Bash(go test:*)","scope":"in shop on this Mac"},
           {"id":"a2","label":"Yes, and don't ask again for go commands","kind":"allow_always","shortcut":"opt+3","rule":"Bash(go:*)","scope":"in shop on this Mac"}]}}
        """#
        guard case .permission(let request) = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.utf8)).content else { return XCTFail("not a permission") }
        XCTAssertEqual([request.keyed(2), request.keyed(3)].map { $0.flatMap { request.alwaysConfirm($0.id)?.rule } }, ["Bash(go test:*)", "Bash(go:*)"])
        XCTAssertNil(request.alwaysConfirm("o"))
        let bare = json.replacingOccurrences(of: #"(?s),\s*"card":\{.*\]\}\}"#, with: "}", options: .regularExpression)
        guard case .permission(let fallback) = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(bare.utf8)).content else { return XCTFail("not a permission") }
        XCTAssertNil(fallback.card)
        XCTAssertEqual(["a1", "a2"].map { fallback.alwaysConfirm($0)?.rule }, ["Bash(go test:*)", "Bash(go:*)"])
    }

    func testDockSaysWhatTheCardHides() throws {
        let json = #"""
        [{"kind":"permission","id":"p1","toolCall":{"kind":"execute","rawInput":{"command":"a\rb\u2028c\nd\ne"}},"options":[]}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        guard case .permission(let request) = entries[0].content else { return XCTFail("not a permission") }
        XCTAssertEqual(request.hiddenInDock, "2 more lines")
        XCTAssertEqual(request.subject, "a\nb\nc\nd\ne")
    }

    func testAskingSessionDocksTheLastQuestionAndDropsItFromTheMessage() throws {
        let json = #"""
        [{"kind":"agent","id":"a1","text":"Checkout runs in three steps.\n\n- **validate** checks the cart\n\nShould I add retries to the charge step?"}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        let session = AgentSession(id: "s", agent: "claude", status: .done, afk: true, asks: true)
        let screen = SessionScreen(entries: entries, session: session)
        XCTAssertEqual(session.state.label, "Asking you")
        XCTAssertEqual(session.headerDetail, "")
        XCTAssertEqual(session.composerPlaceholder, "Answer Claude…")
        XCTAssertEqual(screen.dock, .question("Should I add retries to the charge step?"))
        guard case .agent(_, let blocks, _, _) = screen.rows.first else { return XCTFail("unexpected rows \(screen.rows)") }
        XCTAssertEqual(blocks.count, 2)
    }

    func testAgentRowCarriesTheMessageDocLinksAndCards() throws {
        let json = #"""
        [{"kind":"agent","id":"a1","text":"Wrote `docs/plan.md`.","docLinks":{"docs/plan.md":"/w/docs/plan.md"},"docCards":[{"path":"/w/docs/plan.md","kind":"markdown","title":"Plan","rel":"docs/plan.md","presented":true}]}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        guard case .agent(_, _, let docLinks, let docCards) = SessionScreen(entries: entries, session: nil).rows.first else { return XCTFail("no agent row") }
        XCTAssertEqual(docLinks, ["docs/plan.md": "/w/docs/plan.md"])
        XCTAssertEqual(docCards, [DocCard(path: "/w/docs/plan.md", kind: .markdown, title: "Plan", rel: "docs/plan.md", presented: true)])
    }

    func testMessageThatIsOnlyAPlanFoldsIntoThePlanCard() throws {
        let json = #"""
        [{"kind":"agent","id":"a1","text":"Steps:\n\n1. One\n2. Two"},
         {"kind":"plan","id":"p1","plan":{"entries":[{"content":"One","status":"pending"}]}}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        XCTAssertEqual(SessionScreen(entries: entries, session: nil).rows.map(\.id), ["plan/p1"])
    }

    func testRunningSessionComposerQueues() {
        let session = AgentSession(id: "s", agent: "claude", status: .running)
        XCTAssertEqual(session.state, .running)
        XCTAssertEqual(session.composerPlaceholder, "Queue a message")
        XCTAssertEqual(session.sendLabel, "Queue")
        XCTAssertEqual(session.composerCaption(), "Sends after the current turn · Stop is in •••")
        XCTAssertEqual(session.composerCaption(stopInHeader: true), "Sends after the current turn")
    }
}
