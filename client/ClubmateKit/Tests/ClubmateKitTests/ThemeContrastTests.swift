import XCTest

import ClubmateKit

final class ThemeContrastTests: XCTestCase {
    private let latteBackground = RGB(hex: 0xEFF1F5)
    private let latteForeground = RGB(hex: 0x4C4F69)

    func testAlphaIsTheLowestThatReachesTheTarget() {
        let background = RGB(hex: 0x1F1F28)
        let foreground = RGB(hex: 0xDCD7BA)
        let alpha = ThemeContrast.alpha(of: foreground, over: background, reaching: 4.5)
        XCTAssertGreaterThanOrEqual(background.mixed(with: foreground, amount: alpha).contrast(with: background), 4.5)
        XCTAssertLessThan(background.mixed(with: foreground, amount: alpha - 0.01).contrast(with: background), 4.5)
    }

    func testInkDarkensALightThemeToneButKeepsItsHue() {
        let ink = ThemeContrast.ink(RGB(hex: 0xDF8E1D), toward: latteForeground, on: latteBackground, reaching: 4.5)
        XCTAssertGreaterThanOrEqual(ink.contrast(with: latteBackground), 4.5)
        XCTAssertGreaterThan(ink.red, ink.blue + 0.2)
    }

    func testToneThatAlreadyPassesIsKept() {
        let tone = RGB(hex: 0x98BB6C)
        XCTAssertEqual(ThemeContrast.ink(tone, toward: RGB(hex: 0xDCD7BA), on: RGB(hex: 0x1F1F28), reaching: 4.5), tone)
    }

    func testOnAccentTakesTheFirstCandidateThatPasses() {
        let white = RGB(hex: 0xFFFFFF)
        XCTAssertEqual(ThemeContrast.on(RGB(hex: 0x7E9CD8), candidates: [RGB(hex: 0x1F1F28), white], reaching: 4.5), RGB(hex: 0x1F1F28))
        XCTAssertEqual(ThemeContrast.on(RGB(hex: 0x1E66F5), candidates: [latteBackground, white], reaching: 4.5), white)
    }
}
