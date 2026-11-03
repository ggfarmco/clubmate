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

    func testTreeItemBadgesChangesAndMenuKeyDecode() {
        let tree = SDUINode(type: "tree", props: ["items": .array([
            .object([
                "id": .string("file:a.go"), "label": .string("a.go"),
                "badges": .array([.object(["text": .string("new"), "tone": .string("success")])]),
                "changes": .object(["added": .number(10), "removed": .number(0)]),
                "menu": .array([.object(["action": .string("toggleReviewed"), "label": .string("Mark reviewed"), "key": .string("r")])]),
            ]),
        ])])
        let item = tree.treeItems()[0]

        XCTAssertEqual(item.badges, [SDUIHeaderBadge(.object(["text": .string("new"), "tone": .string("success")]))])
        XCTAssertEqual(item.changes, SDUIChanges(.object(["added": .number(10), "removed": .number(0)])))
        XCTAssertEqual(item.item.menu.first?.key, "r")
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
