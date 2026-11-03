import XCTest

import ClubmateKit

@MainActor
final class PaneFocusTests: XCTestCase {
    func testOnlyTheFocusedPaneClearsIt() {
        let focus = PaneFocus()
        focus.report(.tree(field: true), from: "sidebar", focused: true)
        focus.report(.table, from: "main", focused: true)
        focus.report(.tree(field: true), from: "sidebar", focused: false)

        XCTAssertEqual(focus.hints, "j/k · h/l fold")

        focus.report(.table, from: "main", focused: false)

        XCTAssertNil(focus.pane)
    }

    func testMainPaneWithAComposerHandsItFocus() {
        XCTAssertTrue(SDUINode(type: "stack", children: [SDUINode(type: "transcript"), SDUINode(type: "composer")]).hasComposer)
        XCTAssertFalse(SDUINode(type: "stack", children: [SDUINode(type: "transcript", children: [SDUINode(type: "composer")])]).hasComposer)
    }

    func testHintsFollowThePaneState() {
        XCTAssertEqual(FocusedPane.tree(field: false).hints, "j/k · ↩ open")
        XCTAssertEqual(FocusedPane.list.hints, "j/k · ↩ open")
        XCTAssertEqual(FocusedPane.graph(history: true).hints, "j/k · ↩ open · h history")
        XCTAssertEqual(FocusedPane.graph(history: false).hints, "j/k · ↩ open")
        XCTAssertEqual(FocusedPane.graph(history: true, edges: true).hints, "j/k · ↩ open · tab edge · h history")
        XCTAssertEqual(FocusedPane.graph(history: true, edges: true, output: true).hints, "j/k · ↩ open · tab edge · o output · h history")
        XCTAssertEqual(FocusedPane.composer(running: false, queues: false).hints, "⌘↩ send")
        XCTAssertEqual(FocusedPane.composer(running: true, queues: true).hints, "⌘↩ queue · Esc stop")
        XCTAssertNil(FocusedPane.terminal.hints)
    }

    func testFontSlotFollowsFocusAndSkipsTerminals() {
        let focus = PaneFocus()
        focus.report(.terminal, from: "t", slot: "editor-nvim.main", focused: true)
        XCTAssertNil(focus.fontSlot)

        focus.report(.composer(running: false, queues: false), from: "c", slot: "agents.main", focused: true)
        XCTAssertEqual(focus.fontSlot, "agents.main")

        focus.report(.field, from: "f", slot: "agents.sidebar.field", focused: true)
        XCTAssertEqual(focus.fontSlot, "agents.sidebar")
    }
}
