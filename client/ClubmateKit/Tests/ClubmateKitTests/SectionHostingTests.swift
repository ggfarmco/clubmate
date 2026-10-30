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
         {"id":"git","title":"Git","icon":"","order":15,"scope":"project","plugin":"git","host":"editor-nvim","slots":["review"]}]
        """#)

        XCTAssertEqual(SectionHosting.rail(all).map(\.id), ["editor-nvim"])
        XCTAssertEqual(SectionHosting.resolve("git", slot: "git.sidebar", in: all), SectionTarget(section: "editor-nvim", tab: "git.sidebar", slot: "editor-nvim.sidebar"))
        XCTAssertEqual(SectionHosting.resolve("git", slot: "", in: all), SectionTarget(section: "editor-nvim", tab: "git.sidebar", slot: ""))
        XCTAssertEqual(SectionHosting.resolve("git", slot: "git.review", in: all), SectionTarget(section: "editor-nvim", tab: "git.review", slot: "editor-nvim.sidebar"))
        XCTAssertEqual(SectionHosting.resolve("editor-nvim", slot: "editor-nvim.main", in: all), SectionTarget(section: "editor-nvim", tab: nil, slot: "editor-nvim.main"))
        XCTAssertEqual(SectionHosting.rail(Array(all.dropFirst())).map(\.id), ["git"])
    }

    func testReviewStartedRightAfterLaunchSwitches() {
        let tabs = SidebarTabs.tabs(section: CodeSection.id, title: "Code", hosted: [], review: true)
        var state = SidebarTabs()
        state.reviewSeen(SDUINode(type: "empty"))
        state.reviewSeen(SDUINode(type: "tree", props: ["review": .object(["base": .string("main"), "head": .string("x"), "started": .number(100)])]))
        XCTAssertEqual(state.current(CodeSection.id, in: tabs)?.title, "Review")
    }

    func testSidebarTabIsTheUsersUntilAReviewStarts() throws {
        let hosted = try sections(#"[{"id":"git","title":"Git","icon":"","order":15,"scope":"project","plugin":"git","host":"editor-nvim"}]"#)
        let tabs = SidebarTabs.tabs(section: CodeSection.id, title: "Code", hosted: hosted, review: true)
        var state = SidebarTabs()
        func seen(_ started: Double?) {
            var header: [String: JSONValue] = ["base": .string("main"), "head": .string("x")]
            if let started { header["started"] = .number(started) }
            state.reviewSeen(SDUINode(type: "tree", props: ["review": .object(header)]))
        }
        func current() -> String? { state.current(CodeSection.id, in: tabs)?.title }
        XCTAssertEqual(tabs.map(\.title), ["Files", "Git", "Review"])

        state.choose("editor-nvim.sidebar", in: CodeSection.id)
        seen(100)
        seen(50)
        seen(100)
        state.reviewSeen(SDUINode(type: "empty"))
        XCTAssertEqual(current(), "Files")

        seen(200)
        XCTAssertEqual(current(), "Review")

        state.choose("editor-nvim.sidebar", in: CodeSection.id)
        seen(nil)
        XCTAssertEqual(current(), "Files")
        seen(300)
        XCTAssertEqual(current(), "Review")

        var relaunched = SidebarTabs()
        relaunched.reviewSeen(SDUINode(type: "tree", props: ["review": .object(["base": .string("main"), "head": .string("x"), "started": .number(300)])]))
        XCTAssertEqual(relaunched.current(CodeSection.id, in: tabs)?.title, "Files")
        XCTAssertEqual(relaunched.cycle(CodeSection.id, in: tabs, by: -1)?.title, "Review")
        XCTAssertEqual(relaunched.cycle(CodeSection.id, in: tabs, by: 1)?.title, "Files")
    }
}
