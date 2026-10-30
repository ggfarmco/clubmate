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

    func testNotifierPostsWaitingOncePerRequestWhenUnfocused() {
        var notifier = AgentNotifier()
        let waiting = AgentSessionState(workspace: "w", session: "s", agent: "claude", title: "Fix", status: "waiting", requests: ["permission-1"])
        let running = AgentSessionState(workspace: "w", session: "r", agent: "claude", title: "Run", status: "running", requests: ["permission-9"])

        XCTAssertEqual(notifier.alerts([running], windowKey: false, appActive: false), [])
        XCTAssertEqual(notifier.alerts([waiting], windowKey: true, appActive: true), [])
        XCTAssertEqual(notifier.alerts([waiting], windowKey: true, appActive: false).map(\.request), ["permission-1"])
        XCTAssertEqual(notifier.alerts([waiting], windowKey: false, appActive: false), [])

        var next = waiting
        next.requests = ["permission-1", "permission-2"]
        XCTAssertEqual(notifier.alerts([next], windowKey: false, appActive: true).map(\.request), ["permission-2"])
    }
}
