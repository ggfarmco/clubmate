import XCTest

import ClubmateKit

final class SwitcherGridTests: XCTestCase {
    func testGridMove() {
        func move(_ highlight: Int, _ key: SwitcherGrid.Arrow) -> Int {
            SwitcherGrid.move(highlight: highlight, count: 5, columns: 3, key: key)
        }
        XCTAssertEqual(move(0, .right), 1)
        XCTAssertEqual(move(2, .right), 2)
        XCTAssertEqual(move(1, .down), 4)
        XCTAssertEqual(move(4, .down), 4)
        XCTAssertEqual(move(2, .down), 5)
        XCTAssertEqual(move(0, .left), 0)
        XCTAssertEqual(move(4, .up), 1)
    }

    func testPickerMove() {
        func move(_ cursor: Int, _ key: SwitcherGrid.Arrow, count: Int = 5) -> Int {
            SwitcherGrid.pickerMove(cursor: cursor, count: count, key: key)
        }
        XCTAssertEqual(move(0, .right), 1)
        XCTAssertEqual(move(1, .left), 0)
        XCTAssertEqual(move(0, .down), 2)
        XCTAssertEqual(move(1, .down), 2)
        XCTAssertEqual(move(2, .up), 0)
        XCTAssertEqual(move(3, .up), 2)
        XCTAssertEqual(move(4, .down), 4)
        XCTAssertEqual(move(2, .right), 2)
        XCTAssertEqual(move(0, .down, count: 2), 0)
    }
}
