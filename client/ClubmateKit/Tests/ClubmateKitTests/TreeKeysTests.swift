import Foundation
import XCTest

import ClubmateKit

final class TreeKeysTests: XCTestCase {
    func testTreeCursorMoves() throws {
        let tree = SDUINode(type: "tree", props: ["items": .array([
            item("a", expanded: true, children: [item("a1", children: [item("a1x")])]),
            item("b"),
        ])])

        let rows = TreeCursor.rows(tree.treeItems())

        XCTAssertEqual(rows, [
            TreeRow(id: "a", depth: 0, parent: nil, hasChildren: true, expanded: true),
            TreeRow(id: "a1", depth: 1, parent: "a", hasChildren: true, expanded: false),
            TreeRow(id: "b", depth: 0, parent: nil, hasChildren: false, expanded: false),
        ])
        XCTAssertEqual(TreeCursor.move("a1", by: 1, in: rows), "b")
        XCTAssertEqual(TreeCursor.move("b", by: 1, in: rows), "b")
        XCTAssertEqual(TreeCursor.move(nil, by: 1, in: rows), "a")
    }

    func testTreeCursorResolve() {
        let rows = [
            TreeRow(id: "a", depth: 0, parent: nil, hasChildren: true, expanded: true),
            TreeRow(id: "a1", depth: 1, parent: "a", hasChildren: true, expanded: false),
            TreeRow(id: "b", depth: 0, parent: nil, hasChildren: false, expanded: false),
        ]
        let cases: [(TreeKeyAction, String?, TreeCommand?)] = [
            (.move(1), "a", .cursor("a1")),
            (.move(1), nil, .cursor("a")),
            (.activate, "b", .select("b")),
            (.activate, nil, nil),
            (.expand, "a1", .expand("a1")),
            (.expand, "a", .cursor("a1")),
            (.expand, "b", nil),
            (.collapse, "a", .collapse("a")),
            (.collapse, "a1", .cursor("a")),
            (.collapse, "b", nil),
            (.focusMain, nil, .focusMain),
        ]
        for (action, cursor, command) in cases {
            XCTAssertEqual(TreeCursor.resolve(action, cursor: cursor, in: rows), command, "\(action) at \(cursor ?? "nil")")
        }
    }

    func testTreeKeyActions() {
        let rows: [(UInt16, Bool, TreeKeyAction)] = [
            (38, false, .move(1)),
            (125, false, .move(1)),
            (40, false, .move(-1)),
            (126, false, .move(-1)),
            (37, false, .expand),
            (124, false, .expand),
            (37, true, .focusMain),
            (4, false, .collapse),
            (123, false, .collapse),
            (36, false, .activate),
            (31, false, .activate),
            (53, false, .focusMain),
        ]
        for (keyCode, control, action) in rows {
            XCTAssertEqual(TreeKeys.action(keyCode: keyCode, control: control, command: false), action, "\(keyCode) control=\(control)")
        }
        XCTAssertNil(TreeKeys.action(keyCode: 40, control: false, command: true))
    }

    func testCollapseOthersKey() {
        XCTAssertEqual(TreeKeys.action(keyCode: 27, control: false, command: false), .collapseOthers)
        XCTAssertEqual(TreeCursor.resolve(.collapseOthers, cursor: nil, in: []), .run("files.collapse-others"))
    }

    func testStickyHeaders() {
        let rows = [
            TreeRow(id: "a", depth: 0, parent: nil, hasChildren: true, expanded: true),
            TreeRow(id: "a1", depth: 1, parent: "a", hasChildren: true, expanded: true),
            TreeRow(id: "a1x", depth: 2, parent: "a1", hasChildren: false, expanded: false),
            TreeRow(id: "a1y", depth: 2, parent: "a1", hasChildren: false, expanded: false),
            TreeRow(id: "a2", depth: 1, parent: "a", hasChildren: false, expanded: false),
            TreeRow(id: "b", depth: 0, parent: nil, hasChildren: false, expanded: false),
        ]
        XCTAssertEqual(TreeCursor.sticky(rows, top: 0, limit: 4), [])
        XCTAssertEqual(TreeCursor.sticky(rows, top: 1, limit: 4), ["a", "a1"])
        XCTAssertEqual(TreeCursor.sticky(rows, top: 1, limit: 1), ["a"])
        XCTAssertEqual(TreeCursor.sticky(rows, top: 3, limit: 4), ["a"])
        XCTAssertEqual(TreeCursor.stickyTarget("a1", in: rows, limit: 4), "a")
    }

    func testCommandConfirmDecoded() throws {
        let commands = try JSONDecoder().decode([Command].self, from: Data(#"""
        [{"id":"git.discard","title":"Discard","plugin":"git","hidden":false,"confirm":"Discard changes?"},
         {"id":"git.stage","title":"Stage","plugin":"git","hidden":false}]
        """#.utf8))
        XCTAssertEqual(commands.map(\.confirm), ["Discard changes?", ""])
    }
}

private func item(_ id: String, expanded: Bool = false, children: [JSONValue] = []) -> JSONValue {
    .object([
        "id": .string(id),
        "label": .string(id),
        "hasChildren": .bool(!children.isEmpty),
        "expanded": .bool(expanded),
        "children": .array(children),
    ])
}
