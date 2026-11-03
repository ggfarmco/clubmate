import XCTest

import ClubmateKit

final class GraphViewStateTests: XCTestCase {
    func testZoomStepsStayInRangeAndFitForgetsThem() {
        var state = GraphViewState()
        state.zoom("run/graph", steps: 1, from: 0.8)
        XCTAssertEqual(state.zoom("run/graph") ?? 0, 1, accuracy: 1e-9)
        XCTAssertNil(state.zoom("other/graph"))
        state.zoom("run/graph", steps: 9, from: 1)
        XCTAssertEqual(state.zoom("run/graph"), 2)
        state.zoom("run/graph", steps: -9, from: 2)
        XCTAssertEqual(state.zoom("run/graph"), 0.5)
        state.fit("run/graph")
        XCTAssertNil(state.zoom("run/graph"))
    }

    func testFitFillsTheWidthButNotBelowEightyPercentOrAboveFull() {
        XCTAssertEqual(GraphViewState.fit(width: 900, content: 1000), 0.9)
        XCTAssertEqual(GraphViewState.fit(width: 400, content: 1000), 0.8)
        XCTAssertEqual(GraphViewState.fit(width: 1600, content: 1000), 1)
    }

    func testHistoryStartsAtItsDefaultAndTogglesFromIt() {
        var state = GraphViewState()
        XCTAssertFalse(state.historyOpen("run/graph"))
        XCTAssertTrue(state.historyOpen("task/owners", default: true))
        state.toggleHistory("task/owners", default: true)
        XCTAssertFalse(state.historyOpen("task/owners", default: true))
    }
}
