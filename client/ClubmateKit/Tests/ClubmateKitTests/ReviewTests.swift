import Foundation
import XCTest

import ClubmateKit

final class ReviewTests: XCTestCase {
    func testReviewSlotDecodesHeaderAndColumns() throws {
        let tree = try JSONDecoder().decode(SDUINode.self, from: Data(#"""
        {"type":"tree","props":{
          "items":[{"id":"a.go","label":"a.go","badge":"M","additions":12,"deletions":3,"reviewed":true,"comments":2}],
          "review":{"base":"main","head":"DUE-970-cards","baseRef":"main","headPath":"/r/.worktrees/due","mode":"module",
            "progress":"16 files · 6 hidden · 7/16 reviewed","warning":"","error":"",
            "chips":[{"id":"tests","label":"Tests","on":true},{"id":"generated","label":"Generated","on":false}]}}}
        """#.utf8))

        let header = try XCTUnwrap(ReviewSlot.header(SDUINode(type: "scroll", children: [tree])))
        let item = try XCTUnwrap(tree.treeItems().first)

        XCTAssertEqual(header.title, "main ◂ DUE-970-cards (working tree)")
        XCTAssertEqual(header.fraction, 7.0 / 16.0)
        XCTAssertEqual(header.chips.map(\.on), [true, false])
        XCTAssertEqual(ReviewCommand.filter(header.chips[0]), CommandInvocation(id: "git.review-filter", args: .object(["tests": .bool(false)])))
        XCTAssertEqual([item.additions, item.deletions, item.comments], [12, 3, 2])
        XCTAssertTrue(item.reviewed)
        XCTAssertTrue(ReviewSlot.isActive(tree))
        XCTAssertFalse(ReviewSlot.isActive(SDUINode(type: "empty")))
    }

    func testReviewKeys() {
        let rows = [
            TreeRow(id: "a", depth: 0, parent: nil, hasChildren: true, expanded: true),
            TreeRow(id: "a/x.go", depth: 1, parent: "a", hasChildren: false, expanded: false),
            TreeRow(id: "b", depth: 0, parent: nil, hasChildren: true, expanded: true),
            TreeRow(id: "b/c", depth: 1, parent: "b", hasChildren: true, expanded: true),
            TreeRow(id: "b/c/y.go", depth: 2, parent: "b/c", hasChildren: false, expanded: false),
        ]
        let cases: [(UInt16, Bool, String?, UIAction.Trigger?)] = [
            (15, false, "a/x.go", .reviewed("a/x.go")),
            (15, false, "a", nil),
            (15, false, nil, nil),
            (45, false, nil, .next(1)),
            (45, true, "a", .next(-1)),
            (38, false, "a", nil),
        ]
        for (keyCode, shift, cursor, trigger) in cases {
            XCTAssertEqual(ReviewKeys.trigger(keyCode: keyCode, shift: shift, control: false, command: false, option: false, cursor: cursor, rows: rows), trigger, "\(keyCode) \(shift) \(cursor ?? "-")")
        }
        XCTAssertNil(ReviewKeys.trigger(keyCode: 45, shift: false, control: true, command: false, option: false, cursor: nil, rows: rows))

        let tree = SDUINode(type: "tree", id: "review")
        let event = UIAction.event(for: tree, trigger: .next(-1), slot: "git.review", projectID: "p")
        XCTAssertEqual([event.action, event.nodeId], ["next", "review"])
        XCTAssertEqual(event.value, .number(-1))
        let collapse = UIAction.event(for: tree, trigger: .collapseOthers("a/x.go"), slot: "git.review", projectID: "p")
        XCTAssertEqual([collapse.action], ["collapse-others"])
        XCTAssertEqual(collapse.value, .string("a/x.go"))
    }
}
