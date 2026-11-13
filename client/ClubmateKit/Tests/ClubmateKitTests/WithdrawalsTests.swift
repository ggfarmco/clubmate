import XCTest

@testable import ClubmateKit

@MainActor
final class WithdrawalsTests: XCTestCase {
    func testEarlyWithdrawalsAgeOutAndAreCapped() {
        var withdrawals = Withdrawals()
        let start = ContinuousClock.now
        withdrawals.withdraw("stale", now: start)
        XCTAssertFalse(withdrawals.track("stale", reason: "gone", now: start + .seconds(31)).expired)
        withdrawals.withdraw("first", now: start)
        for index in 0..<Withdrawals.earlyCap { withdrawals.withdraw("id\(index)", now: start) }
        XCTAssertFalse(withdrawals.track("first", reason: "gone", now: start).expired)
        let held = withdrawals.track("id0", reason: "gone", now: start)
        XCTAssertTrue(held.expired)
        XCTAssertEqual(held.reason, "gone")
    }
}
