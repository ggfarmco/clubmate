import Foundation

import ClubmateProtocol

public enum PermissionKeys {
    public static func option(characters: String?, option: Bool, otherModifiers: Bool, bare: Bool = false, options: [SDUIPermissionOption]) -> Int? {
        guard !otherModifiers, option || bare, let characters else { return nil }
        if !option, let kind = letters[characters.lowercased()] {
            return options.firstIndex { $0.kind == kind && !$0.menu }
        }
        guard let typed = Int(characters) else { return nil }
        return options.indices.first { digit($0, in: options) == typed }
    }

    private static let letters: [String: PermissionKind] = ["y": .allowOnce, "n": .rejectOnce]

    public static func label(_ index: Int, in options: [SDUIPermissionOption]) -> String? {
        digit(index, in: options).map { "⌥\($0)" }
    }

    private static func digit(_ index: Int, in options: [SDUIPermissionOption]) -> Int? {
        let option = options[index]
        guard !option.menu else { return nil }
        guard options.contains(where: { $0.shortcut != nil }) else {
            return index < 9 && option.kind != .allowAlways && option.kind != .rejectAlways ? index + 1 : nil
        }
        guard let combo = option.shortcut, combo.option, !combo.command, !combo.control, !combo.shift else { return nil }
        return Int(combo.charactersIgnoringModifiers)
    }
}
