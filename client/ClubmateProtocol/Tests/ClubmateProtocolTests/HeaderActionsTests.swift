import XCTest

import ClubmateProtocol

final class HeaderActionsTests: XCTestCase {
    private func button(_ id: String, _ variant: String, _ extra: [String: JSONValue] = [:]) -> SDUINode {
        SDUINode(type: "button", id: id, props: ["label": .string(id), "variant": .string(variant)].merging(extra) { $1 })
    }

    func testPillIsNotABarButton() {
        let header = SDUINode(type: "header", props: ["title": .string("Task")], children: [
            button("review", "ghost"), SDUINode(type: "button", id: "pill", props: ["label": .string("3 repos"), "pill": .bool(true)]),
        ])

        XCTAssertEqual(header.headerPill?.id, "pill")
        XCTAssertEqual(header.headerBar.map(\.id), ["review"])
    }

    func testReposRowsDecode() {
        let row: JSONValue = .object(["id": .string("a"), "order": .number(2), "name": .string("auth"), "landsBy": .string("merge to main"), "state": .string("failed"), "tone": .string("danger"), "opens": .bool(true), "open": .bool(true)])
        let node = SDUINode(type: "repos", id: "r", props: ["rows": .array([row, .string("junk")]), "summary": .array([.object(["text": .string("1 failed"), "tone": .string("danger")])])])

        XCTAssertEqual(node.reposRows().map(\.order), [2])
        XCTAssertEqual(node.reposRows().first?.open, true)
        XCTAssertEqual(node.reposSummary().first?.tone, "danger")
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
