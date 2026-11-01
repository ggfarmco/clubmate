import Foundation
import XCTest

import CompanionKit

@MainActor
final class LayoutTests: XCTestCase {
    func testWidthPicksTheLayout() {
        XCTAssertEqual([0, 599, 600, 1000, 1001, 1376].map { CompanionLayout(width: $0) }, [.compact, .compact, .regular, .regular, .wide, .wide])
        XCTAssertEqual(CompanionLayout(width: 874, compactClass: true), .compact)
    }

    func testRegularShowsTheMacsUntilOneIsChosen() {
        XCTAssertEqual(CompanionLayout.regular.columns(chosen: false), .three)
        XCTAssertEqual(CompanionLayout.regular.columns(chosen: true), .two)
        XCTAssertEqual(CompanionLayout.wide.columns(chosen: true), .three)
        XCTAssertEqual(CompanionLayout.compact.columns(chosen: true), .stack)
    }

    func testWideKeepsTheSidebarTheUserHidUntilNothingIsChosen() {
        XCTAssertEqual(CompanionLayout.wide.columns(chosen: true, sidebarHidden: true), .two)
        XCTAssertEqual(CompanionLayout.wide.columns(chosen: false, sidebarHidden: true), .three)
    }

    func testStackShowsTheDeepestOpenColumn() {
        XCTAssertEqual(CompanionLayout.stackColumn(chosen: true, detail: true), .detail)
        XCTAssertEqual(CompanionLayout.stackColumn(chosen: true, detail: false), .content)
        XCTAssertEqual(CompanionLayout.stackColumn(chosen: false, detail: false), .sidebar)
    }

    func testPendingPermissionIsTheOpenRequestOfTheSession() async throws {
        let runner = FakeRunner()
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.refresh()
        await store.loadTranscript("agt_0000000000000001")
        await store.loadTranscript("agt_0000000000000002")
        XCTAssertEqual(store.pendingPermission("agt_0000000000000001")?.id, "permission-3")
        XCTAssertNil(store.pendingPermission("agt_0000000000000002"))
    }
}
