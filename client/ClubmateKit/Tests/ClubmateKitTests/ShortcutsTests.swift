import Foundation
import XCTest

import ClubmateKit

final class ShortcutsTests: XCTestCase {
    func testEveryAppKeyIsInTheShortcutsDoc() throws {
        let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let doc = try String(contentsOf: repo.appending(path: "docs/shortcuts.md"), encoding: .utf8).lowercased()
        for shortcut in AppShortcuts.groups.flatMap(\.shortcuts) {
            for label in shortcut.labels {
                XCTAssertTrue(doc.contains(label.lowercased()), "\(shortcut.id): \(label)")
            }
        }
    }

    func testSearchMatchesKeysAndActionText() {
        let group = ShortcutGroup(id: "app", title: "App and windows", shortcuts: [
            Shortcut("new", ["cmd+n"], "New terminal"),
            Shortcut("home", ["cmd+shift+n"], "New Home Terminal"),
            Shortcut("palette", ["cmd+k"], "Command Palette", when: "Anywhere"),
        ])
        let cases: [(String, [String])] = [
            ("⌘n", ["new", "home"]),
            ("cmd+n", ["new"]),
            ("NEW home", ["home"]),
            ("anywhere", ["palette"]),
            ("", ["new", "home", "palette"]),
        ]
        for (query, ids) in cases {
            XCTAssertEqual(group.filtered(query)?.shortcuts.map(\.id) ?? [], ids, query)
        }
        XCTAssertNil(group.filtered("⌥x"))
    }
}
