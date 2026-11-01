import XCTest

import ClubmateKit

final class HeaderActionsTests: XCTestCase {
    private func button(_ id: String, _ variant: String, _ extra: [String: JSONValue] = [:]) -> SDUINode {
        SDUINode(type: "button", id: id, props: ["label": .string(id), "variant": .string(variant)].merging(extra) { $1 })
    }

    func testOverflowShowsFirstThreeNonDangerAndMovesTheRestButKeepsTheFieldOut() {
        let header = SDUINode(type: "header", props: ["title": .string("Job")], children: [
            SDUINode(type: "textField", id: "filter", props: [:]), button("run", "primary"), button("delete", "danger"), button("pause", "secondary"), button("reveal", "ghost"), button("json", "ghost"),
        ])

        let overflow = header.headerOverflow()
        XCTAssertEqual(overflow.shown.map(\.id), ["run", "pause", "reveal"])
        XCTAssertEqual(overflow.more.map(\.id), ["delete", "json"])
        XCTAssertEqual(header.headerField?.id, "filter")
    }

    func testOverflowButtonsAlwaysGoToTheMenuAndASessionsStopStaysOutOfTheBar() {
        let header = SDUINode(type: "header", props: ["title": .string("Run"), "status": .string("running")], children: [
            button("stop", "ghost", ["action": .string("stop")]), button("open", "ghost"), button("remove", "danger", ["overflow": .bool(true)]),
        ])

        XCTAssertEqual(header.headerStop?.id, "stop")
        XCTAssertEqual(header.headerBar.map(\.id), ["open"])
        XCTAssertEqual(header.headerOverflow().shown.map(\.id), ["open"])
        XCTAssertEqual(header.headerOverflow().more.map(\.id), ["remove"])
        XCTAssertEqual(header.headerStatusTone, "accent")
    }
}
