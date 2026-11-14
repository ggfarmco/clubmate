import Foundation
import XCTest

import ClubmateKit

final class SectionHostingTests: XCTestCase {
    private func sections(_ json: String) throws -> [SectionInfo] {
        try JSONDecoder().decode([SectionInfo].self, from: Data(json.utf8))
    }

    func testSlotBelongsToTheLongestKnownSectionId() {
        let ids = ["agents", "agents.gates", "terminals"]

        XCTAssertEqual(SectionHosting.section(ofSlot: "agents.gates.main", in: ids), "agents.gates")
        XCTAssertEqual(SectionHosting.section(ofSlot: "agents.sidebar.field", in: ids), "agents")
        XCTAssertEqual(SectionHosting.section(ofSlot: "echo.main", in: ids), "echo")
    }

    func testHostedSectionResolvesToItsHostTab() throws {
        let all = try sections(#"""
        [{"id":"editor-nvim","title":"Code","icon":"","order":10,"scope":"workspace","plugin":"editor-nvim"},
         {"id":"alpha","title":"Alpha","icon":"","order":15,"scope":"workspace","plugin":"alpha","host":"editor-nvim","slots":["extra"]}]
        """#)

        XCTAssertEqual(SectionHosting.rail(all).map(\.id), ["editor-nvim"])
        XCTAssertEqual(SectionHosting.resolve("alpha", slot: "alpha.sidebar", in: all), SectionTarget(section: "editor-nvim", tab: "alpha.sidebar", slot: "editor-nvim.sidebar"))
        XCTAssertEqual(SectionHosting.resolve("alpha", slot: "", in: all), SectionTarget(section: "editor-nvim", tab: "alpha.sidebar", slot: ""))
        XCTAssertEqual(SectionHosting.resolve("alpha", slot: "alpha.extra", in: all), SectionTarget(section: "editor-nvim", tab: "alpha.extra", slot: "editor-nvim.sidebar"))
        XCTAssertEqual(SectionHosting.resolve("editor-nvim", slot: "editor-nvim.main", in: all), SectionTarget(section: "editor-nvim", tab: nil, slot: "editor-nvim.main"))
        XCTAssertEqual(SectionHosting.rail(Array(all.dropFirst())).map(\.id), ["alpha"])
    }

    func testRailFalseSectionIsNotListed() throws {
        let all = try sections(#"""
        [{"id":"terminals","title":"Terminals","icon":"","order":20,"scope":"workspace","plugin":"terminals"},
         {"id":"docs","title":"Docs","icon":"","order":90,"scope":"workspace","plugin":"docs","rail":false,"slots":["popup"]}]
        """#)
        let shown = SectionHosting.rail(all) + SectionHosting.hosted(all)

        XCTAssertEqual(shown.map(\.id), ["terminals"])
        XCTAssertFalse(CommandPalette.entries(commands: [], sections: shown, sidebar: true).contains { $0.title == "Go to Docs" })
        XCTAssertNil(SectionHosting.resolve("docs", slot: "docs.popup", in: shown))
    }
}
