import XCTest

@testable import CompanionKit

final class PaneLadderTests: XCTestCase {
    private let full = PaneFacts(hasMessages: true, listReachable: true)

    func testEscapeStepsComposerTranscriptListThenBackToTheComposer() {
        XCTAssertEqual(PaneLadder.escape(from: .composer, full).pane, .transcript)
        XCTAssertEqual(PaneLadder.escape(from: .transcript, full).pane, .list)
        XCTAssertEqual(PaneLadder.escape(from: .list, full).pane, .composer)
        XCTAssertEqual(PaneLadder.escape(from: nil, full).pane, .composer)
    }

    func testEmptyStopsAreSkipped() {
        XCTAssertEqual(PaneLadder.escape(from: .composer, PaneFacts(hasMessages: false, listReachable: true)).pane, .list)
        XCTAssertEqual(PaneLadder.escape(from: .transcript, PaneFacts(hasMessages: true, listReachable: false)).pane, .composer)
        XCTAssertEqual(PaneLadder.escape(from: .list, PaneFacts(hasMessages: true, canType: false, listReachable: true)).pane, .transcript)
    }

    func testOnlyLeavingTheComposerOfARunningTurnAsksForTheStopHint() {
        let running = PaneFacts(hasMessages: true, listReachable: true, turnRunning: true)
        XCTAssertTrue(PaneLadder.escape(from: .composer, running).leftRunningPrompt)
        XCTAssertFalse(PaneLadder.escape(from: .transcript, running).leftRunningPrompt)
        XCTAssertFalse(PaneLadder.escape(from: .composer, full).leftRunningPrompt)
    }

    func testArrowsGoStraightToAPaneThatCanTakeFocus() {
        XCTAssertEqual([PaneArrow.left, .right, .up, .down].map { PaneLadder.arrow($0, full) }, [.list, .composer, .transcript, .composer])
        let bare = PaneFacts(hasMessages: false, canType: false, listReachable: false)
        XCTAssertEqual([PaneArrow.left, .right, .up, .down].map { PaneLadder.arrow($0, bare) }, [nil, nil, nil, nil])
    }

    func testTheStopHintShowsTwentyTimesPerDevice() throws {
        let defaults = try XCTUnwrap(UserDefaults(suiteName: UUID().uuidString))
        defaults.set(StopHint.limit - 1, forKey: StopHint.defaultsKey)
        XCTAssertTrue(StopHint.take(from: defaults))
        XCTAssertFalse(StopHint.take(from: defaults))
    }
}
