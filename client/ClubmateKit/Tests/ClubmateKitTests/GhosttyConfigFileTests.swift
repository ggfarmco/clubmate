import XCTest

import ClubmateKit

final class GhosttyConfigFileTests: XCTestCase {
    func testSettingReplacesEveryOccurrenceInPlace() {
        var text = GhosttyConfigFile.setting("font-size", to: "14", in: "")
        XCTAssertEqual(text, "font-size = 14\n")
        text = GhosttyConfigFile.setting("font-family", to: "\"Iosevka\"", in: "# mine\nfont-family = A\nfont-size = 14\nfont-family = B\n")
        XCTAssertEqual(text, "# mine\nfont-family = \"Iosevka\"\nfont-size = 14\n")
        XCTAssertEqual(GhosttyConfigFile.setting("font-size", to: nil, in: text), "# mine\nfont-family = \"Iosevka\"\n")
    }

    func testImportCommentsColoursAndPinsIncludes() {
        let source = URL(filePath: "/Users/me/.config/ghostty/config")
        let text = "theme = dark:Kanagawa Wave,light:Light Owl\nfont-size = 13\nconfig-file = ?keys.conf\n"
        let block = GhosttyConfigFile.imported(source, text: text, on: Date(timeIntervalSince1970: 1_800_000_000))
        XCTAssertEqual(block, """
        # Imported from /Users/me/.config/ghostty/config on 2027-01-15
        # theme = dark:Kanagawa Wave,light:Light Owl  (Clubmate theme)
        font-size = 13
        config-file = ?"/Users/me/.config/ghostty/keys.conf"

        """)
        XCTAssertNil(GhosttyConfigText([block]).last("theme"))
    }
}
