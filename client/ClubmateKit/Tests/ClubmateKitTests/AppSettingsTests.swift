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
        XCTAssertFalse(settings.autoHideSidebar)
        XCTAssertEqual(settings.interfaceFontSize, 12)
        XCTAssertNil(settings.terminalFontSize)
        XCTAssertNil(settings.navigationKeys)

        settings.sidebarSide = .right
        settings.autoHideSidebar = true
        settings.interfaceFontSize = 14
        settings.terminalFontSize = 15
        settings.navigationKeys = false
        let saved = AppSettings(defaults: defaults)
        XCTAssertEqual(saved.sidebarSide, .right)
        XCTAssertTrue(saved.autoHideSidebar)
        XCTAssertEqual(saved.interfaceFontSize, 14)
        XCTAssertEqual(saved.terminalFontSize, 15)
        XCTAssertEqual(saved.navigationKeys, false)

        saved.terminalFontSize = nil
        saved.navigationKeys = nil
        let cleared = AppSettings(defaults: defaults)
        XCTAssertNil(cleared.terminalFontSize)
        XCTAssertNil(cleared.navigationKeys)
    }

    func testAutoHideIsTransientAndCodeOnly() throws {
        let name = "AppSettingsTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        defer { defaults.removePersistentDomain(forName: name) }
        let settings = AppSettings(defaults: defaults)
        XCTAssertFalse(settings.hidesSidebar(CodeSection.id, manual: false, autoHidden: true))
        XCTAssertTrue(settings.hidesSidebar(CodeSection.id, manual: true, autoHidden: false))
        settings.autoHideSidebar = true
        XCTAssertTrue(settings.hidesSidebar(CodeSection.id, manual: false, autoHidden: true))
        XCTAssertFalse(settings.hidesSidebar("terminals", manual: false, autoHidden: true))
        XCTAssertFalse(settings.autoHides("terminals"))
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
        [{"id":"a","title":"A","icon":"","order":1,"scope":"project","plugin":"p"},
         {"id":"b","title":"B","icon":"","order":2,"scope":"project","plugin":"p"},
         {"id":"c","title":"C","icon":"","order":3,"scope":"project","plugin":"p"}]
        """#.utf8))
        let settings = AppSettings(defaults: defaults)
        settings.sectionOrder = ["home"]
        settings.moveSection("c", onto: "a", in: sections)
        let saved = AppSettings(defaults: defaults)
        XCTAssertEqual(saved.sectionOrder, ["c", "a", "b", "home"])
        XCTAssertEqual(saved.ordered(sections).map(\.id), ["c", "a", "b"])
    }

    func testSidebarEdgeMatchesSide() {
        func request(_ edge: String?) -> FocusRequest {
            FocusRequest(slot: "editor-nvim.sidebar", serial: 1, edge: edge)
        }
        XCTAssertTrue(request("h").accepts(side: .left))
        XCTAssertFalse(request("h").accepts(side: .right))
        XCTAssertTrue(request("l").accepts(side: .right))
        XCTAssertFalse(request("l").accepts(side: .left))
        XCTAssertTrue(request(nil).accepts(side: .left))
        XCTAssertTrue(request(nil).accepts(side: .right))
    }

    func testFontSteps() {
        XCTAssertEqual(AppSettings.fontSteps(current: 12, delta: 1, range: 8...32), 13)
        XCTAssertEqual(AppSettings.fontSteps(current: 32, delta: 1, range: 8...32), 32)
        XCTAssertEqual(AppSettings.fontSteps(current: 8, delta: -1, range: 8...32), 8)
    }

    func testSkippedKeysMessage() {
        let result = JSONValue.object(["skipped": .array([.string("<C-j>"), .string("<C-k>")])])
        XCTAssertEqual(CodeSection.skippedKeysMessage(result), "Ctrl+J, Ctrl+K left alone: you already map them")
        XCTAssertNil(CodeSection.skippedKeysMessage(.object(["skipped": .array([])])))
    }
}
