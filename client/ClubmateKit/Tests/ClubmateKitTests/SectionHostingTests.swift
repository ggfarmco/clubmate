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
         {"id":"git","title":"Git","icon":"","order":15,"scope":"project","plugin":"git","host":"editor-nvim"}]
        """#)

        XCTAssertEqual(SectionHosting.rail(all).map(\.id), ["editor-nvim"])
        XCTAssertEqual(SectionHosting.resolve("git", slot: "git.sidebar", in: all), SectionTarget(section: "editor-nvim", tab: "git.sidebar", slot: "editor-nvim.sidebar"))
        XCTAssertEqual(SectionHosting.resolve("git", slot: "", in: all), SectionTarget(section: "editor-nvim", tab: "git.sidebar", slot: ""))
        XCTAssertEqual(SectionHosting.resolve("editor-nvim", slot: "editor-nvim.main", in: all), SectionTarget(section: "editor-nvim", tab: nil, slot: "editor-nvim.main"))
        XCTAssertEqual(SectionHosting.rail(Array(all.dropFirst())).map(\.id), ["git"])
    }

    func testSidebarTabIsTheUsersUntilAReviewStarts() throws {
        let hosted = try sections(#"[{"id":"git","title":"Git","icon":"","order":15,"scope":"project","plugin":"git","host":"editor-nvim"}]"#)
        let reviewing = SidebarTabs.tabs(section: CodeSection.id, title: "Code", hosted: hosted, review: true)
        let idle = SidebarTabs.tabs(section: CodeSection.id, title: "Code", hosted: hosted, review: false)
        var state = SidebarTabs()
        XCTAssertEqual(reviewing.map(\.title), ["Files", "Git", "Review"])

        state.choose("editor-nvim.sidebar", in: CodeSection.id)
        state.reviewChanged(from: nil, to: 1)
        state.reviewChanged(from: 1, to: 1)
        state.reviewChanged(from: 1, to: 0)
        XCTAssertEqual(state.current(CodeSection.id, in: reviewing)?.title, "Files")

        state.reviewChanged(from: 0, to: 2)
        XCTAssertEqual(state.current(CodeSection.id, in: reviewing)?.title, "Review")

        state.choose("editor-nvim.sidebar", in: CodeSection.id)
        state.reviewChanged(from: 2, to: 3)
        XCTAssertEqual(state.current(CodeSection.id, in: reviewing)?.title, "Review")
        XCTAssertEqual(state.current(CodeSection.id, in: idle)?.title, "Files")
    }
}
