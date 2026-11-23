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

    func testStep() {
        XCTAssertEqual(SwitcherGrid.step(cursor: 0, count: 5, by: 1), 1)
        XCTAssertEqual(SwitcherGrid.step(cursor: 0, count: 5, by: -1), 0)
        XCTAssertEqual(SwitcherGrid.step(cursor: 4, count: 5, by: 1), 4)
    }
}
