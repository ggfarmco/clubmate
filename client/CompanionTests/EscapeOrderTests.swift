import XCTest

import CompanionKit

@testable import Companion

@MainActor
final class EscapeOrderTests: XCTestCase {
    func testEscapeLeavesReadingViewThenClosesTheListOverThePage() {
        let navigation = AppNavigation(restore: false)
        navigation.switchWorkspace(computer: "mac", workspace: "shop")
        navigation.layout = .regular
        navigation.room = .over
        navigation.push(.session("agt_1"))
        navigation.toggleList()
        navigation.readingView = true
        navigation.escape()
        XCTAssertEqual([navigation.readingView, navigation.listShown], [false, true])
        navigation.escape()
        XCTAssertFalse(navigation.listShown)
        XCTAssertFalse(navigation.canEscape)
    }
}
