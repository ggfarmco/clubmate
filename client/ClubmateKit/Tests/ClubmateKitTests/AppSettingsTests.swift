import Foundation
import XCTest

import ClubmateKit

@MainActor
final class AppSettingsTests: XCTestCase {
    func testAppSettingsDefaults() throws {
        let name = "AppSettingsTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }
        let settings = AppSettings(defaults: defaults)
        XCTAssertEqual(settings.sidebarSide, .left)
        XCTAssertEqual(settings.interfaceFontSize, 12)
        XCTAssertFalse(settings.ghosttyImportOffered)

        settings.sidebarSide = .right
        settings.interfaceFontSize = 14
        settings.ghosttyImportOffered = true
        let saved = AppSettings(defaults: defaults)
        XCTAssertEqual(saved.sidebarSide, .right)
        XCTAssertEqual(saved.interfaceFontSize, 14)
        XCTAssertTrue(saved.ghosttyImportOffered)

        defaults.set(15.0, forKey: "terminalFontSize")
        XCTAssertEqual(saved.takeLegacyTerminalFontSize(), 15)
        XCTAssertNil(saved.takeLegacyTerminalFontSize())
    }

    func testPaneFontStepsOnlyThatPaneAndResetFollowsInterface() throws {
        let name = "AppSettingsTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }
        let settings = AppSettings(defaults: defaults)
        for _ in 0..<20 { settings.stepPaneFont("chat", by: 1) }
        settings.stepPaneFont("agents", by: -1)

        let saved = AppSettings(defaults: defaults)
        XCTAssertEqual(saved.paneFontSize("chat"), AppSettings.interfaceFontRange.upperBound)
        XCTAssertEqual(saved.paneFontSize("agents"), 11)
        XCTAssertEqual(saved.paneFontSize("backlog"), 12)

        saved.resetPaneFont("chat")
        saved.interfaceFontSize = 14
        XCTAssertEqual(saved.paneFontSize("chat"), 14)
    }

    func testSlotKeyedPaneSizesBecomePluginKeyed() throws {
        let name = "AppSettingsTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }
        defaults.set(["backlog.main": 14.0, "backlog.sidebar": 13.0, "chat": 15.0], forKey: "paneFontSizes")
        let settings = AppSettings(defaults: defaults)
        XCTAssertEqual(settings.paneFontSize("backlog"), 14)
        XCTAssertEqual(settings.paneFontSize("chat"), 15)
        XCTAssertEqual(PaneFont.key(slot: "agents.main", composer: true), PaneFont.chat)
    }

    func testPaneFontFamilyFollowsInterfaceUntilOverridden() throws {
        let name = "AppSettingsTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }
        let settings = AppSettings(defaults: defaults)
        XCTAssertEqual(settings.paneFontFamily("cron"), FontFamily.terminal)
        settings.setPaneFontFamily("backlog", FontFamily.system)
        settings.interfaceFontFamily = "Menlo"

        let saved = AppSettings(defaults: defaults)
        XCTAssertEqual(saved.paneFontFamily("backlog"), FontFamily.system)
        XCTAssertEqual(saved.paneFontFamily("cron"), "Menlo")
        saved.setPaneFontFamily("backlog", nil)
        XCTAssertEqual(saved.paneFontFamily("backlog"), "Menlo")
    }

    func testChatFontFollowsInterfaceUntilSet() throws {
        let name = "AppSettingsTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }
        let settings = AppSettings(defaults: defaults)
        settings.interfaceFontSize = 14
        XCTAssertEqual(settings.chatFontSize, 14)
        settings.chatFontSize = 40
        settings.interfaceFontSize = 12
        XCTAssertEqual(AppSettings(defaults: defaults).chatFontSize, AppSettings.interfaceFontRange.upperBound)
        settings.resetPaneFont(PaneFont.chat)
        XCTAssertEqual(settings.chatFontSize, 12)
    }

    func testResetPaneFontsClearsAll() throws {
        let name = "AppSettingsTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }
        let settings = AppSettings(defaults: defaults)
        settings.stepPaneFont("agents", by: 1)
        settings.chatFontSize = 16
        settings.setPaneFontFamily("backlog", FontFamily.system)
        XCTAssertEqual(settings.paneFontOverrideCount, 3)
        settings.resetPaneFonts()
        XCTAssertEqual(AppSettings(defaults: defaults).paneFontOverrideCount, 0)
    }

    func testCodeChatStacksUnderTheEditorBelow1100() {
        XCTAssertEqual([0, 900, 1099, 1100, 1280].map(CodeSection.stacksChat), [false, true, true, false, false])
    }

    func testSectionOrderKeepsUnknownSectionsAtDefaultPosition() {
        XCTAssertEqual(SectionOrder.apply(["d", "gone", "a", "c"], to: ["a", "b", "c", "d", "e"]), ["d", "e", "a", "b", "c"])
    }

    func testMoveSectionKeepsAbsentSectionsInPlace() {
        let cases: [(id: String, target: String, saved: [String], want: [String]?)] = [
            ("c", "a", ["a", "x", "b", "c"], ["c", "x", "a", "b"]),
            ("a", "c", ["a", "x", "b", "c"], ["b", "x", "c", "a"]),
            ("b", "a", [], ["b", "a", "c"]),
            ("a", "a", ["a", "b", "c"], nil),
            ("x", "a", ["a", "x", "b", "c"], nil),
        ]
        for c in cases {
            XCTAssertEqual(SectionOrder.move(c.id, onto: c.target, visible: ["a", "b", "c"], saved: c.saved), c.want, "\(c.id) onto \(c.target)")
        }
    }

    func testMovedSectionOrderPersistsAndKeepsOtherScopes() throws {
        let name = "AppSettingsTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }
        let sections = try JSONDecoder().decode([SectionInfo].self, from: Data(#"""
        [{"id":"a","title":"A","icon":"","order":1,"scope":"workspace","plugin":"p"},
         {"id":"b","title":"B","icon":"","order":2,"scope":"workspace","plugin":"p"},
         {"id":"c","title":"C","icon":"","order":3,"scope":"workspace","plugin":"p"}]
        """#.utf8))
        let settings = AppSettings(defaults: defaults)
        settings.sectionOrder = ["home"]
        settings.moveSection("c", onto: "a", in: sections)
        let saved = AppSettings(defaults: defaults)
        XCTAssertEqual(saved.sectionOrder, ["c", "a", "b", "home"])
        XCTAssertEqual(saved.ordered(sections).map(\.id), ["c", "a", "b"])
    }

    func testFontSteps() {
        XCTAssertEqual(AppSettings.fontSteps(current: 12, delta: 1, range: 8...32), 13)
        XCTAssertEqual(AppSettings.fontSteps(current: 32, delta: 1, range: 8...32), 32)
        XCTAssertEqual(AppSettings.fontSteps(current: 8, delta: -1, range: 8...32), 8)
    }
}
