import Foundation
import XCTest

import ClubmateKit

@MainActor
final class LaunchCacheTests: XCTestCase {
    private var url: URL!

    override func setUp() {
        url = FileManager.default.temporaryDirectory.appending(path: "launch-\(UUID().uuidString).json")
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: url)
    }

    func testRecordSurvivesRelaunch() {
        let cache = LaunchCache(url: url, version: "1.0")
        cache.plugins(["agents"])
        cache.recentCount = 3
        cache.update("ws") { record in
            record.name = "demo"
            record.sections = [LaunchSection(id: "agents", title: "Agents", icon: "sparkle")]
            record.selected = "agents"
            record.kinds = ["agents.main": .transcript]
            record.sidebarWidth = 260
        }
        let relaunched = LaunchCache(url: url, version: "1.0")
        XCTAssertEqual(relaunched.record("ws"), cache.record("ws"))
        XCTAssertEqual(relaunched.recentCount, 3)
    }

    func testNewAppVersionDropsTheCache() {
        let cache = LaunchCache(url: url, version: "1.0")
        cache.recentCount = 2
        cache.update("ws") { $0.selected = "agents" }
        let upgraded = LaunchCache(url: url, version: "1.1")
        XCTAssertNil(upgraded.record("ws"))
        XCTAssertEqual(upgraded.recentCount, 0)
    }

    func testNewPluginSetDropsRecords() {
        let cache = LaunchCache(url: url, version: "1.0")
        cache.plugins(["agents", "terminals"])
        cache.update("ws") { $0.selected = "agents" }
        cache.plugins(["terminals", "agents"])
        XCTAssertNotNil(cache.record("ws"))
        cache.plugins(["agents"])
        XCTAssertNil(LaunchCache(url: url, version: "1.0").record("ws"))
    }

    func testChatShowsWhereItWasOpenAndCached() {
        var record = LaunchRecord()
        record.kinds = ["wiki.chat": .list]
        XCTAssertFalse(record.chatShown(in: "wiki"))
        record.chatHidden = ["wiki": false]
        XCTAssertTrue(record.chatShown(in: "wiki"))
        XCTAssertTrue(PaneKind.caches("wiki.chat"))
    }

    func testPaneKindComesFromTheTree() {
        let header = SDUINode(type: "header")
        XCTAssertEqual(PaneKind.of(SDUINode(type: "stack", children: [header, SDUINode(type: "tree")])), .list)
        XCTAssertEqual(PaneKind.of(SDUINode(type: "stack", children: [header, SDUINode(type: "transcript")])), .transcript)
        XCTAssertEqual(PaneKind.of(SDUINode(type: "table")), .table)
        XCTAssertEqual(PaneKind.of(SDUINode(type: "surface")), .terminal)
        XCTAssertEqual(PaneKind.of(SDUINode(type: "scroll", children: [header, SDUINode(type: "text")])), .detail)
        XCTAssertEqual(PaneKind.of(SDUINode(type: "empty")), .unknown)
    }
}

@MainActor
final class StartupWatchTests: XCTestCase {
    func testSlowTextAfterFiveSecondsDependsOnWhatIsWaited() async {
        let delays = Recorder<Duration>()
        let watch = StartupWatch { delay in
            await delays.add(delay)
            if await delays.values.count == 2 { throw CancellationError() }
        }
        await watch.run()
        XCTAssertEqual(delays.values, [.seconds(5), .seconds(10)])
        XCTAssertEqual(watch.label(.core), "Starting core…")
        XCTAssertEqual(watch.label(.workspace), "Connecting to workspace…")
        XCTAssertFalse(watch.showsLog)
    }

    func testLongTextAndLogAfterFifteenSeconds() async {
        let watch = StartupWatch { _ in }
        await watch.run()
        XCTAssertEqual(watch.label(.workspace), "Still starting core…")
        XCTAssertTrue(watch.showsLog)
    }
}
