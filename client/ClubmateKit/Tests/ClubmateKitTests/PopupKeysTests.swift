import XCTest

import ClubmateKit

final class PopupKeysTests: XCTestCase {
    private func key(_ combo: String, document: Bool = true, lightbox: Bool = false) -> PopupKey? {
        let combo = KeyCombo(combo)!
        return PopupKeys.key(keyCode: combo.keyCode, characters: combo.charactersIgnoringModifiers, command: combo.command, control: combo.control, option: combo.option, shift: combo.shift, document: document, lightbox: lightbox)
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
