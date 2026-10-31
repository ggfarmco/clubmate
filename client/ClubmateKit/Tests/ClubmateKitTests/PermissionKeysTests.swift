import XCTest

import ClubmateKit

final class PermissionKeysTests: XCTestCase {
    func testOptionDigitPicksTheOptionInButtonOrder() {
        XCTAssertEqual(PermissionKeys.option(characters: "2", option: true, otherModifiers: false, count: 3), 1)
        XCTAssertNil(PermissionKeys.option(characters: "2", option: false, otherModifiers: false, count: 3))
        XCTAssertNil(PermissionKeys.option(characters: "2", option: true, otherModifiers: true, count: 3))
        XCTAssertNil(PermissionKeys.option(characters: "4", option: true, otherModifiers: false, count: 3))
    }
}
