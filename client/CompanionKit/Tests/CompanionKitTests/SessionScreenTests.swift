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
        guard case .agent(_, let blocks, _, _, _, _) = screen.rows[2], case .tools(_, let tools) = screen.rows[4],
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
        guard case .agent(_, let blocks, _, _, _, _) = screen.rows.first else { return XCTFail("unexpected rows \(screen.rows)") }
        XCTAssertEqual(blocks.count, 2)
    }

    func testAgentRowCarriesTheMessageDocLinksCardsImagesAndImageLinks() throws {
        let json = #"""
        [{"kind":"agent","id":"a1","text":"Wrote `docs/plan.md`.","docLinks":{"docs/plan.md":"/w/docs/plan.md"},"docCards":[{"path":"/w/docs/plan.md","kind":"markdown","title":"Plan","rel":"docs/plan.md","presented":true}],"images":[{"path":"/w/shot.png","rel":"shot.png","width":640,"height":480}],"imageLinks":{"shot.png":"/w/shot.png"}}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        guard case .agent(_, _, let docLinks, let docCards, let images, let imageLinks) = SessionScreen(entries: entries, session: nil).rows.first else { return XCTFail("no agent row") }
        XCTAssertEqual(docLinks, ["docs/plan.md": "/w/docs/plan.md"])
        XCTAssertEqual(docCards, [DocCard(path: "/w/docs/plan.md", kind: .markdown, title: "Plan", rel: "docs/plan.md", presented: true)])
        XCTAssertEqual(images, [ImageRef(path: "/w/shot.png", rel: "shot.png", width: 640, height: 480)])
        XCTAssertEqual(imageLinks, ["shot.png": "/w/shot.png"])
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

    func testOpenCardWaitsForABlockingRequestAndLeavesWithTheCount() throws {
        let open = #"{"kind":"openItems","id":"open-1","turn":1,"items":[{"id":"oi-1","kind":"offer","label":"Add a screen","quote":"Q","state":"open"}]}"#
        let permission = #"{"kind":"permission","id":"permission-1","turn":2,"toolCall":{"title":"go test"},"options":[]}"#
        let entries = try [open, permission].map { try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data($0.utf8)) }
        let dock = { (session: AgentSession) in SessionScreen(entries: entries, session: session).dock }
        guard case .permission = dock(AgentSession(id: "s", status: .waiting, pending: ["permission-1"], openCount: 1)) else { return XCTFail("a pending permission docks first") }
        guard case .open = dock(AgentSession(id: "s", status: .done, openCount: 1)) else { return XCTFail("the card docks once nothing waits") }
        XCTAssertNil(dock(AgentSession(id: "s", status: .done)))
        XCTAssertNil(dock(AgentSession(id: "s", status: .closed, openCount: 1)))
    }

    func testOpenCardShowsThePluginsNumbersAcrossTurns() throws {
        let json = #"""
        [{"kind":"openItems","id":"open-1","turn":1,"items":[{"id":"oi-1","kind":"decision","header":"Lists","state":"open","number":1},{"id":"oi-2","kind":"offer","label":"A","state":"open","number":3}]},
         {"kind":"openItems","id":"open-2","turn":2,"items":[{"id":"oi-3","kind":"decision","header":"Keys","state":"open","number":2},{"id":"oi-4","kind":"offer","label":"B","state":"open","number":3}]}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        guard case .open(let card) = SessionScreen(entries: entries, session: AgentSession(id: "s", status: .done, openCount: 4)).dock else { return XCTFail("no card") }
        XCTAssertEqual(card.sections.map(\.number), [1, 2, 3])
        XCTAssertEqual(card.items.map(\.number), [1, 3, 2, 3])
    }

    func testHiddenToolCallLeavesNoRowAndIsNotCounted() throws {
        let json = #"""
        [{"kind":"tool","id":"a","tool":{"title":"Read a.go","kind":"read","status":"completed"}},
         {"kind":"tool","id":"open","hidden":true,"tool":{"title":"mcp__clubmate__agents_open_items","kind":"other","status":"completed"}},
         {"kind":"tool","id":"b","tool":{"title":"Read b.go","kind":"read","status":"completed"}}]
        """#
        let entries = try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: Data(json.utf8))
        let rows = SessionScreen(entries: entries, session: AgentSession(id: "s", status: .done)).rows
        guard rows.count == 1, case .tools(_, let group) = rows[0] else { return XCTFail("unexpected rows \(rows)") }
        XCTAssertEqual(group.title, "2 actions")
        XCTAssertTrue(SessionScreen(entries: [entries[1]], session: AgentSession(id: "s", status: .done)).rows.isEmpty)
    }

    func testResolvedOpenItemsLeaveOneQuietRow() throws {
        let json = #"{"kind":"openItems","id":"open-1","turn":1,"overflow":[211],"items":[{"id":"oi-1","kind":"decision","header":"Lists","state":"answered"},{"id":"oi-2","kind":"offer","label":"A","state":"no"},{"id":"oi-3","kind":"offer","label":"B","state":"backlog","task":210}]}"#
        let entry = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.utf8))
        guard case .openClosed(_, let items) = SessionScreen(entries: [entry], session: AgentSession(id: "s", status: .done)).rows.first else { return XCTFail("no row") }
        XCTAssertEqual(items.summary, "1 answered · 1 declined · 2 in the backlog: #210, #211")
    }

    func testARequestAnsweredOnTheMacDocksAPlainRow() throws {
        let json = """
        {"id": "agt_1", "agent": "claude", "workspace": "w", "worktree": "/w", "status": "done", "needsYou": true,
         "pending": ["cr_1"], "pendingSummary": {"kind": "connect", "text": "Connect 2 repos?", "onMac": true, "at": "2026-11-06T10:00:00Z"}}
        """
        let session = try JSONDecoder.clubmate.decode(AgentSession.self, from: Data(json.utf8))

        guard case .onMac(let text) = SessionScreen(entries: [], session: session).dock else { return XCTFail("no row") }

        XCTAssertEqual(text, "Connect 2 repos?")
    }

    func testAFailedRepoIsAPlainRowAndNoQuestion() throws {
        let json = """
        {"id": "agt_1", "agent": "claude", "workspace": "w", "worktree": "/w", "status": "done", "needsYou": true,
         "pendingSummary": {"kind": "failed", "text": "ads failed", "onMac": true, "at": "2026-11-06T10:00:00Z"}}
        """
        let session = try JSONDecoder.clubmate.decode(AgentSession.self, from: Data(json.utf8))

        guard case .onMac(let text) = SessionScreen(entries: [], session: session).dock else { return XCTFail("no row") }

        XCTAssertEqual(text, "ads failed")
        XCTAssertEqual(session.state, .waitingForYou)
    }
}
