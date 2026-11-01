import Foundation
import XCTest

import ClubmateProtocol

final class PopupsTests: XCTestCase {
    private func view(_ slot: String, _ type: String) -> SlotView {
        SlotView(workspaceId: "prj_1", slot: slot, owner: "p", rev: 1, tree: SDUINode(type: type))
    }

    func testSlotsListEverySectionDeclaringPopup() throws {
        let sections = try JSONDecoder().decode([SectionInfo].self, from: Data(#"""
        [{"id":"terminals","title":"Terminals","icon":"","order":20,"scope":"workspace","plugin":"terminals"},
         {"id":"docs","title":"Docs","icon":"","order":90,"scope":"workspace","plugin":"docs","rail":false,"slots":["popup"]}]
        """#.utf8))

        XCTAssertEqual(Popups.slots(sections), ["docs.popup"])
    }

    func testCurrentIsTheLatestOpenPopup() {
        let order = ["a.popup", "b.popup"]

        XCTAssertEqual(Popups.current(["a.popup": view("a.popup", "stack"), "b.popup": view("b.popup", "stack")], order: order), "b.popup")
        XCTAssertEqual(Popups.current(["a.popup": view("a.popup", "stack"), "b.popup": view("b.popup", "empty")], order: order), "a.popup")
        XCTAssertNil(Popups.current(["a.popup": view("a.popup", "empty")], order: order))
    }

    func testEmptyTreeClosesPopup() {
        var presentation = PopupPresentation()

        XCTAssertEqual(presentation.update(Popups.current(["docs.popup": view("docs.popup", "stack")], order: ["docs.popup"])), .opened("docs.popup"))
        XCTAssertTrue(presentation.presented)
        XCTAssertEqual(presentation.update(Popups.current(["docs.popup": view("docs.popup", "empty")], order: ["docs.popup"])), .closed("docs.popup"))
        XCTAssertFalse(presentation.presented)
    }
}
