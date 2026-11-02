import Foundation
import XCTest

import ClubmateKit

final class SettingsPageTests: XCTestCase {
    func testSettingsPageArgument() {
        XCTAssertEqual(SettingsPage(argument: "general"), .general)
        XCTAssertEqual(SettingsPage(argument: "remote"), .plugin("remote"))
        XCTAssertEqual(SettingsPage(argument: "devices"), .plugin("remote"))
        XCTAssertEqual(SettingsPage(argument: "voice"), .plugin("voice"))
        XCTAssertEqual(SettingsPage.slot(plugin: "voice"), "voice.settings")
    }

    func testDeclaresSettingsFromManifestViews() throws {
        let plugins = try JSONDecoder().decode([PluginStatus].self, from: Data(#"""
        [{"id":"voice","state":"running","pid":1,"restarts":0,"lastError":"",
          "manifest":{"name":"Voice","version":"1","contributes":{"sections":[],"commands":[],"views":[{"slot":"voice.settings"}]}}},
         {"id":"beta","state":"running","pid":2,"restarts":0,"lastError":"",
          "manifest":{"name":"Beta","version":"1","contributes":{"sections":[],"commands":[],"views":[{"slot":"alpha.settings"},{"slot":"beta.sidebar"}]}}},
         {"id":"gamma","state":"running","pid":3,"restarts":0,"lastError":"",
          "manifest":{"name":"Gamma","version":"1","contributes":{"sections":[],"commands":[],"views":null}}}]
        """#.utf8))
        XCTAssertEqual(plugins.map(\.declaresSettings), [true, false, false])
        XCTAssertEqual(plugins[1].manifest.contributes.views.map(\.slot), ["alpha.settings", "beta.sidebar"])
    }
}
