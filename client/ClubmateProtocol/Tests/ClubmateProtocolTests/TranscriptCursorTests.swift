import XCTest

import ClubmateProtocol

final class TranscriptCursorTests: XCTestCase {
    private let folded: [TranscriptBlock] = [
        TranscriptBlock(id: "more", kind: .earlier),
        TranscriptBlock(id: "p", kind: .prompt),
        TranscriptBlock(id: "t", kind: .thought, folds: true),
        TranscriptBlock(id: "g", kind: .actions, folds: true),
        TranscriptBlock(id: "a", kind: .answer),
    ]

    private let open: [TranscriptBlock] = [
        TranscriptBlock(id: "p", kind: .prompt),
        TranscriptBlock(id: "g", kind: .actions, folds: true, expanded: true),
        TranscriptBlock(id: "g1", kind: .action, folds: true, parent: "g", file: true),
        TranscriptBlock(id: "g2", kind: .action, parent: "g"),
        TranscriptBlock(id: "a", kind: .answer),
        TranscriptBlock(id: "c", kind: .card),
    ]

    func testFocusArrivesOnTheNewestBlockAndMovesClamp() {
        XCTAssertEqual(TranscriptCursor.resolve(.move(1), cursor: nil, in: folded), .cursor("a"))
        XCTAssertEqual(TranscriptCursor.resolve(.move(-1), cursor: "a", in: folded), .cursor("g"))
        XCTAssertEqual(TranscriptCursor.resolve(.move(1), cursor: "a", in: folded), .cursor("a"))
        XCTAssertEqual(TranscriptCursor.resolve(.move(-1), cursor: "g1", in: open), .cursor("g"))
    }

    func testFirstPressesShowEarlierAndLastFollows() {
        XCTAssertEqual(TranscriptCursor.resolve(.first, cursor: "a", in: folded), .top("p", earlier: "more"))
        XCTAssertEqual(TranscriptCursor.resolve(.first, cursor: "a", in: open), .top("p", earlier: nil))
        XCTAssertEqual(TranscriptCursor.resolve(.last, cursor: "p", in: open), .bottom("c"))
    }

    func testUnfoldOpensThenEntersAndFoldClosesFromAChild() {
        XCTAssertEqual(TranscriptCursor.resolve(.unfold, cursor: "g", in: folded), .expand("g"))
        XCTAssertEqual(TranscriptCursor.resolve(.unfold, cursor: "g", in: open), .cursor("g1"))
        XCTAssertEqual(TranscriptCursor.resolve(.unfold, cursor: "a", in: open), nil)
        XCTAssertEqual(TranscriptCursor.resolve(.fold, cursor: "g2", in: open), .collapse("g", cursor: "g"))
        XCTAssertEqual(TranscriptCursor.resolve(.fold, cursor: "g", in: open), .collapse("g", cursor: "g"))
    }

    func testReturnTogglesAFoldAndPressesACard() {
        XCTAssertEqual(TranscriptCursor.resolve(.toggle, cursor: "t", in: folded), .expand("t"))
        XCTAssertEqual(TranscriptCursor.resolve(.toggle, cursor: "g", in: open), .collapse("g", cursor: "g"))
        XCTAssertEqual(TranscriptCursor.resolve(.toggle, cursor: "c", in: open), .press("c"))
        XCTAssertEqual(TranscriptCursor.resolve(.toggle, cursor: "more", in: folded), .press("more"))
        XCTAssertNil(TranscriptCursor.resolve(.toggle, cursor: "a", in: open))
    }

    func testCursorSettlesOnTheParentOrTheBlockBefore() {
        XCTAssertEqual(TranscriptCursor.settle("g2", from: open, to: folded), "g")
        XCTAssertEqual(TranscriptCursor.settle("c", from: open, to: Array(open.dropLast())), "a")
        XCTAssertEqual(TranscriptCursor.settle("p", from: open, to: folded), "p")
    }

    func testOnlyBlocksBelowCountAsNewAndRowsLoadedAboveDoNot() {
        let earlier = TranscriptBlock(id: "s/earlier", kind: .earlier)
        let grown = [earlier, TranscriptBlock(id: "p0", kind: .prompt), TranscriptBlock(id: "a0", kind: .answer)] + open + [TranscriptBlock(id: "p2", kind: .prompt)]
        XCTAssertEqual(TranscriptCursor.follow("a", from: [earlier] + open, to: grown), TranscriptFollow(cursor: "a", follows: false, newer: 1))
    }

    func testTheNewestBlockAndTheDockedCardFollowNewOutput() {
        let docked = open.dropLast() + [TranscriptBlock(id: "dock", kind: .card)]
        let grown = open.dropLast() + [TranscriptBlock(id: "a2", kind: .answer), TranscriptBlock(id: "dock", kind: .card)]
        XCTAssertEqual(TranscriptCursor.follow("dock", from: Array(docked), to: Array(grown)), TranscriptFollow(cursor: "dock", follows: true, newer: 0))
        XCTAssertEqual(TranscriptCursor.follow("c", from: open, to: open + [TranscriptBlock(id: "a2", kind: .answer)]), TranscriptFollow(cursor: "a2", follows: true, newer: 0))
    }
}
