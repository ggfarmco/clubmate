import Foundation
import XCTest

import CompanionKit

final class ReadingModeTests: XCTestCase {
    private func scrolled(_ mode: inout ReadingMode, from start: Double, by steps: [Double]) {
        var offset = start
        for step in steps {
            offset += step
            mode.handle(.scrolled(offset: offset, delta: step))
        }
    }

    private func reading() -> ReadingMode {
        var mode = ReadingMode()
        scrolled(&mode, from: 160, by: [20, 20])
        return mode
    }

    func testReadingModeEntersAfterThresholds() {
        var mode = ReadingMode()
        scrolled(&mode, from: 100, by: [20, 20, 20])
        XCTAssertFalse(mode.active)
        scrolled(&mode, from: 160, by: [-5, 20, 15])
        XCTAssertFalse(mode.active)
        scrolled(&mode, from: 190, by: [10])
        XCTAssertTrue(mode.active)
    }

    func testReadingModeLeavesOnScrollUpTapEscape() {
        var mode = reading()
        scrolled(&mode, from: 400, by: [-10, -10])
        XCTAssertTrue(mode.active)
        scrolled(&mode, from: 380, by: [-4])
        XCTAssertFalse(mode.active)

        mode = reading()
        mode.handle(.scrolled(offset: 40, delta: 1))
        XCTAssertFalse(mode.active)

        for input in [ReadingMode.Input.tappedText, .escape] {
            mode = reading()
            mode.handle(input)
            XCTAssertFalse(mode.active)
        }
    }

    func testReadingModeNeverUnderAccessibility() {
        for assistive in [ReadingMode.Assistive.voiceOver, .switchControl, .reduceMotion] {
            var mode = reading()
            mode.handle(.accessibility(assistive))
            XCTAssertFalse(mode.active)
            scrolled(&mode, from: 200, by: [50, 50])
            XCTAssertFalse(mode.active)
        }
    }
}
