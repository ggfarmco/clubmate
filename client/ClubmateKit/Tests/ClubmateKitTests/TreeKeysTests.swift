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
            (4, false, .collapse),
            (123, false, .collapse),
            (36, false, .open),
            (31, false, .activate),
            (53, false, .focusMain),
        ]
        for (keyCode, control, action) in rows {
            for side in SidebarSide.allCases {
                XCTAssertEqual(TreeKeys.action(keyCode: keyCode, control: control, command: false, side: side), action, "\(keyCode) control=\(control) \(side)")
            }
        }
        XCTAssertNil(TreeKeys.action(keyCode: 40, control: false, command: true, side: .left))
    }

    func testTreeEdgeKeyFollowsSide() {
        XCTAssertEqual(TreeKeys.action(keyCode: 37, control: true, command: false, side: .left), .focusMain)
        XCTAssertNil(TreeKeys.action(keyCode: 4, control: true, command: false, side: .left))
        XCTAssertEqual(TreeKeys.action(keyCode: 4, control: true, command: false, side: .right), .focusMain)
        XCTAssertNil(TreeKeys.action(keyCode: 37, control: true, command: false, side: .right))
    }

    func testOpenResolve() {
        let rows = [
            TreeRow(id: "/r/a", depth: 0, parent: nil, hasChildren: true, expanded: true),
            TreeRow(id: "/r/a/x.go", depth: 1, parent: "/r/a", hasChildren: false, expanded: false),
            TreeRow(id: "/r/b", depth: 0, parent: nil, hasChildren: true, expanded: false),
            TreeRow(id: "/r/y.go", depth: 0, parent: nil, hasChildren: false, expanded: false),
        ]
        let cases: [(String, TreeCommand)] = [
            ("/r/a", .collapse("/r/a")),
            ("/r/b", .expand("/r/b")),
            ("/r/y.go", .select("/r/y.go")),
        ]
        for (cursor, command) in cases {
            XCTAssertEqual(TreeCursor.resolve(.open, cursor: cursor, in: rows), command, "open at \(cursor)")
        }
    }

    func testChordKeys() {
        XCTAssertEqual([8, 0].map(TreeChord.resolve), [.focusField, nil])
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
        XCTAssertEqual(TreeCursor.scrollTarget("a1x", from: "a1y", in: rows, limit: 4), "a")
        XCTAssertEqual(TreeCursor.scrollTarget("a1y", from: "a1x", in: rows, limit: 4), "a1y")
    }

    func testCommandConfirmDecoded() throws {
        let commands = try JSONDecoder().decode([Command].self, from: Data(#"""
        [{"id":"alpha.discard","title":"Discard","plugin":"alpha","hidden":false,"confirm":"Discard changes?"},
         {"id":"alpha.stage","title":"Stage","plugin":"alpha","hidden":false}]
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
