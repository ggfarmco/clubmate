import Foundation
import XCTest

import CompanionKit

@MainActor
final class LayoutTests: XCTestCase {
    func testWidthPicksTheLayout() {
        XCTAssertEqual([0, 599, 600, 1000, 1001, 1376].map { CompanionLayout(width: $0) }, [.compact, .compact, .regular, .regular, .wide, .wide])
        XCTAssertEqual(CompanionLayout(width: 874, compactClass: true), .compact)
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
