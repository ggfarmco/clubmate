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

    func testStepOutClosesWhatIsOpenFirstThenStepsTheLadder() {
        let navigation = AppNavigation(restore: false)
        navigation.switchWorkspace(computer: "mac", workspace: "shop")
        navigation.layout = .regular
        navigation.push(.session("agt_1"))
        var steps = 0
        let panes = PaneKeys(facts: PaneFacts(hasMessages: true, listReachable: true), escape: { steps += 1 }, go: { _ in })
        navigation.menu.offer(MenuState(keys: SessionKeyActions(scope: "agt_1|", enabled: true, answers: [:], send: nil, panes: panes)), move: MenuMove(tab: .agents, depth: 1))
        navigation.readingView = true
        navigation.stepOut()
        XCTAssertFalse(navigation.readingView)
        XCTAssertEqual(steps, 0)
        navigation.stepOut()
        XCTAssertEqual(steps, 0)
        navigation.menu.panes.listFocused = true
        navigation.stepOut()
        XCTAssertEqual(steps, 1)
    }
}
