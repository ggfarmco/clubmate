import Foundation
import XCTest

import ClubmateProtocol

final class ClubmateProtocolTests: XCTestCase {
    func testMarkdownBlocks() {
        let source = "# Title\n\nSome *text*\nmore\n- one\n  - two\n3. third\n> quoted\n> twice\nafter quote\n\n```go\nfunc main() {\n\n}\n```\nafter"

        XCTAssertEqual(Markdown.blocks(source), [
            .heading(level: 1, text: "Title"),
            .paragraph("Some *text*\nmore"),
            .list([
                MarkdownListItem(marker: "•", depth: 0, text: "one"),
                MarkdownListItem(marker: "•", depth: 1, text: "two"),
                MarkdownListItem(marker: "3.", depth: 0, text: "third"),
            ]),
            .quote("quoted\ntwice"),
            .paragraph("after quote"),
            .code(language: "go", text: "func main() {\n\n}"),
            .paragraph("after"),
        ])
        XCTAssertEqual(Markdown.blocks("```\nstreaming"), [.code(language: "", text: "streaming")])
    }

    func testMarkdownLinksOnlyOpenWebAndMail() throws {
        for allowed in ["https://example.com", "HTTP://example.com/a", "mailto:a@b.c"] {
            XCTAssertTrue(MarkdownLinkPolicy.allows(try XCTUnwrap(URL(string: allowed))), allowed)
        }
        for denied in ["file:///etc/passwd", "x-callback-url://run", "clubmate://open", "javascript:alert(1)", "relative/path"] {
            XCTAssertFalse(MarkdownLinkPolicy.allows(try XCTUnwrap(URL(string: denied))), denied)
        }
    }

    func testDiffLineKinds() {
        let text = "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n ctx\n\n"
        XCTAssertEqual(DiffLine.parse(text), [
            DiffLine(text: "--- a/x", kind: .context),
            DiffLine(text: "+++ b/x", kind: .context),
            DiffLine(text: "@@ -1 +1 @@", kind: .hunk),
            DiffLine(text: "-old", kind: .removed),
            DiffLine(text: "+new", kind: .added),
            DiffLine(text: " ctx", kind: .context),
            DiffLine(text: "", kind: .context),
        ])
    }

    func testAgentSessionDecodesSessionInfoAndBareRecord() throws {
        let info = #"{"id":"agt_1","workspace":"prj_1","worktree":"/src/shop","agent":"claude","agentSessionId":"c-1","parentSession":"agt_0","title":"Fix","titleSet":true,"status":"waiting","mode":"plan","finish":"merge","afk":true,"createdAt":"2026-10-30T09:41:00.123456789Z","updatedAt":"2026-10-30T09:42:00Z","closed":false,"asks":true,"notify":"muted","error":"boom","pending":["permission-1"],"notifyEffective":"needs-input"}"#
        let session = try JSONDecoder().decode(AgentSession.self, from: Data(info.utf8))
        XCTAssertEqual(session, AgentSession(
            id: "agt_1", workspace: "prj_1", worktree: "/src/shop", agent: "claude", agentSessionId: "c-1", parentSession: "agt_0",
            title: "Fix", titleSet: true, status: .waiting, mode: "plan", finish: .merge, afk: true,
            createdAt: try XCTUnwrap(Date.parseRFC3339("2026-10-30T09:41:00.123Z")), updatedAt: try XCTUnwrap(Date.parseRFC3339("2026-10-30T09:42:00Z")),
            asks: true, notify: .muted, error: "boom", pending: ["permission-1"], notifyEffective: .needsInput
        ))
        XCTAssertEqual(try JSONDecoder().decode(AgentSession.self, from: JSONEncoder().encode(session)), session)

        let bare = try JSONDecoder().decode(AgentSession.self, from: Data(#"{"id":"agt_2","createdAt":"2026-10-30T09:41:00Z","error":""}"#.utf8))
        XCTAssertEqual(bare, AgentSession(id: "agt_2", createdAt: try XCTUnwrap(Date.parseRFC3339("2026-10-30T09:41:00Z"))))
        XCTAssertEqual(bare.finish, .ask)
        XCTAssertEqual(bare.notifyEffective, .all)
    }

    func testEnumsDecodeKnownValuesAndFallBackOnUnknown() throws {
        struct Wire: Decodable, Equatable {
            let session: AgentSessionStatus
            let kind: ToolKind
            let status: ToolStatus
            let plan: PlanStatus
            let role: MessageRole
            let permission: PermissionKind
            let outcome: PermissionOutcomeKind
            let entry: TranscriptEntryKind
            let finish: AgentFinish
            let notify: NotificationPolicy
        }
        let decode = { (json: String) in try JSONDecoder().decode([Wire].self, from: Data(json.utf8)) }

        XCTAssertEqual(try decode(#"[{"session":"waiting","kind":"switch_mode","status":"in_progress","plan":"completed","role":"thought","permission":"allow_once","outcome":"cancelled","entry":"permission","finish":"merge","notify":"muted"},{"session":"zombie","kind":"teleport","status":42,"plan":"blocked","role":"system","permission":"maybe","outcome":"timeout","entry":"diff","finish":"pr","notify":"loud"}]"#), [
            Wire(session: .waiting, kind: .switchMode, status: .inProgress, plan: .completed, role: .thought, permission: .allowOnce, outcome: .cancelled, entry: .permission, finish: .merge, notify: .muted),
            Wire(session: .other, kind: .other, status: .other, plan: .pending, role: .agent, permission: .other, outcome: .other, entry: .other, finish: .ask, notify: .all),
        ])
    }

    func testBusEventDecodesCoreAndRemoteEnvelopes() throws {
        let decode = { (json: String) in try JSONDecoder().decode(BusEvent.self, from: Data(json.utf8)) }

        XCTAssertEqual(try decode(#"{"seq":7,"topic":"worktree.changed","source":"plugin:git","time":"2026-10-31T10:00:00Z","payload":{"workspace":"w1"}}"#),
                       BusEvent(seq: 7, topic: "worktree.changed", workspace: "w1", payload: .object(["workspace": .string("w1")])))
        XCTAssertEqual(try decode(#"{"seq":3,"topic":"notify.posted","workspace":"w2","payload":{"workspace":"other"}}"#),
                       BusEvent(seq: 3, topic: "notify.posted", workspace: "w2", payload: .object(["workspace": .string("other")])))
        XCTAssertEqual(try decode(#"{"seq":4,"topic":"plugin.changed"}"#), BusEvent(seq: 4, topic: "plugin.changed", payload: .null))
    }
}
