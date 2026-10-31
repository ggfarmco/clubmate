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

    func testEnumsDecodeKnownValuesAndFallBackOnUnknown() throws {
        struct Wire: Decodable, Equatable {
            let session: AgentSessionStatus
            let kind: ToolKind
            let status: ToolStatus
            let plan: PlanStatus
            let role: MessageRole
            let permission: PermissionKind
        }
        let decode = { (json: String) in try JSONDecoder().decode([Wire].self, from: Data(json.utf8)) }

        XCTAssertEqual(try decode(#"[{"session":"waiting","kind":"switch_mode","status":"in_progress","plan":"completed","role":"thought","permission":"allow_once"},{"session":"zombie","kind":"teleport","status":42,"plan":"blocked","role":"system","permission":"maybe"}]"#), [
            Wire(session: .waiting, kind: .switchMode, status: .inProgress, plan: .completed, role: .thought, permission: .allowOnce),
            Wire(session: .other, kind: .other, status: .other, plan: .pending, role: .agent, permission: .other),
        ])
    }
}
