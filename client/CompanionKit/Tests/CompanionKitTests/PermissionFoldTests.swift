import Foundation
import XCTest

import CompanionKit

final class PermissionFoldTests: XCTestCase {
    func testPermissionFoldFoldsFarFromLiveEndAndOpensNearIt() {
        var fold = PermissionFold()
        fold.handle(.liveEnd(below: 120))
        XCTAssertFalse(fold.folded)
        fold.handle(.liveEnd(below: 121))
        XCTAssertTrue(fold.folded)
        fold.handle(.liveEnd(below: 30))
        XCTAssertTrue(fold.folded)
        fold.handle(.liveEnd(below: 24))
        XCTAssertFalse(fold.folded)
    }

    func testPermissionFoldNeverWhileComposerFocused() {
        var fold = PermissionFold()
        fold.handle(.liveEnd(below: 500))
        fold.handle(.composerFocused(true))
        XCTAssertFalse(fold.folded)
        fold.handle(.liveEnd(below: 500))
        XCTAssertFalse(fold.folded)
        fold.handle(.composerFocused(false))
        fold.handle(.liveEnd(below: 500))
        XCTAssertTrue(fold.folded)
    }

    func testPermissionFoldTapOpensUntilLiveEndIsReached() {
        var fold = PermissionFold()
        fold.handle(.liveEnd(below: 500))
        fold.handle(.tapped)
        fold.handle(.liveEnd(below: 300))
        XCTAssertFalse(fold.folded)
        fold.handle(.liveEnd(below: 0))
        fold.handle(.liveEnd(below: 300))
        XCTAssertTrue(fold.folded)
    }

    func testPermissionFoldPulsesOnNewRequest() {
        var fold = PermissionFold()
        fold.handle(.newRequest)
        XCTAssertEqual(fold.pulses, 0)
        fold.handle(.liveEnd(below: 500))
        fold.handle(.newRequest)
        XCTAssertEqual(fold.pulses, 1)
    }
}
