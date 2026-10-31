import XCTest

import ClubmateKit

final class HeaderActionsTests: XCTestCase {
    func testOverflowShowsFirstThreeNonDangerAndMovesTheRest() {
        func button(_ id: String, _ variant: String) -> SDUINode {
            SDUINode(type: "button", id: id, props: ["label": .string(id), "variant": .string(variant)])
        }
        let header = SDUINode(type: "header", props: ["title": .string("Job")], children: [
            button("run", "primary"), button("delete", "danger"), button("pause", "secondary"), button("reveal", "ghost"), button("json", "ghost"),
        ])

        let overflow = header.headerOverflow()
        XCTAssertEqual(overflow.shown.map(\.id), ["run", "pause", "reveal"])
        XCTAssertEqual(overflow.more.map(\.id), ["delete", "json"])
    }
}
