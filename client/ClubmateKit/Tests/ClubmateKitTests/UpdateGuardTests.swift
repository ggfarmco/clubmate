import XCTest

import ClubmateKit

final class UpdateGuardTests: XCTestCase {
    func testUpdateGuard() {
        XCTAssertFalse(UpdateGuard.needsConfirmation(itemProtocol: nil, coreProtocol: 1, liveSessions: 3))
        XCTAssertFalse(UpdateGuard.needsConfirmation(itemProtocol: 1, coreProtocol: 1, liveSessions: 3))
        XCTAssertFalse(UpdateGuard.needsConfirmation(itemProtocol: 2, coreProtocol: 1, liveSessions: 0))
        XCTAssertTrue(UpdateGuard.needsConfirmation(itemProtocol: 2, coreProtocol: 1, liveSessions: 3))
    }
}
