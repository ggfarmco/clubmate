import XCTest

import ClubmateKit

final class CircledNumberTests: XCTestCase {
    func testOneToTwentyOnly() {
        XCTAssertEqual(CircledNumber.value("①".utf16.first!), 1)
        XCTAssertEqual(CircledNumber.value("⑳".utf16.first!), 20)
        XCTAssertNil(CircledNumber.value("㉑".utf16.first!))
        XCTAssertNil(CircledNumber.value("1".utf16.first!))
    }
}
