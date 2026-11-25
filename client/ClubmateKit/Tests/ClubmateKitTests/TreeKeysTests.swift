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
        XCTAssertEqual(tree.treeItems().flatMap(\.visibleItems).map(\.id), rows.map(\.id))
        XCTAssertEqual(TreeCursor.move("a1", by: 1, in: rows), "b")
        XCTAssertEqual(TreeCursor.move("b", by: 1, in: rows), "b")
        XCTAssertEqual(TreeCursor.move(nil, by: 1, in: rows), "a")
    }

    func testJKSkipSectionLabels() throws {
        let tree = try JSONDecoder().decode(SDUINode.self, from: Data("""
        {"type": "tree", "props": {"items": [
          {"id": "a", "label": "A"},
          {"id": "sec", "label": "guides", "action": true, "hasChildren": true, "expanded": true, "children": [
            {"id": "sub", "label": "plain", "action": true, "hasChildren": true, "expanded": true, "children": [{"id": "x", "label": "X"}]}
          ]},
          {"id": "done", "label": "Done", "action": true, "hasChildren": true}
        ]}}
        """.utf8))
        let rows = TreeCursor.rows(tree.treeItems())

        XCTAssertEqual(rows.filter(\.heading).map(\.id), ["sec", "sub", "done"])
        XCTAssertEqual(TreeCursor.move("a", by: 1, in: rows), "x")
        XCTAssertEqual(TreeCursor.move("x", by: -1, in: rows), "a")
        XCTAssertEqual(TreeCursor.resolve(.collapse, cursor: "x", in: rows), .cursor("sub"))
        XCTAssertEqual(TreeCursor.move("x", by: 1, in: rows), "done")
        XCTAssertEqual(TreeCursor.resolve(.expand, cursor: "done", in: rows), .expand("done"))
    }

    func testFoldedParentKeepsItsPinnedChildren() throws {
        let tree = try JSONDecoder().decode(JSONValue.self, from: Data("""
        {"id": "task", "label": "Task", "hasChildren": true, "expanded": false, "children": [{"id": "failed", "label": "auth", "pinned": true}, {"id": "merged", "label": "ads"}]}
        """.utf8))
        let item = try XCTUnwrap(SDUITreeItem(tree))
        XCTAssertEqual(item.shownChildren.map(\.id), ["failed"])
        XCTAssertEqual(TreeCursor.rows([item]).map(\.id), ["task", "failed"])
        XCTAssertEqual(item.visibleItems.map(\.id), ["task", "failed"])
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

    func testTreeCursorSettles() {
        let old = ["a", "more", "b"]
        XCTAssertEqual(TreeCursor.settle("more", activated: true, from: old, to: ["a", "c1", "c2", "b"]), "c1")
        XCTAssertEqual(TreeCursor.settle("b", activated: false, from: old, to: ["a", "more"]), "more")
        XCTAssertEqual(TreeCursor.settle("more", activated: true, from: old, to: ["a", "c1", "more", "b"]), "c1")
        XCTAssertNil(TreeCursor.settle("more", activated: false, from: old, to: ["a", "c1", "more", "b"]))
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
            (53, false, .escape),
            (33, true, .escape),
        ]
        for (keyCode, control, action) in rows {
            for side in SidebarSide.allCases {
                XCTAssertEqual(TreeKeys.action(keyCode: keyCode, control: control, command: false, side: side), action, "\(keyCode) control=\(control) \(side)")
            }
        }
        XCTAssertNil(TreeKeys.action(keyCode: 40, control: false, command: true, side: .left))
    }

    func testAngleBracketsCycleTheSidebarTabs() {
        XCTAssertEqual(TreeKeys.action(keyCode: 43, control: false, command: false, side: .left, shift: true), .cycleTabs(-1))
        XCTAssertEqual(TreeKeys.action(keyCode: 47, control: false, command: false, side: .left, shift: true), .cycleTabs(1))
        XCTAssertNil(TreeKeys.action(keyCode: 47, control: false, command: false, side: .left))
        let sidebar = SDUINode(type: "stack", children: [SDUINode(type: "tabs", id: "filter"), SDUINode(type: "tree", id: "sessions")])
        XCTAssertEqual(sidebar.firstTabs?.id, "filter")
        let ids = ["all", "needs", "running"]
        XCTAssertEqual(TabCycle.next(ids, from: "running", by: 1), "all")
        XCTAssertEqual(TabCycle.next(ids, from: "all", by: -1), "running")
        XCTAssertEqual(TabCycle.next(ids, from: nil, by: 1), "all")
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
        XCTAssertEqual([8, 0, 18].map { TreeChord.resolve($0) }, [.focusField, nil, nil])
        XCTAssertEqual([18, 25, 37, 45, 17, 34, 0].map { TreeChord.resolve($0, sessions: true) }, [.jump(1), .jump(9), .last, .needs(1), .tree, .composer, nil])
        XCTAssertEqual([45, 8].map { TreeChord.resolve($0, shift: true, sessions: true) }, [.needs(-1), nil])
    }

    func testShiftedKeyAfterGPassesOutsideAgents() {
        XCTAssertTrue(TreeChord.passes(command: false, control: false, option: false, shift: true, sessions: false))
        XCTAssertFalse(TreeChord.passes(command: false, control: false, option: false, shift: true, sessions: true))
        XCTAssertTrue(TreeChord.passes(command: true, control: false, option: false, shift: false, sessions: true))
        XCTAssertFalse(TreeChord.passes(command: false, control: false, option: false, shift: false, sessions: false))
    }

    func testControlDownLandsTheHeldTreeOnceAndTimesTheDigits() {
        var keys = ControlHoldKeys()
        XCTAssertEqual(keys.flags(control: true, command: false, option: false, shift: false), [.land, .start])
        XCTAssertEqual(keys.flags(control: true, command: true, option: false, shift: false), [.cancel])
        XCTAssertEqual(keys.flags(control: false, command: false, option: false, shift: false), [.cancel])
        XCTAssertEqual(keys.flags(control: true, command: true, option: false, shift: false), [.land, .cancel])
    }

    func testControlTabIsLastSessionOnlyAlone() {
        XCTAssertTrue(SessionKeys.isLast(keyCode: 48, control: true, command: false, option: false, shift: false))
        XCTAssertFalse(SessionKeys.isLast(keyCode: 48, control: true, command: false, option: false, shift: true))
        XCTAssertFalse(SessionKeys.isLast(keyCode: 48, control: false, command: false, option: false, shift: false))
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

    private func docked() -> [TreeRow] {
        let run: JSONValue = .object(["id": .string("run:gr_1"), "label": .string("Review gate"), "open": .string("run"), "hasChildren": .bool(true), "expanded": .bool(true), "children": .array([item("agt_c")])])
        let tree = SDUINode(type: "tree", props: [
            "items": .array([item("wt", expanded: true, children: [run])]),
            "dock": .array([
                .object(["id": .string("gates"), "label": .string("Land gates"), "rows": .array([item("gate-help")]), "cap": .number(6)]),
                .object(["id": .string("graphs"), "label": .string("Graphs"), "expanded": .bool(true), "cap": .number(6), "rows": .array([item("graph:nightly")])]),
            ]),
        ])
        return TreeCursor.rows(tree.treeItems(), dock: tree.treeDock())
    }

    func testJStepsIntoDock() {
        let rows = docked()
        XCTAssertEqual(rows.map(\.id), ["wt", "run:gr_1", "agt_c", "gates", "graphs", "graph:nightly"])
        XCTAssertEqual(TreeCursor.resolve(.move(1), cursor: "agt_c", in: rows), .cursor("gates"))
    }

    func testKStepsBackFromDock() {
        XCTAssertEqual(TreeCursor.resolve(.move(-1), cursor: "gates", in: docked()), .cursor("agt_c"))
    }

    func testReturnOnOpenRowSelects() {
        XCTAssertEqual(TreeCursor.resolve(.open, cursor: "run:gr_1", in: docked()), .openPage("run:gr_1"))
    }

    func testEscFoldsOpenDockRow() {
        let rows = docked()
        XCTAssertEqual(TreeKeys.action(keyCode: 53, control: false, command: false, side: .left), .escape)
        XCTAssertEqual(TreeCursor.resolve(.escape, cursor: "graphs", in: rows), .collapse("graphs"))
        XCTAssertEqual(TreeCursor.resolve(.escape, cursor: "graph:nightly", in: rows), .collapse("graphs"))
        XCTAssertEqual(TreeCursor.resolve(.escape, cursor: "agt_c", in: rows), .focusMain)
        XCTAssertEqual(TreeCursor.resolve(.open, cursor: "gates", in: rows), .expand("gates"))
    }

    func testJWalksGatesGroupedByRepoOnceAndEscFoldsTheDock() {
        func repo(_ id: String, _ gates: [String]) -> JSONValue {
            .object(["id": .string(id), "label": .string(id), "expanded": .bool(true), "children": .array(gates.map { item($0) })])
        }
        let tree = SDUINode(type: "tree", props: [
            "items": .array([]),
            "dock": .array([
                .object(["id": .string("gates"), "label": .string("Gates"), "expanded": .bool(true), "cap": .number(6), "rows": .array([repo("r1", ["g1", "g2"]), repo("r2", ["g3", "g4"])])]),
                .object(["id": .string("graphs"), "label": .string("Graphs"), "cap": .number(6), "rows": .array([item("graph:nightly")])]),
            ]),
        ])
        let rows = TreeCursor.rows(tree.treeItems(), dock: tree.treeDock())

        var cursor: String? = "gates"
        var walked: [String] = []
        for _ in 0..<7 {
            cursor = TreeCursor.move(cursor, by: 1, in: rows)
            walked.append(cursor ?? "")
        }
        XCTAssertEqual(walked, ["r1", "g1", "g2", "r2", "g3", "g4", "graphs"])
        XCTAssertEqual(TreeCursor.resolve(.escape, cursor: "g3", in: rows), .collapse("gates"))
    }

    func testCmdDeleteRemovesTheCursorRow() throws {
        XCTAssertEqual(TreeKeys.action(keyCode: 51, control: false, command: true, side: .left), .remove)
        XCTAssertNil(TreeKeys.action(keyCode: 51, control: false, command: true, side: .left, option: true))
        XCTAssertEqual(TreeCursor.resolve(.remove, cursor: "agt_c", in: docked()), .remove("agt_c"))
        let entry = try XCTUnwrap(SDUIMenuEntry(.object(["action": .string("delete"), "label": .string("Delete Session"), "key": .string("cmd+delete")])))
        XCTAssertEqual(entry.key, "cmd+delete")
    }

    func testMenuEntryKeys() {
        XCTAssertEqual(MenuEntryKey("cmd+delete"), .commandDelete)
        XCTAssertEqual(MenuEntryKey("c"), .plain("c"))
        XCTAssertNil(MenuEntryKey(nil))
    }

    func testFolderCarryingAPageOpensItOnReturnAndFoldsOnArrows() {
        let rows = [TreeRow(id: "dir:app/sdui", depth: 0, parent: nil, hasChildren: true, expanded: true, selects: true)]

        XCTAssertEqual(TreeCursor.resolve(.open, cursor: "dir:app/sdui", in: rows), .select("dir:app/sdui"))
        XCTAssertEqual(TreeCursor.resolve(.collapse, cursor: "dir:app/sdui", in: rows), .collapse("dir:app/sdui"))
    }

    func testReturnOnFolderRowStillFolds() {
        XCTAssertEqual(TreeCursor.resolve(.open, cursor: "wt", in: docked()), .collapse("wt"))
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
