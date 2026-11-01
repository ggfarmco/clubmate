import XCTest

import ClubmateKit

final class TableKeysTests: XCTestCase {
    func testTableCursorSkipsGroups() {
        func row(_ id: String, _ depth: Int, group: Bool = false, children: Bool = false) -> SDUITableRow {
            SDUITableRow(id: id, depth: depth, hasChildren: children, expanded: children, muted: false, group: group, cells: [])
        }
        let rows = TableKeys.rows([row("g", 0, group: true, children: true), row("1", 1, children: true), row("2", 2), row("3", 1)])

        XCTAssertEqual(rows, [
            TreeRow(id: "1", depth: 1, parent: nil, hasChildren: true, expanded: true),
            TreeRow(id: "2", depth: 2, parent: "1", hasChildren: false, expanded: false),
            TreeRow(id: "3", depth: 1, parent: nil, hasChildren: false, expanded: false),
        ])
        XCTAssertEqual(TableKeys.action(keyCode: 37, plain: true).flatMap { TreeCursor.resolve($0, cursor: "1", in: rows) }, .cursor("2"))
        XCTAssertEqual(TableKeys.action(keyCode: 4, plain: true).flatMap { TreeCursor.resolve($0, cursor: "2", in: rows) }, .cursor("1"))
        XCTAssertNil(TableKeys.action(keyCode: 40, plain: false))
    }
}
