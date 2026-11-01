import XCTest

import ClubmateKit

final class PermissionKeysTests: XCTestCase {
    private let legacy = [
        SDUIPermissionOption(id: "a", label: "Yes", kind: .allowOnce),
        SDUIPermissionOption(id: "b", label: "Yes, always", kind: .allowAlways),
        SDUIPermissionOption(id: "c", label: "No", kind: .rejectOnce),
    ]
    private let keyed = [
        SDUIPermissionOption(id: "once", label: "Allow once", kind: .allowOnce, shortcut: "opt+1"),
        SDUIPermissionOption(id: "always", label: "Always allow…", kind: .allowAlways, shortcut: "opt+2", rule: "Bash(go test:*)"),
        SDUIPermissionOption(id: "never", label: "Always reject", kind: .rejectAlways, menu: true),
        SDUIPermissionOption(id: "reject", label: "Reject", kind: .rejectOnce, shortcut: "opt+3"),
    ]

    func testOptionDigitPicksTheOptionInButtonOrder() {
        XCTAssertEqual(PermissionKeys.option(characters: "3", option: true, otherModifiers: false, options: legacy), 2)
        XCTAssertNil(PermissionKeys.option(characters: "1", option: false, otherModifiers: false, options: legacy))
        XCTAssertNil(PermissionKeys.option(characters: "1", option: true, otherModifiers: true, options: legacy))
        XCTAssertNil(PermissionKeys.option(characters: "4", option: true, otherModifiers: false, options: legacy))
    }

    func testBareKeysOnlyWhenAllowed() {
        XCTAssertNil(PermissionKeys.option(characters: "1", option: false, otherModifiers: false, bare: false, options: legacy))
        XCTAssertEqual(PermissionKeys.option(characters: "3", option: false, otherModifiers: false, bare: true, options: legacy), 2)
        XCTAssertEqual(PermissionKeys.option(characters: "y", option: false, otherModifiers: false, bare: true, options: legacy), 0)
        XCTAssertEqual(PermissionKeys.option(characters: "n", option: false, otherModifiers: false, bare: true, options: legacy), 2)
        XCTAssertNil(PermissionKeys.option(characters: "y", option: true, otherModifiers: false, bare: true, options: legacy))
    }

    func testWithoutShortcutsPersistentChoicesHaveNoKey() {
        XCTAssertNil(PermissionKeys.option(characters: "2", option: true, otherModifiers: false, options: legacy))
        XCTAssertNil(PermissionKeys.label(1, in: legacy))
    }

    func testShortcutsKeyTheirOptionAndSkipTheMenu() {
        XCTAssertEqual(PermissionKeys.option(characters: "2", option: true, otherModifiers: false, options: keyed), 1)
        XCTAssertEqual(PermissionKeys.option(characters: "3", option: false, otherModifiers: false, bare: true, options: keyed), 3)
        XCTAssertEqual(PermissionKeys.option(characters: "n", option: false, otherModifiers: false, bare: true, options: keyed), 3)
        XCTAssertEqual(PermissionKeys.label(1, in: keyed), "⌥2")
        XCTAssertNil(PermissionKeys.label(2, in: keyed))
    }
}
