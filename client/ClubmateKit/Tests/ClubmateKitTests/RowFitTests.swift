import XCTest

import ClubmateKit

final class RowFitTests: XCTestCase {
    private func widths(_ available: CGFloat, label: CGFloat = 300, detail: CGFloat? = 90) -> (label: CGFloat, detail: CGFloat?) {
        RowFit.widths(available: available, label: label, detail: detail, gap: 20, keptLabel: 120, keptDetail: 72)
    }

    func testRoomyRowKeepsBothIdeals() {
        let fit = widths(500)
        XCTAssertEqual(fit.label, 300)
        XCTAssertEqual(fit.detail, 90)
    }

    func testLongLabelTruncatesBeforeTheDetail() {
        let full = widths(400)
        XCTAssertEqual(full.label, 290)
        XCTAssertEqual(full.detail, 90)
        let capped = widths(300, detail: 200)
        XCTAssertEqual(capped.label, 120)
        XCTAssertEqual(capped.detail, 160)
        let halved = widths(200)
        XCTAssertEqual(halved.label, 90)
        XCTAssertEqual(halved.detail, 90)
    }

    func testDetailHidesWhenBothMinimaDoNotFit() {
        let fit = widths(160)
        XCTAssertEqual(fit.label, 160)
        XCTAssertNil(fit.detail)
        XCTAssertEqual(widths(80, label: 60, detail: nil).label, 60)
    }

    func testBadgesFoldBeforeTheDetailIsCut() {
        XCTAssertTrue(RowFit.badgesFit(available: 500, label: 300, badges: 80, detail: 90, gap: 8, keptLabel: 120, keptDetail: 72))
        XCTAssertFalse(RowFit.badgesFit(available: 260, label: 300, badges: 80, detail: 90, gap: 8, keptLabel: 120, keptDetail: 72))
        XCTAssertTrue(RowFit.badgesFit(available: 100, label: 300, badges: 80, detail: nil, gap: 8, keptLabel: 120, keptDetail: 72))
        XCTAssertTrue(RowFit.badgesFit(available: 267.6, label: 300, badges: 80, detail: 90, gap: 8, keptLabel: 120, keptDetail: 72))
        XCTAssertFalse(RowFit.badgesFit(available: 266.6, label: 300, badges: 80, detail: 90, gap: 8, keptLabel: 120, keptDetail: 72))
    }
}
