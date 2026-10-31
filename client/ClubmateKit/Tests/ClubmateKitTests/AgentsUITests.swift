import XCTest

import ClubmateKit

@MainActor
final class AgentsUITests: XCTestCase {
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
        XCTAssertEqual(model.handle(send), .send("fix it"))

        model.text = "/co"
        XCTAssertEqual(model.suggestions.map(\.name), ["compact", "context"])
        XCTAssertEqual(model.handle(down), .handled)
        XCTAssertEqual(model.handle(accept), .handled)
        XCTAssertEqual(model.text, "/context ")
        XCTAssertEqual(model.suggestions, [])
        XCTAssertNil(model.handle(accept))
    }

    func testComposerDraftsSurviveRecreation() {
        let drafts = ComposerDrafts()
        drafts.model(for: "a/composer/0").text = "draft a"
        drafts.model(for: "b/composer/0").text = "draft b"
        XCTAssertEqual(drafts.model(for: "a/composer/0").text, "draft a")
        XCTAssertEqual(drafts.model(for: "b/composer/0").text, "draft b")
        drafts.clear("a/composer/0", sent: "draft a")
        XCTAssertEqual(drafts.model(for: "a/composer/0").text, "")
        XCTAssertEqual(drafts.model(for: "b/composer/0").text, "draft b")
        drafts.model(for: "b/composer/0").text = "next prompt"
        drafts.clear("b/composer/0", sent: "draft b")
        XCTAssertEqual(drafts.model(for: "b/composer/0").text, "next prompt")
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

    func testComposerPickerLabels() {
        let option: (String, String) -> JSONValue = { .object(["value": .string($0), "label": .string($1)]) }
        let composer = SDUINode(type: "composer", id: "s/composer/0", props: ["pickers": .array([
            .object(["id": .string("model"), "value": .string("opus"), "options": .array([option("opus", "Opus 5.5")])]),
            .object(["id": .string("effort"), "title": .string("Effort"), "value": .string("high"), "options": .array([option("high", "High")])]),
            .object(["id": .string("advisor"), "title": .string("Advisor"), "value": .string(""), "options": .array([option("", "Default")])]),
        ])])

        XCTAssertEqual(composer.pickers().map(\.label), ["Opus 5.5", "Effort: High", "Advisor: Default"])
        XCTAssertEqual(SDUIPicker.summary(composer.pickers()), "Opus 5.5 · High")
        let merging = SDUINode(type: "composer", props: ["pickers": .array([
            .object(["id": .string("finish"), "title": .string("Finish"), "value": .string("merge"), "options": .array([option("ask", "Ask"), option("merge", "Merge to main")])]),
        ])])
        XCTAssertEqual(SDUIPicker.summary(merging.pickers()), "Merge to main")
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

        XCTAssertEqual(items.map(\.id), ["m1", "t1/actions", "p1", "t3/actions"])
        guard case let .actions(_, cards) = items[3] else { return XCTFail() }
        XCTAssertEqual(cards.map(\.id), ["t3", "t4", "t5", "t6", "t7"])
    }

    func testToolActionsSummary() {
        func card(_ kind: String, _ status: String = "completed") -> SDUINode {
            SDUINode(type: "toolCard", props: ["kind": .string(kind), "status": .string(status), "title": .string("go test ./...")])
        }
        let done = ToolActionsSummary(["read", "search", "execute", "read", "edit", "search", "execute", "read"].map { card($0) })
        let running = ToolActionsSummary([card("execute", "failed"), card("execute", "in_progress")])

        XCTAssertEqual(done.text, "8 actions: read 3 files, 2 searches, 2 commands, edited 1 file")
        XCTAssertNil(done.running)
        XCTAssertEqual(done.failed, 0)
        XCTAssertEqual(running.text, "2 actions: 2 commands")
        XCTAssertEqual(running.failed, 1)
        XCTAssertEqual(running.running, "go test ./...")
    }

    func testEditLineShowsFileName() {
        func card(_ kind: String, _ title: String) -> SDUINode {
            SDUINode(type: "toolCard", props: ["kind": .string(kind), "title": .string(title)])
        }

        let edit = ToolLineTitle(card("edit", "Edit `/repo/my docs/view.go`"))
        let move = ToolLineTitle(card("move", "Move /repo/a.go to /repo/b/d.go."))

        XCTAssertEqual([edit.text, edit.help], ["view.go", "/repo/my docs/view.go"])
        XCTAssertEqual([move.text, move.help], ["a.go → d.go", "/repo/a.go → /repo/b/d.go"])
        XCTAssertEqual(ToolLineTitle(card("edit", "Write /repo/x.go:")).text, "x.go")
        XCTAssertEqual(ToolLineTitle(card("edit", "Edit a.go")).text, "Edit a.go")
        XCTAssertNil(ToolLineTitle(card("execute", "ls /tmp/x")).help)
    }

    func testToolExpansionFollowsStreamingGroups() {
        var state = ToolExpansionState()

        state.toggle(card: "s/tool/a")
        XCTAssertTrue(state.isExpanded(group: ["s/tool/a", "s/tool/b"]))
        state.toggle(group: ["s/tool/a", "s/tool/b"])
        XCTAssertFalse(state.isExpanded(card: "s/tool/a"))
        state.toggle(group: ["s/tool/a", "s/tool/b", "s/tool/c", "s/tool/d"])
        XCTAssertTrue(state.isExpanded(group: ["s/tool/c", "s/tool/d"]))
        state.prune(SDUINode(type: "transcript", id: "s/transcript", children: [SDUINode(type: "toolCard", id: "s/tool/d")]))
        XCTAssertFalse(state.isExpanded(group: ["s/tool/a", "s/tool/b", "s/tool/c"]))
        XCTAssertTrue(state.isExpanded(group: ["s/tool/d", "s/tool/e"]))
    }

    func testFlowRowsWrapOnlyWhenTheNextChipDoesNotFit() {
        let sizes = [CGSize(width: 60, height: 16), CGSize(width: 80, height: 16), CGSize(width: 70, height: 18), CGSize(width: 250, height: 16)]
        let origins = FlowRows.origins(sizes: sizes, width: 200, spacing: 4)
        XCTAssertEqual(origins, [CGPoint(x: 0, y: 0), CGPoint(x: 64, y: 0), CGPoint(x: 0, y: 20), CGPoint(x: 0, y: 42)])
        XCTAssertEqual(FlowRows.size(sizes: sizes, width: 200, spacing: 4), CGSize(width: 250, height: 58))
    }
}
