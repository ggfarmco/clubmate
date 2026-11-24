import Foundation
import XCTest

import ClubmateProtocol
import CompanionKit

final class WorkspacePickerTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    func testActivityDecodesWithDefaultsAndOldPayloadsHaveNone() throws {
        let list = try JSONDecoder.clubmate.decode([Workspace].self, from: Data(#"[{"id":"a","name":"shop","activity":{"needsYou":2,"lastActiveAt":"2026-11-23T08:38:00Z"}},{"id":"b","name":"old"}]"#.utf8))

        XCTAssertEqual(list[0].activity, Workspace.Activity(needsYou: 2, running: 0, failed: 0, lastActiveAt: Date.parseRFC3339("2026-11-23T08:38:00Z")))
        XCTAssertNil(list[1].activity)
    }

    func testStatusPicksNeedsYouThenFailedThenRunningThenIdle() {
        XCTAssertEqual(WorkspaceSummary(needsYou: 1, running: 1, failed: 1).status.symbol, "hand.raised")
        XCTAssertEqual(WorkspaceSummary(running: 1, failed: 1).status.symbol, "xmark.circle")
        XCTAssertEqual(WorkspaceSummary(running: 1).status.symbol, "play")
        XCTAssertEqual(WorkspaceSummary().status.symbol, "folder")
        XCTAssertEqual(WorkspaceSummary().glyph(home: true), "house")
        XCTAssertEqual(WorkspaceSummary(running: 1).glyph(home: true), "play")
    }

    func testSubtitleListsOnlyNonZeroPartsInOrderElseIdleAge() {
        XCTAssertEqual(WorkspaceSummary(needsYou: 2, running: 1, failed: 3).text(now: now), "2 waiting · 3 failed · 1 running")
        XCTAssertEqual(WorkspaceSummary(running: 1).text(now: now), "1 running")
        XCTAssertEqual(WorkspaceSummary(lastActiveAt: now.addingTimeInterval(-3 * 3_600)).text(now: now), "idle · 3 h")
        XCTAssertNil(WorkspaceSummary().text(now: now))
    }

    func testAgeBuckets() {
        let ages = [10, 300, 3 * 3_600, 2 * 86_400, 8 * 86_400].map { WorkspaceSummary.age(now.addingTimeInterval(-Double($0)), now: now) }

        XCTAssertEqual(ages, ["now", "5 min", "3 h", "2 d", "1 w"])
    }

    func testOfflineSubtitleLeadsWithAsOfAndKeepsOnlyTheMostUrgentPart() {
        let seen = now.addingTimeInterval(-600)
        let clock = ComputerConnection.clock(seen)

        XCTAssertEqual(WorkspaceSummary(needsYou: 2, running: 1, failed: 3).subtitle(now: now, asOf: seen, offline: true), "as of \(clock) · 2 waiting")
        XCTAssertEqual(WorkspaceSummary(running: 1, failed: 3).subtitle(now: now, asOf: seen, offline: true), "as of \(clock) · 3 failed")
        XCTAssertEqual(WorkspaceSummary(lastActiveAt: now.addingTimeInterval(-3 * 3_600)).subtitle(now: now, asOf: seen, offline: true), "as of \(clock) · idle · 3 h")
        XCTAssertEqual(WorkspaceSummary().subtitle(now: now, asOf: seen, offline: true), "as of \(clock)")
        XCTAssertEqual(WorkspaceSummary(needsYou: 1).subtitle(now: now, asOf: seen, offline: false), "1 waiting")
        XCTAssertEqual(WorkspaceSummary.macSubtitle(WorkspaceSummary(needsYou: 1), offline: true, lastSeen: seen, now: now), "offline · as of \(clock)")
    }

    func testMacSummarySumsWorkspacesThatSendActivity() {
        let list = [
            Workspace(id: "a", name: "a", activity: Workspace.Activity(needsYou: 1, running: 2, lastActiveAt: now.addingTimeInterval(-60))),
            Workspace(id: "b", name: "b", activity: Workspace.Activity(needsYou: 1, failed: 1, lastActiveAt: now.addingTimeInterval(-7_200))),
        ]

        XCTAssertEqual(WorkspaceSummary.of(list) { _ in 9 }, WorkspaceSummary(needsYou: 2, running: 2, failed: 1, lastActiveAt: now.addingTimeInterval(-60)))
    }

    func testAMacWithoutActivityFallsBackToTheSessionCount() {
        let list = [Workspace(id: "a", name: "a"), Workspace(id: "b", name: "b")]

        XCTAssertEqual(WorkspaceSummary.of(list) { $0.id == "a" ? 3 : 0 }, WorkspaceSummary(needsYou: 3))
    }

    func testRowNameCutsALongNameToOneMenuLine() {
        XCTAssertEqual(WorkspaceSummary.rowName("infra-terraform-staging-eu-central-1-migration"), "infra-terraform-s…")
        XCTAssertEqual(WorkspaceSummary.rowName("infra-terraform-st"), "infra-terraform-st")
    }

    func testLastUsedWorkspaceFallsBackToHomeWhenNoneOrGone() {
        let list = [Workspace(id: "shop", name: "shop"), Workspace(id: "home", name: "Home", home: true), Workspace(id: "blog", name: "blog")]
        let last = LastWorkspaces(["studio": "blog", "pro": "gone"])

        XCTAssertEqual(last.workspace(for: "studio", in: list)?.id, "blog")
        XCTAssertEqual(last.workspace(for: "pro", in: list)?.id, "home")
        XCTAssertEqual(last.workspace(for: "air", in: list)?.id, "home")
        XCTAssertEqual(last.workspace(for: "air", in: [Workspace(id: "infra", name: "infra")])?.id, "infra")
    }
}
