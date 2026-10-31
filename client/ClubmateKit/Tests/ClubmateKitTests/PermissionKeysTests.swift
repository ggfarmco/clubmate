import XCTest

import ClubmateKit

final class PermissionKeysTests: XCTestCase {
    private let kinds: [PermissionKind] = [.allowOnce, .allowAlways, .rejectOnce]

    func testOptionDigitPicksTheOptionInButtonOrder() {
        XCTAssertEqual(PermissionKeys.option(characters: "3", option: true, otherModifiers: false, kinds: kinds), 2)
        XCTAssertNil(PermissionKeys.option(characters: "1", option: false, otherModifiers: false, kinds: kinds))
        XCTAssertNil(PermissionKeys.option(characters: "1", option: true, otherModifiers: true, kinds: kinds))
        XCTAssertNil(PermissionKeys.option(characters: "4", option: true, otherModifiers: false, kinds: kinds))
    }

    func testPersistentChoicesHaveNoKey() {
        XCTAssertNil(PermissionKeys.option(characters: "2", option: true, otherModifiers: false, kinds: kinds))
        XCTAssertNil(PermissionKeys.label(1, kind: .allowAlways))
    }
}
