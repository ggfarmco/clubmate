import XCTest

import ClubmateKit

final class GhosttyConfigTextTests: XCTestCase {
    func testLayeredValues() {
        let config = GhosttyConfigText([
            "# font-family = Commented\nfont-family = \"Iosevka\"\nfont-family = Symbols\n",
            "font-family =\nfont-family = \"JetBrains Mono\"\n",
        ])
        XCTAssertEqual(config.first("font-family"), "JetBrains Mono")
        XCTAssertNil(config.first("background"))
    }
}
