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
        [{"id":"files","title":"Files","icon":"","order":10,"scope":"workspace","plugin":"files"},
         {"id":"alpha","title":"Alpha","icon":"","order":15,"scope":"workspace","plugin":"alpha","host":"files","slots":["extra"]}]
        """#)

        XCTAssertEqual(SectionHosting.rail(all).map(\.id), ["files"])
        XCTAssertEqual(SectionHosting.resolve("alpha", slot: "alpha.sidebar", in: all), SectionTarget(section: "files", tab: "alpha.sidebar", slot: "files.sidebar"))
        XCTAssertEqual(SectionHosting.resolve("alpha", slot: "", in: all), SectionTarget(section: "files", tab: "alpha.sidebar", slot: ""))
        XCTAssertEqual(SectionHosting.resolve("alpha", slot: "alpha.extra", in: all), SectionTarget(section: "files", tab: "alpha.extra", slot: "files.sidebar"))
        XCTAssertEqual(SectionHosting.resolve("files", slot: "files.main", in: all), SectionTarget(section: "files", tab: nil, slot: "files.main"))
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
