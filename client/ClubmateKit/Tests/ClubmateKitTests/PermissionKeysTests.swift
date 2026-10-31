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

    func testBareKeysOnlyWhenAllowed() {
        XCTAssertNil(PermissionKeys.option(characters: "1", option: false, otherModifiers: false, bare: false, kinds: kinds))
        XCTAssertEqual(PermissionKeys.option(characters: "3", option: false, otherModifiers: false, bare: true, kinds: kinds), 2)
        XCTAssertEqual(PermissionKeys.option(characters: "y", option: false, otherModifiers: false, bare: true, kinds: kinds), 0)
        XCTAssertEqual(PermissionKeys.option(characters: "n", option: false, otherModifiers: false, bare: true, kinds: kinds), 2)
        XCTAssertNil(PermissionKeys.option(characters: "y", option: true, otherModifiers: false, bare: true, kinds: kinds))
        XCTAssertNil(PermissionKeys.option(characters: "2", option: false, otherModifiers: false, bare: true, kinds: kinds))
    }

    func testPersistentChoicesHaveNoKey() {
        XCTAssertNil(PermissionKeys.option(characters: "2", option: true, otherModifiers: false, kinds: kinds))
        XCTAssertNil(PermissionKeys.label(1, kind: .allowAlways))
    }
}
