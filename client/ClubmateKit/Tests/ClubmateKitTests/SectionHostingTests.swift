import Foundation
import XCTest

import ClubmateKit

final class SectionHostingTests: XCTestCase {
    private func sections(_ json: String) throws -> [SectionInfo] {
        try JSONDecoder().decode([SectionInfo].self, from: Data(json.utf8))
    }

    func testHostedSectionResolvesToItsHostTab() throws {
        let all = try sections(#"""
        [{"id":"editor-nvim","title":"Code","icon":"","order":10,"scope":"project","plugin":"editor-nvim"},
         {"id":"alpha","title":"Alpha","icon":"","order":15,"scope":"project","plugin":"alpha","host":"editor-nvim","slots":["extra"]}]
        """#)

        XCTAssertEqual(SectionHosting.rail(all).map(\.id), ["editor-nvim"])
        XCTAssertEqual(SectionHosting.resolve("alpha", slot: "alpha.sidebar", in: all), SectionTarget(section: "editor-nvim", tab: "alpha.sidebar", slot: "editor-nvim.sidebar"))
        XCTAssertEqual(SectionHosting.resolve("alpha", slot: "", in: all), SectionTarget(section: "editor-nvim", tab: "alpha.sidebar", slot: ""))
        XCTAssertEqual(SectionHosting.resolve("alpha", slot: "alpha.extra", in: all), SectionTarget(section: "editor-nvim", tab: "alpha.extra", slot: "editor-nvim.sidebar"))
        XCTAssertEqual(SectionHosting.resolve("editor-nvim", slot: "editor-nvim.main", in: all), SectionTarget(section: "editor-nvim", tab: nil, slot: "editor-nvim.main"))
        XCTAssertEqual(SectionHosting.rail(Array(all.dropFirst())).map(\.id), ["alpha"])
    }
}
