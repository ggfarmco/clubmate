import XCTest

import ClubmateKit

final class RowFitTests: XCTestCase {
    private func widths(_ available: CGFloat, label: CGFloat = 300, detail: CGFloat? = 60) -> (label: CGFloat, detail: CGFloat?) {
        RowFit.widths(available: available, label: label, detail: detail, gap: 20, keptLabel: 120, keptDetail: 72)
    }

    func testRoomyRowKeepsBothIdeals() {
        let fit = widths(500)
        XCTAssertEqual(fit.label, 300)
        XCTAssertEqual(fit.detail, 60)
    }

    func testShortWordStaysWholeAndTheLabelGivesWay() {
        let cut = widths(350)
        XCTAssertEqual(cut.label, 270)
        XCTAssertEqual(cut.detail, 60)
        let narrow = widths(140)
        XCTAssertEqual(narrow.label, 60)
        XCTAssertEqual(narrow.detail, 60)
        let none = widths(70)
        XCTAssertEqual(none.label, 70)
        XCTAssertNil(none.detail)
    }

    func testLongDetailIsCutOnlyAfterTheLabelKeepsItsShare() {
        let whole = widths(400, detail: 200)
        XCTAssertEqual(whole.label, 180)
        XCTAssertEqual(whole.detail, 200)
        let cut = widths(300, detail: 200)
        XCTAssertEqual(cut.label, 120)
        XCTAssertEqual(cut.detail, 160)
        let hidden = widths(200, detail: 200)
        XCTAssertEqual(hidden.label, 200)
        XCTAssertNil(hidden.detail)
    }

    func testBadgesFoldBeforeTheDetailIsCutOrTheLabelGetsShort() {
        XCTAssertTrue(RowFit.badgesFit(available: 500, label: 300, badges: 80, detail: 60, gap: 8, keptLabel: 120, keptDetail: 72))
        XCTAssertFalse(RowFit.badgesFit(available: 260, label: 300, badges: 80, detail: 60, gap: 8, keptLabel: 120, keptDetail: 72))
        XCTAssertTrue(RowFit.badgesFit(available: 100, label: 300, badges: 80, detail: nil, gap: 8, keptLabel: 120, keptDetail: 72))
        XCTAssertTrue(RowFit.badgesFit(available: 268, label: 300, badges: 80, detail: 60, gap: 8, keptLabel: 120, keptDetail: 72))
        XCTAssertFalse(RowFit.badgesFit(available: 267, label: 300, badges: 80, detail: 60, gap: 8, keptLabel: 120, keptDetail: 72))
    }
}
