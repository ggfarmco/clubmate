import XCTest

import ClubmateKit

final class KeyComboTests: XCTestCase {
    func testParse() throws {
        let cases: [(String, UInt16, String, String, [Bool])] = [
            ("opt+return", 36, "\r", "\r", [false, false, true, false]),
            ("cmd+shift+n", 45, "N", "N", [true, false, false, true]),
            ("ctrl+j", 38, "\n", "j", [false, true, false, false]),
            ("shift+2", 19, "@", "@", [false, false, false, true]),
            ("down", 125, "\u{f701}", "\u{f701}", [false, false, false, false]),
        ]
        for (combo, keyCode, characters, ignoring, flags) in cases {
            let key = try XCTUnwrap(KeyCombo(combo), combo)
            XCTAssertEqual(key.keyCode, keyCode, combo)
            XCTAssertEqual(key.characters, characters, combo)
            XCTAssertEqual(key.charactersIgnoringModifiers, ignoring, combo)
            XCTAssertEqual([key.command, key.control, key.option, key.shift], flags, combo)
        }
        for invalid in ["", "cmd+", "hyper+a", "f13", "cmd+shift"] {
            XCTAssertNil(KeyCombo(invalid), invalid)
        }
    }

    func testLabelAndMatch() throws {
        let key = try XCTUnwrap(KeyCombo("cmd+shift+r"))
        XCTAssertEqual(key.label, "⇧⌘R")
        XCTAssertEqual(try XCTUnwrap(KeyCombo("esc")).label, "⎋")
        XCTAssertTrue(key.matches(keyCode: 15, command: true, control: false, option: false, shift: true))
        XCTAssertFalse(key.matches(keyCode: 15, command: true, control: false, option: false, shift: false))
        XCTAssertTrue(try XCTUnwrap(KeyCombo("p")).bare)
    }
}
