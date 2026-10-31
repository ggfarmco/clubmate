import Foundation
import Observation
import XCTest

import ClubmateKit

@MainActor
@Observable
private final class Box {
    var value: Int?
}

@MainActor
final class ObservedTests: XCTestCase {
    func testFirstWaitsForAChangeAndGivesUpAtTheDeadline() async {
        let box = Box()
        Task {
            try await Task.sleep(for: .milliseconds(50))
            box.value = 7
        }
        let start = ContinuousClock.now
        let seen = await Observed.first(within: .seconds(5)) { box.value }
        XCTAssertEqual(seen, 7)
        XCTAssertLessThan(ContinuousClock.now - start, .seconds(1))

        let never = Box()
        let missing = await Observed.first(within: .milliseconds(100)) { never.value }
        XCTAssertNil(missing)
    }
}
