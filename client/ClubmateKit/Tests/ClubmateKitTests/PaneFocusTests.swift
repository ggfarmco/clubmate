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
        XCTAssertEqual(FocusedPane.composer(running: false, queues: false).hints, "⌘↩ send")
        XCTAssertEqual(FocusedPane.composer(running: true, queues: true).hints, "⌘↩ queue · Esc stop")
        XCTAssertNil(FocusedPane.terminal.hints)
    }
}
