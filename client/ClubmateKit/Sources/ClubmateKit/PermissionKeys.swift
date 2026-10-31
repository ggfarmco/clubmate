import Foundation

import ClubmateProtocol

public enum PermissionKeys {
    public static func option(characters: String?, option: Bool, otherModifiers: Bool, bare: Bool = false, kinds: [PermissionKind]) -> Int? {
        guard !otherModifiers, option || bare, let characters else { return nil }
        if !option, let kind = letters[characters.lowercased()] {
            return kinds.firstIndex(of: kind)
        }
        guard let digit = Int(characters), digit >= 1, digit <= kinds.count, label(digit - 1, kind: kinds[digit - 1]) != nil else { return nil }
        return digit - 1
    }

    private static let letters: [String: PermissionKind] = ["y": .allowOnce, "n": .rejectOnce]

    public static func label(_ index: Int, kind: PermissionKind) -> String? {
        guard index < 9, kind != .allowAlways, kind != .rejectAlways else { return nil }
        return "⌥\(index + 1)"
    }
}
