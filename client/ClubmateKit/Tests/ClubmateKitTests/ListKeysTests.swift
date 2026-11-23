import XCTest

import ClubmateKit

final class ListKeysTests: XCTestCase {
    func testFieldKeys() {
        let cases: [(UInt16, ListKeyAction)] = [
            (38, .move(1)), (45, .move(1)), (40, .move(-1)), (35, .move(-1)), (37, .open),
        ]
        for (keyCode, action) in cases {
            XCTAssertEqual(ListKeys.action(keyCode: keyCode, control: true, command: false, option: false), action, "\(keyCode)")
        }
        XCTAssertNil(ListKeys.action(keyCode: 38, control: false, command: false, option: false))
        XCTAssertNil(ListKeys.action(keyCode: 4, control: true, command: false, option: false))
        XCTAssertNil(ListKeys.action(keyCode: 38, control: true, command: true, option: false))
    }

    func testGridKeys() {
        let cases: [(UInt16, SwitcherGrid.Arrow)] = [(4, .left), (38, .down), (40, .up), (37, .right)]
        for (keyCode, arrow) in cases {
            XCTAssertEqual(ListKeys.gridArrow(keyCode: keyCode, control: false, command: false, option: false), arrow, "\(keyCode)")
        }
        XCTAssertNil(ListKeys.gridArrow(keyCode: 38, control: true, command: false, option: false))
    }

    func testWelcomeKeys() {
        let plain: [(UInt16, ListKeyAction)] = [(125, .move(1)), (38, .move(1)), (126, .move(-1)), (40, .move(-1)), (36, .open), (76, .open)]
        for (keyCode, action) in plain {
            XCTAssertEqual(ListKeys.welcome(keyCode: keyCode, control: false, command: false, option: false), action, "\(keyCode)")
        }
        XCTAssertEqual(ListKeys.welcome(keyCode: 45, control: true, command: false, option: false), .move(1))
        XCTAssertNil(ListKeys.welcome(keyCode: 125, control: false, command: true, option: false))
    }
}
