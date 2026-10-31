import Foundation

public enum PermissionKeys {
    public static func option(characters: String?, option: Bool, otherModifiers: Bool, count: Int) -> Int? {
        guard option, !otherModifiers, let characters, let digit = Int(characters), (1...min(count, 9)).contains(digit) else { return nil }
        return digit - 1
    }

    public static func label(_ index: Int) -> String? {
        index < 9 ? "⌥\(index + 1)" : nil
    }
}
