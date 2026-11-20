import Foundation
import XCTest

import CompanionKit

final class PathSelectorTests: XCTestCase {
    private let both: Set<NetworkPath> = [.tailnet, .relay]

    func testAutoStartsWithTheLastPathThatAnsweredAndFallsToTheOther() {
        XCTAssertEqual(PathSelector(choice: .auto, paths: both).order, [.tailnet, .relay])
        let relayFirst = PathSelector(choice: .auto, paths: both, answered: .relay)
        XCTAssertEqual(relayFirst.order, [.relay, .tailnet])
        XCTAssertEqual(relayFirst.next(after: .relay, failed: RemoteError.notSent("Mac not connected")), .tailnet)
        XCTAssertEqual(relayFirst.next(after: .relay, failed: RemoteError.unreachable("timed out")), .tailnet)
        XCTAssertNil(relayFirst.next(after: .tailnet, failed: RemoteError.notSent("no route")))
        XCTAssertEqual(PathSelector(choice: .auto, paths: [.tailnet], answered: .relay).order, [.tailnet])
        XCTAssertEqual(PathSelector(choice: .auto, paths: [.relay]).order, [.relay])
    }

    func testRevokedAccessOrAnAnswerNeverFallsOver() {
        let selector = PathSelector(choice: .auto, paths: both)
        for error: RemoteError in [.unauthorized, .relayRevoked, .http(500), .server(code: "failed", message: "x"), .badResponse] {
            XCTAssertNil(selector.next(after: .tailnet, failed: error), "\(error)")
        }
        XCTAssertNil(selector.next(after: .tailnet, failed: CancellationError()))
    }

    func testFixedChoicesUseOnlyTheirPath() {
        XCTAssertEqual(PathSelector(choice: .tailnet, paths: both, answered: .relay).order, [.tailnet])
        XCTAssertEqual(PathSelector(choice: .relay, paths: both).order, [.relay])
        XCTAssertNil(PathSelector(choice: .relay, paths: both).next(after: .relay, failed: RemoteError.notSent("x")))
        XCTAssertEqual(PathSelector(choice: .relay, paths: [.tailnet]).order, [.tailnet])
    }

    func testBackgroundProbeOfTailscaleIsDueAfterTenMinutesOnTheRelay() {
        let auto = PathSelector(choice: .auto, paths: both, answered: .relay)
        XCTAssertFalse(auto.probeDue(active: .relay, elapsed: PathSelector.probeEvery - .seconds(1)))
        XCTAssertTrue(auto.probeDue(active: .relay, elapsed: PathSelector.probeEvery))
        XCTAssertFalse(auto.probeDue(active: .tailnet, elapsed: .seconds(3600)))
        XCTAssertFalse(PathSelector(choice: .relay, paths: both).probeDue(active: .relay, elapsed: .seconds(3600)))
        XCTAssertFalse(PathSelector(choice: .auto, paths: [.relay]).probeDue(active: .relay, elapsed: .seconds(3600)))
    }
}
