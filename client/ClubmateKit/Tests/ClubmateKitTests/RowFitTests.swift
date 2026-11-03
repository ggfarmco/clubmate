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
        let squeezed = widths(220)
        XCTAssertEqual(squeezed.label, 120)
        XCTAssertEqual(squeezed.detail, 80)
    }

    func testDetailHidesWhenBothMinimaDoNotFit() {
        let fit = widths(180)
        XCTAssertEqual(fit.label, 180)
        XCTAssertNil(fit.detail)
        XCTAssertEqual(widths(80, label: 60, detail: nil).label, 60)
    }
}
