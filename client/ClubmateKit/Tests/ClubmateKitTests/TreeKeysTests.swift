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
            (47, false, .focus),
            (51, false, .focusUp),
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

    func testOpenAndFocusResolve() {
        let rows = [
            TreeRow(id: "/r/a", depth: 0, parent: nil, hasChildren: true, expanded: true),
            TreeRow(id: "/r/a/x.go", depth: 1, parent: "/r/a", hasChildren: false, expanded: false),
            TreeRow(id: "/r/b", depth: 0, parent: nil, hasChildren: true, expanded: false),
            TreeRow(id: "/r/y.go", depth: 0, parent: nil, hasChildren: false, expanded: false),
        ]
        let crumbs = ["/r", "/r/p"]
        let cases: [(TreeKeyAction, String?, [String], TreeCommand?)] = [
            (.open, "/r/a", [], .collapse("/r/a")),
            (.open, "/r/b", [], .expand("/r/b")),
            (.open, "/r/y.go", [], .select("/r/y.go")),
            (.focus, "/r/b", [], .focus("/r/b")),
            (.focus, "/r/a/x.go", [], .focus("/r/a")),
            (.focus, "/r/y.go", [], nil),
            (.focusUp, nil, crumbs, .focus("/r")),
            (.focusUp, nil, [], nil),
        ]
        for (action, cursor, breadcrumb, command) in cases {
            XCTAssertEqual(TreeCursor.resolve(action, cursor: cursor, in: rows, breadcrumb: breadcrumb), command, "\(action) at \(cursor ?? "nil")")
        }
    }

    func testTabAndChordKeys() {
        XCTAssertEqual(TreeKeys.action(keyCode: 43, control: false, command: false, side: .left, shift: true), .cycleTab(-1))
        XCTAssertEqual(TreeKeys.action(keyCode: 47, control: false, command: false, side: .left, shift: true), .cycleTab(1))
        XCTAssertEqual(TreeCursor.resolve(.cycleTab(1), cursor: nil, in: []), .cycleTab(1))
        XCTAssertEqual([0, 32, 15, 8, 9].map(TreeChord.resolve), [.menu("stage"), .menu("unstage"), .menu("discard"), .focusField, nil])
    }

    func testCollapseOthersKey() {
        XCTAssertEqual(TreeKeys.action(keyCode: 27, control: false, command: false, side: .left), .collapseOthers)
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
        XCTAssertEqual(TreeCursor.scrollTarget("a1x", from: "a1y", in: rows, limit: 4), "a")
        XCTAssertEqual(TreeCursor.scrollTarget("a1y", from: "a1x", in: rows, limit: 4), "a1y")
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
