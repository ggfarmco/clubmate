import XCTest

import ClubmateProtocol
@testable import ClubmateKit

final class GuideTourTests: XCTestCase {
    private func actions() throws -> [SDUIStepAction] {
        let json = """
        {"type": "guide.steps", "id": "s/steps", "props": {"session": "s", "step": "step-1", "actions": [
          {"id": "continue", "label": "Continue", "enabled": true, "primary": true, "shortcut": "cmd+return"},
          {"id": "test_me", "label": "Test me", "enabled": true, "shortcut": "opt+t"},
          {"id": "show_me", "label": "Show me", "enabled": false, "shortcut": "opt+s"}]}}
        """
        return try XCTUnwrap(JSONDecoder().decode(SDUINode.self, from: Data(json.utf8)).stepControls()).actions
    }

    func testTreeMovesFromTheCurrentRowAndClosesOffersOnMove() {
        let ids = ["a", "b", "c"]
        var selection = TourSelection()

        XCTAssertEqual(selection.at(ids, current: "b"), "b")
        selection.move(1, in: ids, current: "b")
        XCTAssertEqual(selection.selected, "c")
        selection.toggleOffers(for: "c")
        XCTAssertEqual(selection.opened, "c")
        selection.move(1, in: ids, current: "b")
        XCTAssertEqual(selection.selected, "c")
        XCTAssertEqual(selection.opened, "c")
        selection.move(-1, in: ids, current: "b")
        XCTAssertNil(selection.opened)
        selection.move(-5, in: ids, current: "b")
        XCTAssertEqual(selection.selected, "a")
    }

    func testTourPaneNeedsAWideMainColumnAndOtherAsidesAreKept() {
        let tour = SDUINode(type: "guide.tour")
        let aside = SDUINode(type: "stack")

        XCTAssertTrue(GuideTour.paneShown(shown: true, aside: tour, main: 960))
        XCTAssertFalse(GuideTour.paneShown(shown: true, aside: tour, main: 959))
        XCTAssertFalse(GuideTour.asideVisible(shown: true, aside: tour, main: 700))
        XCTAssertTrue(GuideTour.asideVisible(shown: true, aside: aside, main: 700))
        XCTAssertFalse(GuideTour.asideVisible(shown: false, aside: tour, main: 1400))
    }

    func testStepKeyFiresOnlyAnEnabledNonContinueAction() throws {
        let actions = try actions()

        XCTAssertEqual(GuideTour.stepAction(in: actions, keyCode: 17, command: false, control: false, option: true, shift: false)?.id, "test_me")
        XCTAssertNil(GuideTour.stepAction(in: actions, keyCode: 1, command: false, control: false, option: true, shift: false))
        XCTAssertNil(GuideTour.stepAction(in: actions, keyCode: 36, command: true, control: false, option: false, shift: false))
    }

    @MainActor
    func testCommandReturnContinuesOnlyAnEmptyComposerThatOffersContinue() throws {
        let model = ComposerModel()
        let send = try XCTUnwrap(ComposerKey(keyCode: 36, characters: "\r", command: true, control: false, option: false, shift: false))
        let enter = try XCTUnwrap(ComposerKey(keyCode: 36, characters: "\r", command: false, control: false, option: false, shift: false))

        XCTAssertEqual(model.handle(send), .handled)
        model.continues = true
        XCTAssertEqual(model.handle(send), .continueStep)
        XCTAssertEqual(model.handle(enter), .handled)
        model.text = "why"
        XCTAssertEqual(model.handle(send), .send("why"))
    }
}
