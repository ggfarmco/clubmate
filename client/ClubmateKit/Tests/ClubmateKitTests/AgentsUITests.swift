import XCTest

import ClubmateKit

@MainActor
final class AgentsUITests: XCTestCase {
    func testMarkdownBlocks() {
        let source = "# Title\n\nSome *text*\nmore\n- one\n  - two\n3. third\n\n```go\nfunc main() {\n\n}\n```\nafter"

        XCTAssertEqual(Markdown.blocks(source), [
            .heading(level: 1, text: "Title"),
            .paragraph("Some *text*\nmore"),
            .list([
                MarkdownListItem(marker: "•", depth: 0, text: "one"),
                MarkdownListItem(marker: "•", depth: 1, text: "two"),
                MarkdownListItem(marker: "3.", depth: 0, text: "third"),
            ]),
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

    func testComposerKeys() throws {
        let model = ComposerModel()
        model.commands = ["review", "compact", "context"].map { ComposerCommand(name: $0, description: "", hint: "") }
        let send = try XCTUnwrap(ComposerKey(keyCode: 36, characters: "\r", command: true, control: false, option: false, shift: false))
        let escape = try XCTUnwrap(ComposerKey(keyCode: 53, characters: "\u{1b}", command: false, control: false, option: false, shift: false))
        let down = try XCTUnwrap(ComposerKey(keyCode: 125, characters: nil, command: false, control: false, option: false, shift: false))
        let accept = try XCTUnwrap(ComposerKey(keyCode: 36, characters: "\r", command: false, control: false, option: false, shift: false))

        model.text = "  fix it \n"
        XCTAssertEqual(model.handle(send), .send("fix it"))
        XCTAssertNil(model.handle(escape))
        model.running = true
        XCTAssertEqual(model.handle(escape), .stop)

        model.text = "/co"
        XCTAssertEqual(model.suggestions.map(\.name), ["compact", "context"])
        XCTAssertEqual(model.handle(down), .handled)
        XCTAssertEqual(model.handle(accept), .handled)
        XCTAssertEqual(model.text, "/context ")
        XCTAssertEqual(model.suggestions, [])
        XCTAssertNil(model.handle(accept))
    }

    func testAgentEventMapping() {
        let card = SDUINode(type: "permissionCard", id: "s/permission/p1")
        let custom = SDUINode(type: "permissionCard", id: "s/permission/p2", props: ["action": .string("answer")])
        let composer = SDUINode(type: "composer", id: "s/composer/0")

        let answer = UIAction.event(for: card, trigger: .permission(request: "p1", option: "allow"), slot: "agents.main", projectID: "w")
        XCTAssertEqual(answer, UIEvent(projectId: "w", slot: "agents.main", nodeId: "s/permission/p1", action: "permission", value: .object(["request": .string("p1"), "option": .string("allow")])))
        XCTAssertEqual(UIAction.event(for: custom, trigger: .permission(request: "p2", option: "deny"), slot: "agents.main", projectID: "w").action, "answer")
        let send = UIAction.event(for: composer, trigger: .named("send", .object(["text": .string("hi")])), slot: "agents.main", projectID: "w")
        XCTAssertEqual(send, UIEvent(projectId: "w", slot: "agents.main", nodeId: "s/composer/0", action: "send", value: .object(["text": .string("hi")])))
    }

    func testTranscriptGroupsAdjacentToolCards() {
        func card(_ id: String, _ status: String = "completed") -> SDUINode {
            SDUINode(type: "toolCard", id: id, props: ["kind": .string("read"), "status": .string(status)])
        }
        let transcript = SDUINode(type: "transcript", children: [
            SDUINode(type: "message", id: "m1"),
            card("t1"), card("t2"),
            SDUINode(type: "permissionCard", id: "p1"),
            card("t3"),
            card("t4", "failed"),
            card("t5"), card("t6", "in_progress"), card("t7"),
        ])

        let items = transcript.transcriptItems()

        XCTAssertEqual(items.map(\.id), ["m1", "t1/actions", "p1", "t3", "t4", "t5/actions"])
        guard case .action = items[3], case .node = items[4], case let .actions(_, cards) = items[5] else { return XCTFail() }
        XCTAssertEqual(cards.map(\.id), ["t5", "t6", "t7"])
    }

    func testToolActionsSummary() {
        func card(_ kind: String, _ status: String = "completed") -> SDUINode {
            SDUINode(type: "toolCard", props: ["kind": .string(kind), "status": .string(status)])
        }
        let done = ToolActionsSummary(["read", "search", "execute", "read", "edit", "search", "execute", "read"].map { card($0) })
        let running = ToolActionsSummary([card("execute", "in_progress")])

        XCTAssertEqual(done.text, "8 actions: read 3 files, 2 searches, 2 commands, edited 1 file")
        XCTAssertFalse(done.running)
        XCTAssertEqual(running.text, "1 action: 1 command")
        XCTAssertTrue(running.running)
    }
}
