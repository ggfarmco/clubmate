import XCTest

import ClubmateKit

final class ColorSchemeChoiceTests: XCTestCase {
    func testIsDarkAppearance() {
        XCTAssertTrue(ColorSchemeChoice.isDark(appearanceName: "NSAppearanceNameDarkAqua"))
        XCTAssertFalse(ColorSchemeChoice.isDark(appearanceName: "NSAppearanceNameAqua"))
        XCTAssertTrue(ColorSchemeChoice.isDark(appearanceName: "NSAppearanceNameAccessibilityHighContrastDarkAqua"))
    }
}
