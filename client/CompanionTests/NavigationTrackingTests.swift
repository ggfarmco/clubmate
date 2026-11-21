import Observation
import XCTest

import CompanionKit

@testable import Companion

@MainActor
final class NavigationTrackingTests: XCTestCase {
    private final class Hits: @unchecked Sendable {
        var tabs: Set<CompanionTab> = []
    }

    func testAPushInvalidatesOnlyReadersOfItsOwnTab() {
        let navigation = AppNavigation(restore: false)
        navigation.switchWorkspace(computer: "mac", workspace: "shop")
        let hits = Hits()
        for tab in [CompanionTab.agents, .backlog] {
            withObservationTracking { _ = navigation.path(tab) } onChange: { hits.tabs.insert(tab) }
        }
        navigation.push(.session("agt_1"))
        XCTAssertEqual(hits.tabs, [.agents])
        XCTAssertEqual(navigation.path(.agents).map(\.destination), [.session("agt_1")])
    }

    func testEachWorkspaceKeepsItsOwnPaths() {
        let navigation = AppNavigation(restore: false)
        navigation.switchWorkspace(computer: "mac", workspace: "shop")
        navigation.push(.session("agt_1"))
        navigation.switchWorkspace(computer: "mac", workspace: "blog")
        XCTAssertEqual(navigation.path(.agents), [])
        navigation.switchWorkspace(computer: "mac", workspace: "shop")
        XCTAssertEqual(navigation.path(.agents).map(\.destination), [.session("agt_1")])
    }
}
