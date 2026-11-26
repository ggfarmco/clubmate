import XCTest

import ClubmateProtocol

final class GuideTests: XCTestCase {
    func testTourDecodesConceptsLinksAndOffersAndHeaderKeepsTheStripOutOfTheBar() throws {
        let json = """
        {"type": "header", "props": {"title": "guide"}, "children": [
          {"type": "button", "id": "x", "props": {"label": "X"}},
          {"type": "guide.strip", "id": "s/strip", "props": {"session": "s", "title": "Tour", "step": 5, "of": 8, "stepLabel": "step 5 of 8", "current": "c5", "shown": false, "concepts": [
            {"id": "c4", "label": "Refill", "state": "missed", "stateLabel": "missed", "stepLabel": "step 4", "depth": 2, "note": "line 32", "offers": [{"id": "ask_again", "label": "Ask again"}]},
            {"id": "c5", "label": "Cap", "state": "current", "stateLabel": "now", "depth": 3, "links": [{"id": "c4", "label": "Refill"}]}]}}]}
        """
        let header = try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8))
        let tour = try XCTUnwrap(header.headerStrip?.node.tour())

        XCTAssertEqual(header.headerBar.map(\.id), ["x"])
        XCTAssertEqual(tour.currentConcept?.links.map(\.id), ["c4"])
        XCTAssertEqual(tour.concepts.first?.offers.map(\.id), ["ask_again"])
        XCTAssertEqual(tour.concepts.first?.depth, 2)
        XCTAssertFalse(tour.shown)
    }
}
