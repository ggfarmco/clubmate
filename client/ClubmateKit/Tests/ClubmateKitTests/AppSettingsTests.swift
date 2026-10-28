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
}
