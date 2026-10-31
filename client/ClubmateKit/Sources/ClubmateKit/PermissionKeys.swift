import Foundation

import ClubmateProtocol

public enum PermissionKeys {
    public static func option(characters: String?, option: Bool, otherModifiers: Bool, kinds: [PermissionKind]) -> Int? {
        guard option, !otherModifiers, let characters, let digit = Int(characters), digit >= 1, digit <= kinds.count, label(digit - 1, kind: kinds[digit - 1]) != nil else { return nil }
        return digit - 1
    }

    public static func label(_ index: Int, kind: PermissionKind) -> String? {
        guard index < 9, kind != .allowAlways, kind != .rejectAlways else { return nil }
        return "⌥\(index + 1)"
    }
}
