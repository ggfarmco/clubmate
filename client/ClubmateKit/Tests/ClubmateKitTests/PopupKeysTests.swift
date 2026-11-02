import XCTest

import ClubmateKit

final class PopupKeysTests: XCTestCase {
    private func key(_ combo: String, document: Bool = true, lightbox: Bool = false, finding: Bool = false) -> PopupKey? {
        let combo = KeyCombo(combo)!
        return PopupKeys.key(keyCode: combo.keyCode, characters: combo.charactersIgnoringModifiers, command: combo.command, control: combo.control, option: combo.option, shift: combo.shift, document: document, lightbox: lightbox, finding: finding)
    }

    func testFindKeysOverADocumentAndEscClosesFindFirst() {
        XCTAssertEqual(key("cmd+f"), .find)
        XCTAssertNil(key("cmd+f", document: false))
        XCTAssertNil(key("cmd+g"))
        XCTAssertEqual(key("cmd+g", finding: true), .findNext)
        XCTAssertEqual(key("cmd+shift+g", finding: true), .findPrevious)
        XCTAssertEqual(key("shift+return", finding: true), .findPrevious)
        XCTAssertNil(key("shift+return"))
        XCTAssertEqual(key("esc", finding: true), .closeFind)
        XCTAssertNil(key("esc", lightbox: true, finding: true))
    }

    func testZoomKeysOnlyOverADocument() {
        XCTAssertEqual(key("cmd+="), .zoomIn)
        XCTAssertEqual(key("cmd+shift+="), .zoomIn)
        XCTAssertEqual(key("cmd+-"), .zoomOut)
        XCTAssertEqual(key("cmd+0"), .zoomReset)
        XCTAssertNil(key("cmd+=", document: false))
        XCTAssertNil(key("ctrl+cmd+="))
    }

    func testEscPassesToAnOpenLightbox() {
        XCTAssertEqual(key("esc"), .close)
        XCTAssertEqual(key("cmd+w", document: false), .close)
        XCTAssertNil(key("esc", lightbox: true))
        XCTAssertEqual(key("cmd+w", lightbox: true), .close)
    }

    func testCmdJSwitchesFocusOverAnyPopup() {
        XCTAssertEqual(key("cmd+j", document: false), .switchFocus)
        XCTAssertNil(key("cmd+shift+j"))
    }
}
