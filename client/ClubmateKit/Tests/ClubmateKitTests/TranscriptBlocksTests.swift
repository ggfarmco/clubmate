import XCTest

import ClubmateKit

@MainActor
final class TranscriptBlocksTests: XCTestCase {
    private func message(_ id: String, _ role: String, _ text: String, open: Bool = false) -> SDUINode {
        SDUINode(type: "message", id: id, props: ["role": .string(role), "text": .string(text), "open": .bool(open)])
    }

    private func tool(_ id: String, _ title: String, diff: String? = nil, file: Bool = false) -> SDUINode {
        var props: [String: JSONValue] = ["title": .string(title)]
        if let diff { props["body"] = .object(["format": .string("diff"), "text": .string(diff)]) }
        if file { props["actions"] = .array([.object(["action": .string("openLocation"), "label": .string("Open in nvim")])]) }
        return SDUINode(type: "toolCard", id: id, props: props)
    }

    private var transcript: SDUINode {
        SDUINode(type: "transcript", id: "s/transcript", children: [
            SDUINode(type: "button", id: "s/earlier", props: ["label": .string("Show 5 earlier")]),
            message("s/p", "user", "fix it"),
            message("s/t", "thought", "hmm", open: true),
            tool("s/r", "Read `a.go`"),
            tool("s/e", "Edit `a.go`", diff: "@@ -1 +1 @@\n-a\n+b\n", file: true),
            message("s/a", "agent", "Done."),
        ])
    }

    func testBlocksFollowTheFoldsAndEndWithTheDockedCard() {
        let card = SDUINode(type: "stack", children: [transcript, SDUINode(type: "permissionCard", id: "s/permission/1"), SDUINode(type: "composer")])
        let docked = TranscriptBlocks.docked(in: card)
        let closed = TranscriptBlocks.blocks(transcript, folds: TranscriptFolds(), docked: docked)
        XCTAssertEqual(closed.map(\.kind), [.earlier, .prompt, .thought, .actions, .answer, .card])
        XCTAssertEqual(closed[2].expanded, true)

        var tools = ToolExpansionState()
        tools.toggle(group: ["s/r", "s/e"])
        let open = TranscriptBlocks.blocks(transcript, folds: TranscriptFolds(tools: tools, thoughts: ["s/t": false]))
        XCTAssertEqual(open.map(\.id), ["s/earlier", "s/p", "s/t", "s/r/actions", "s/r", "s/e", "s/a"])
        XCTAssertEqual(open[2].expanded, false)
        XCTAssertEqual(open[5], TranscriptBlock(id: "s/e", kind: .action, folds: true, parent: "s/r/actions", file: true))
    }

    func testCopyGivesMarkdownOfTheBlock() {
        XCTAssertEqual(TranscriptBlocks.markdown("s/a", in: transcript), "Done.")
        XCTAssertEqual(TranscriptBlocks.markdown("s/e", in: transcript), "**Edit `a.go`**\n\n```diff\n@@ -1 +1 @@\n-a\n+b\n```")
        XCTAssertEqual(TranscriptBlocks.markdown("s/r/actions", in: transcript), "2 actions: 2 other tools\n\n- Read `a.go`\n- Edit `a.go`")
    }

    func testTheFocusedEditTakesFullDiffElseTheNewest() {
        let first = tool("s/e1", "Edit `a.go`", diff: "-a\n+b\n")
        let review: JSONValue = .array([.object(["action": .string("review"), "label": .string("Full diff")])])
        let one = SDUINode(type: first.type, id: first.id, props: first.props.merging(["actions": review]) { $1 })
        let two = SDUINode(type: first.type, id: "s/e2", props: one.props)
        let node = SDUINode(type: "transcript", children: [one, two])
        XCTAssertEqual(node.fullDiffCard(focused: "s/e1")?.id, "s/e1")
        XCTAssertEqual(node.fullDiffCard(focused: nil)?.id, "s/e2")
    }

    func testThoughtFoldsOutsideTheTranscriptArePruned() {
        let kept = TranscriptFolds.pruned(["s/t": false, "s/gone": true, "other/t": true], in: transcript)
        XCTAssertEqual(kept, ["s/t": false, "other/t": true])
    }

    func testHelpShowsTheFocusedPanesKeys() {
        XCTAssertEqual(PaneKeysHelp.groups(.transcript).map(\.id), ["transcript", "permissions", "agents-panes"])
        XCTAssertEqual(PaneKeysHelp.groups(.tree(field: true)).map(\.id), ["trees", "agents-panes"])
    }
}
