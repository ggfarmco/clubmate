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
        XCTAssertNil(settings.terminalFontSize)

        settings.sidebarSide = .right
        settings.interfaceFontSize = 14
        settings.terminalFontSize = 15
        let saved = AppSettings(defaults: defaults)
        XCTAssertEqual(saved.sidebarSide, .right)
        XCTAssertEqual(saved.interfaceFontSize, 14)
        XCTAssertEqual(saved.terminalFontSize, 15)

        saved.terminalFontSize = nil
        let cleared = AppSettings(defaults: defaults)
        XCTAssertNil(cleared.terminalFontSize)
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

    func testFontSteps() {
        XCTAssertEqual(AppSettings.fontSteps(current: 12, delta: 1, range: 8...32), 13)
        XCTAssertEqual(AppSettings.fontSteps(current: 32, delta: 1, range: 8...32), 32)
        XCTAssertEqual(AppSettings.fontSteps(current: 8, delta: -1, range: 8...32), 8)
    }
}
