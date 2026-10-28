import Foundation
import XCTest

import ClubmateKit

@MainActor
final class WindowLayoutTests: XCTestCase {
    private func makeDefaults() throws -> UserDefaults {
        let name = "WindowLayoutTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        addTeardownBlock { defaults.removePersistentDomain(forName: name) }
        return defaults
    }

    func testLayoutMigratesOpenProjects() throws {
        let defaults = try makeDefaults()
        defaults.set(["a", "b"], forKey: "openProjects")
        let layout = WindowLayout(defaults: defaults)
        XCTAssertEqual(layout.windows.count, 1)
        XCTAssertEqual(layout.windows[0].workspaces, ["a", "b"])
        XCTAssertEqual(layout.windows[0].active, "b")
        XCTAssertNil(defaults.object(forKey: "openProjects"))
        XCTAssertEqual(WindowLayout(defaults: defaults).windows, layout.windows)
    }

    func testOpenTargetsKeyWindowOrExisting() throws {
        let layout = WindowLayout(defaults: try makeDefaults())
        let w1 = layout.newWindow()
        let w2 = layout.newWindow()
        layout.open("a", preferring: w1)
        XCTAssertEqual(layout.open("a", preferring: w2), w1)
        XCTAssertEqual(layout.window(w1)?.active, "a")
        XCTAssertEqual(layout.open("c", preferring: w2), w2)
        XCTAssertEqual(layout.window(w2)?.workspaces, ["c"])
    }

    func testRemoveActivatesNeighbour() throws {
        let layout = WindowLayout(defaults: try makeDefaults())
        let w = layout.newWindow()
        for ws in ["a", "b", "c"] { layout.open(ws, preferring: w) }
        layout.activate("b", in: w)
        layout.close("b", in: w)
        XCTAssertEqual(layout.window(w)?.active, "c")
        layout.close("c", in: w)
        XCTAssertEqual(layout.window(w)?.active, "a")
        layout.close("a", in: w)
        XCTAssertNil(layout.window(w)?.active)
        XCTAssertEqual(layout.window(w)?.workspaces, [])
    }

    func testNextWraps() throws {
        let layout = WindowLayout(defaults: try makeDefaults())
        let w = layout.newWindow()
        layout.open("a", preferring: w)
        layout.open("b", preferring: w)
        layout.next(in: w)
        XCTAssertEqual(layout.window(w)?.active, "a")
        layout.previous(in: w)
        XCTAssertEqual(layout.window(w)?.active, "b")
    }

    func testLayoutSurvivesQuit() throws {
        let defaults = try makeDefaults()
        let layout = WindowLayout(defaults: defaults)
        let w = layout.newWindow()
        layout.open("a", preferring: w)
        layout.terminating()
        layout.closeWindow(w)
        XCTAssertEqual(WindowLayout(defaults: defaults).window(w)?.workspaces, ["a"])
    }

    func testDeckSurvivesWorkspaceSwitch() throws {
        let layout = WindowLayout(defaults: try makeDefaults())
        let w = layout.newWindow()
        layout.open("a", preferring: w)
        layout.open("b", preferring: w)
        layout.activate("b", in: w)
        XCTAssertFalse(SurfaceDeckPolicy.shouldCloseDeck(workspace: "a", layout: layout))
        layout.close("a", in: w)
        XCTAssertTrue(SurfaceDeckPolicy.shouldCloseDeck(workspace: "a", layout: layout))
        layout.closeWindow(w)
        XCTAssertTrue(SurfaceDeckPolicy.shouldCloseDeck(workspace: "b", layout: layout))
    }
}
