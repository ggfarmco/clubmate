import Foundation
import XCTest

import ClubmateKit

@MainActor
final class SnapshotTests: XCTestCase {
    private var core: TestCore?

    override func tearDown() async throws {
        await core?.stop()
        core = nil
    }

    func testTestModeNeedsScratchDirs() {
        let defaults = ClubmatePaths(state: URL(filePath: "/Users/u/Library/Application Support/clubmate-dev"), runtime: URL(filePath: "/tmp/clubmate-dev-501"), flavor: "dev")
        let scratch = ClubmatePaths(state: URL(filePath: "/tmp/clubmate-test-a/state"), runtime: URL(filePath: "/tmp/clubmate-test-a/run"), flavor: "dev")
        let release = ClubmatePaths(state: scratch.state, runtime: scratch.runtime, flavor: "release")
        let on = ["CLUBMATE_TEST_MODE": "1"]
        XCTAssertTrue(TestMode.enabled(env: on, paths: scratch, defaults: defaults))
        XCTAssertFalse(TestMode.enabled(env: [:], paths: scratch, defaults: defaults))
        XCTAssertFalse(TestMode.enabled(env: on, paths: defaults, defaults: defaults))
        XCTAssertFalse(TestMode.enabled(env: on, paths: release, defaults: defaults))
        XCTAssertEqual(TestMode.suiteName(paths: scratch), "com.clubmate.app.dev.test.clubmate-test-a")
    }

    func testFlags() {
        let elements = [
            element("panel", frame: Rect(x: 0, y: 0, width: 360, height: 500)),
            element("row", frame: Rect(x: 0, y: 0, width: 412, height: 20), parent: "panel"),
            element("a", frame: Rect(x: 0, y: 30, width: 100, height: 20), parent: "panel"),
            element("b", frame: Rect(x: 0, y: 46, width: 100, height: 20), parent: "panel"),
            element("label", frame: Rect(x: 0, y: 80, width: 50, height: 20), parent: "panel", idealWidth: 72, lineHeight: 16),
            element("wrapped", frame: Rect(x: 0, y: 110, width: 50, height: 32), parent: "panel", idealWidth: 72, lineHeight: 16),
            element("footer", frame: Rect(x: 0, y: 590, width: 100, height: 20)),
            element("ax-row", source: "ax", frame: Rect(x: 0, y: 100, width: 500, height: 20), parent: "panel"),
        ]
        let flagged = Dictionary(uniqueKeysWithValues: LayoutAnalyzer.flag(elements, bounds: Rect(x: 0, y: 0, width: 600, height: 600)).map { ($0.id, $0.flags) })
        XCTAssertEqual(flagged["panel"], [])
        XCTAssertEqual(flagged["row"], ["wider-than-parent"])
        XCTAssertEqual(flagged["a"], ["overlaps-sibling"])
        XCTAssertEqual(flagged["b"], ["overlaps-sibling"])
        XCTAssertEqual(flagged["label"], ["truncated"])
        XCTAssertEqual(flagged["wrapped"], [])
        XCTAssertEqual(flagged["footer"], ["outside-window"])
        XCTAssertEqual(flagged["ax-row"], [])
    }

    func testParentIsTheSameIDElementHoldingTheChild() {
        let flagged = LayoutAnalyzer.flag([
            element("stack", frame: Rect(x: 0, y: 0, width: 220, height: 500)),
            element("stack", frame: Rect(x: 220, y: 0, width: 542, height: 500)),
            element("transcript", frame: Rect(x: 220, y: 0, width: 542, height: 400), parent: "stack"),
        ], bounds: Rect(x: 0, y: 0, width: 800, height: 600))
        XCTAssertEqual(flagged.first { $0.id == "transcript" }?.flags, [])
    }

    func testSummary() {
        let elements = LayoutAnalyzer.flag([
            element("panel", frame: Rect(x: 0, y: 0, width: 360, height: 500)),
            element("row", frame: Rect(x: 0, y: 0, width: 412, height: 20), parent: "panel"),
        ], bounds: Rect(x: 0, y: 0, width: 600, height: 600))
        XCTAssertEqual(LayoutAnalyzer.summary([(title: "demo", elements: elements)]), "1 problem:\ndemo sdui row wider-than-parent (412 > 360)")
        XCTAssertEqual(LayoutAnalyzer.summary([(title: "demo", elements: [])]), "0 problems")
    }

    func testOnlySnapshotFilesAreSwept() {
        XCTAssertTrue(SnapshotFiles.owned("w8294.png"))
        XCTAssertTrue(SnapshotFiles.owned("w12.json"))
        XCTAssertFalse(SnapshotFiles.owned("wallpaper.png"))
        XCTAssertFalse(SnapshotFiles.owned("workflow.json"))
        XCTAssertFalse(SnapshotFiles.owned("w12.txt"))
    }

    func testSnapshotRequestReachesHandler() async throws {
        let core = try await TestCore.start()
        self.core = core
        let client = try await CoreClient.connect(paths: core.paths)
        try await client.register(handles: ["ui.snapshot"])
        let asked = AskedSnapshots()
        client.snapshotHandler = { request in
            asked.requests.append(request)
            return SnapshotResult(windows: [], summary: "0 problems")
        }
        let result = try await client.call(method: "ui.snapshot", params: .object(["out": .string("/tmp/x"), "png": .bool(false)]))
        XCTAssertEqual(result.objectValue?["summary"]?.stringValue, "0 problems")
        XCTAssertEqual(asked.requests, [SnapshotRequest(out: "/tmp/x", png: false, width: nil, height: nil, projectId: nil)])
        await client.close()
    }

    private func element(_ id: String, source: String = "sdui", frame: Rect, parent: String? = nil, idealWidth: Double? = nil, lineHeight: Double? = nil) -> LayoutElement {
        LayoutElement(id: id, source: source, role: "group", label: id, frame: frame, parent: parent, text: nil, idealWidth: idealWidth, lineHeight: lineHeight, menu: nil, flags: [])
    }
}

@MainActor
private final class AskedSnapshots {
    var requests: [SnapshotRequest] = []
}
