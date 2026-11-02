import SwiftUI

import ClubmateKit

extension KeyboardShortcut {
    init(_ key: String) {
        let modifiers: [(String, String, EventModifiers)] = [("cmd+", "⌘", .command), ("ctrl+", "⌃", .control), ("opt+", "⌥", .option), ("shift+", "⇧", .shift)]
        let named: [String: KeyEquivalent] = [
            "return": .return, "tab": .tab, "space": .space, "delete": .delete, "esc": .escape,
            "left": .leftArrow, "right": .rightArrow, "up": .upArrow, "down": .downArrow,
        ]
        var rest = Substring(key)
        var flags: EventModifiers = []
        var matched = true
        while matched {
            matched = false
            for (name, glyph, flag) in modifiers {
                for prefix in [name, glyph] where rest.count > prefix.count && rest.hasPrefix(prefix) {
                    rest = rest.dropFirst(prefix.count)
                    flags.insert(flag)
                    matched = true
                }
            }
        }
        self.init(named[String(rest)] ?? KeyEquivalent(Character(rest.lowercased())), modifiers: flags)
    }
}

extension View {
    func keyboardShortcut(_ shortcut: Shortcut, key: String? = nil) -> some View {
        keyboardShortcut(KeyboardShortcut(key ?? shortcut.key))
    }
}
