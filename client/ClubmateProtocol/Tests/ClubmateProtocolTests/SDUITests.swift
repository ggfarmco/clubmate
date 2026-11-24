import Foundation
import XCTest

import ClubmateProtocol

final class SDUITests: XCTestCase {
    func testConfirmActionTitle() {
        let labels: [String?] = ["Discard…", "Delete", nil]
        let titles = labels.map { label in
            SDUINode(type: "button", id: "b", props: label.map { ["label": .string($0)] } ?? [:]).confirmActionTitle()
        }
        XCTAssertEqual(titles, ["Discard", "Delete", "OK"])
    }

    func testDiffLinesDecodeAndFoldOnlyUnchangedRuns() throws {
        let json = #"{"type": "diffLines", "id": "d", "props": {"lines": [{"sign": "", "text": "a"}, {"sign": "", "text": "b"}, {"sign": "-", "text": "c"}, {"sign": "+", "text": "d"}, {"sign": "", "text": "e"}], "fold": true}}"#
        let node = try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8))
        let lines = node.diffLines()

        XCTAssertEqual(lines.map(\.kind), [.context, .context, .removed, .added, .context])
        XCTAssertEqual(DiffLine.rows(lines, fold: node.diffFolds()), [.unchanged(2), .line(lines[2]), .line(lines[3]), .unchanged(1)])
        XCTAssertEqual(DiffLine.rows(lines, fold: false).count, 5)
    }

    func testItemDecodesCaptionAndSection() throws {
        let json = #"{"type": "list", "id": "pick", "props": {"focus": "a", "filter": {"placeholder": "Filter sessions", "minRows": 10}, "items": [{"id": "s", "label": "Suggested", "section": true}, {"id": "a", "label": "Fix login", "caption": "opened this review · 3 m"}, {"id": "b", "label": "Refactor", "caption": "wt-2 · 1 h"}]}}"#
        let list = try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8))
        let items = list.items()

        XCTAssertEqual(items.map(\.section), [true, false, false])
        XCTAssertEqual(items.map(\.caption), [nil, "opened this review · 3 m", "wt-2 · 1 h"])
        XCTAssertEqual(list.string("focus"), "a")

        let filter = try XCTUnwrap(list.listFilter())
        XCTAssertEqual(filter.placeholder, "Filter sessions")
        XCTAssertFalse(filter.shown(rows: 9))
        XCTAssertTrue(filter.shown(rows: 10))
        XCTAssertEqual(items.filter { SDUIListFilter.matches($0, "WT-2") }.map(\.id), ["b"])
    }

    func testTableDecodes() throws {
        let json = """
        {"type": "table", "id": "procs", "props": {
          "columns": [{"id": "name", "title": "Name", "truncate": "middle"}, {"id": "cpu", "title": "CPU", "align": "right", "width": 60, "priority": 2}],
          "rows": [{"id": "42", "depth": 1, "hasChildren": true, "expanded": true, "muted": true, "cells": [
            {"text": "nvim", "spark": {"values": [1, 3], "max": 4}},
            {"text": "35%", "bar": 0.35, "tone": "warn", "since": 1730000000, "action": "showLog", "label": "Show log"}
          ]}],
          "sort": {"column": "cpu", "desc": true},
          "selected": "42"
        }}
        """
        let table = try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8))

        XCTAssertEqual(table.columns(), [
            SDUIColumn(id: "name", title: "Name", align: .left, width: nil, truncate: .middle),
            SDUIColumn(id: "cpu", title: "CPU", align: .right, width: 60, priority: 2),
        ])
        XCTAssertEqual(table.tableRows(), [
            SDUITableRow(id: "42", depth: 1, hasChildren: true, expanded: true, muted: true, cells: [
                SDUICell(text: "nvim", bar: nil, tone: nil, spark: SDUISpark(values: [1, 3], max: 4)),
                SDUICell(text: "35%", bar: 0.35, tone: "warn", spark: nil, since: Date(timeIntervalSince1970: 1_730_000_000), link: .init(action: "showLog", label: "Show log")),
            ]),
        ])
        XCTAssertEqual(SDUICell.elapsed(since: Date(timeIntervalSince1970: 0), now: Date(timeIntervalSince1970: 36 * 3600 + 5 * 60 + 7.9)), "36:05:07")
        XCTAssertEqual(SDUICell.elapsed(since: Date(timeIntervalSince1970: -1e300), now: Date(timeIntervalSince1970: 0)), "2777777:46:40")
        XCTAssertEqual(table.tableSort(), SDUISort(column: "cpu", desc: true))
        XCTAssertEqual(table.string("selected"), "42")
    }

    func testDockRowsFlattenChildrenOneLevelDeeper() throws {
        let json = """
        {"type": "tree", "id": "sessions", "props": {"items": [], "dock": [{"id": "gates", "label": "Gates", "cap": 6, "rows": [
          {"id": "gate-help", "label": "How gates work"},
          {"id": "gate-repo:/src/shop", "label": "shop", "children": [{"id": "gate:gt_1", "label": "into main"}, {"id": "gate:gt_2", "label": "into the parent's branch"}]},
          {"id": "run:gr_1", "label": "Review", "hasChildren": true, "expanded": true, "children": [{"id": "agt_1", "label": "Coder"}]}
        ]}]}}
        """
        let dock = try XCTUnwrap(try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8)).treeDock().first)

        XCTAssertEqual(dock.rows.map(\.id), ["gate-help", "gate-repo:/src/shop", "gate:gt_1", "gate:gt_2", "run:gr_1"])
        XCTAssertEqual(dock.rows.map { dock.depths[$0.id] }, [0, 0, 1, 1, 0])
        XCTAssertTrue(dock.rows.allSatisfy(\.children.isEmpty))
    }

    func testDockRowKeepsItsStateApartFromTheCount() throws {
        let json = #"{"type": "tree", "id": "sessions", "props": {"items": [], "dock": [{"id": "gates", "label": "Gates", "detail": "4", "state": "1 landing"}, {"id": "graphs", "label": "Graphs", "detail": "5", "state": ""}]}}"#
        let dock = try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8)).treeDock()

        XCTAssertEqual(dock.map(\.detail), ["4", "5"])
        XCTAssertEqual(dock.map(\.state), ["1 landing", nil])
    }

    func testSparklineHeightDecodes() throws {
        let sized = try JSONDecoder().decode(SDUINode.self, from: Data(#"{"type": "sparkline", "props": {"values": [1, 3], "max": 4, "height": 64}}"#.utf8))
        XCTAssertEqual(sized.spark(), SDUISpark(values: [1, 3], max: 4))
        XCTAssertEqual(sized.sparklineHeight(), 64)
        XCTAssertNil(SDUINode(type: "sparkline", props: ["values": .array([]), "max": .number(1)]).sparklineHeight())
    }

    func testItemTruncateDecodes() {
        let tree = SDUINode(type: "tree", props: ["items": .array([
            .object(["id": .string("wt"), "label": .string("feat/long"), "truncate": .string("middle")]),
            .object(["id": .string("s"), "label": .string("Session")]),
        ])])

        XCTAssertEqual(tree.treeItems().map(\.item.truncate), [.middle, .tail])
    }

    func testTreeItemBadgesChangesActionAndMenuKeyDecode() {
        let tree = SDUINode(type: "tree", props: ["items": .array([
            .object([
                "id": .string("file:a.go"), "label": .string("a.go"), "action": .bool(true),
                "badges": .array([.object(["text": .string("new"), "tone": .string("success")])]),
                "changes": .object(["added": .number(10), "removed": .number(0)]),
                "menu": .array([.object(["action": .string("toggleReviewed"), "label": .string("Mark reviewed"), "key": .string("r")])]),
            ]),
        ])])
        let item = tree.treeItems()[0]

        XCTAssertEqual(item.badges, [SDUIHeaderBadge(.object(["text": .string("new"), "tone": .string("success")]))])
        XCTAssertEqual(item.changes, SDUIChanges(.object(["added": .number(10), "removed": .number(0)])))
        XCTAssertEqual(item.item.menu.first?.key, "r")
        XCTAssertTrue(item.action)
    }

    func testTreeTapOpensAPageRowAndFoldsAPlainFolder() throws {
        func item(_ json: String) throws -> SDUITreeItem {
            try XCTUnwrap(SDUITreeItem(JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))))
        }

        XCTAssertEqual(try item(#"{"id": "dir:a/g", "label": "G", "mirrorOf": "a/g/index.md", "hasChildren": true}"#).tap, .open("a/g/index.md", unfold: true))
        XCTAssertEqual(try item(#"{"id": "dir:a/g", "label": "G", "mirrorOf": "a/g/index.md", "hasChildren": true, "expanded": true}"#).tap, .open("a/g/index.md", unfold: false))
        XCTAssertEqual(try item(#"{"id": "dir:a/p", "label": "p", "action": true, "hasChildren": true, "expanded": true}"#).tap, .fold(expand: false))
        XCTAssertEqual(try item(#"{"id": "new:a/x.md", "label": "X", "mirrorOf": "a/x.md"}"#).tap, .select("a/x.md"))
        XCTAssertEqual(try item(#"{"id": "gate-repo:/r", "label": "r", "hasChildren": true, "expanded": true}"#).tap, .select("gate-repo:/r"))
    }

    func testTableColumnsDropByPriority() {
        let columns = [
            SDUIColumn(id: "pid", title: "PID", align: .left, width: 60),
            SDUIColumn(id: "mem", title: "MEM", align: .right, width: 80, priority: 1),
            SDUIColumn(id: "hist", title: "HISTORY", align: .left, width: 80, priority: 2),
            SDUIColumn(id: "cmd", title: "COMMAND", align: .left, width: nil),
        ]
        let all = 60 + 80 + 80 + SDUIColumn.minFlexibleWidth + 3 * 10

        XCTAssertEqual(SDUIColumn.visible(columns, width: 0, gutter: 10), [0, 1, 2, 3])
        XCTAssertEqual(SDUIColumn.visible(columns, width: all, gutter: 10), [0, 1, 2, 3])
        XCTAssertEqual(SDUIColumn.visible(columns, width: all - 1, gutter: 10), [0, 1, 3])
        XCTAssertEqual(SDUIColumn.visible(columns, width: 100, gutter: 10), [0, 3])
    }

    func testTableSortToggle() {
        let name = SDUIColumn(id: "name", title: "Name", align: .left, width: nil)
        let cpu = SDUIColumn(id: "cpu", title: "CPU", align: .right, width: 60)

        XCTAssertEqual(SDUISort.next(clicking: name, current: nil), SDUISort(column: "name", desc: false))
        XCTAssertEqual(SDUISort.next(clicking: cpu, current: SDUISort(column: "name", desc: false)), SDUISort(column: "cpu", desc: true))
        XCTAssertEqual(SDUISort.next(clicking: cpu, current: SDUISort(column: "cpu", desc: true)), SDUISort(column: "cpu", desc: false))
    }
}
