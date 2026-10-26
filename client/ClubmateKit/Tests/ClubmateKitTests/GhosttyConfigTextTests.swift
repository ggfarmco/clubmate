import XCTest

import ClubmateKit

final class GhosttyConfigTextTests: XCTestCase {
    func testLayeredValues() {
        let config = GhosttyConfigText([
            "# font-family = Commented\nfont-family = \"Iosevka\"\nfont-family = Symbols\nselection-background = #111111\n",
            "font-family =\nfont-family = \"JetBrains Mono\"\nselection-background = 2d4f67\n",
        ])
        XCTAssertEqual(config.first("font-family"), "JetBrains Mono")
        XCTAssertEqual(config.last("selection-background").flatMap(GhosttyConfigText.hexColor), 0x2D4F67)
        XCTAssertNil(config.last("background"))
        XCTAssertNil(GhosttyConfigText.hexColor("cell-foreground"))
    }

    func testThemeSide() {
        let dual = "dark:Kanagawa Wave,light:Light Owl"
        XCTAssertEqual(GhosttyConfigText.themeName(dual, dark: true), "Kanagawa Wave")
        XCTAssertEqual(GhosttyConfigText.themeName(dual, dark: false), "Light Owl")
        XCTAssertEqual(GhosttyConfigText.themeName("Dracula", dark: true), "Dracula")
    }
}
