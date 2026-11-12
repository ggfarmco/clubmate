import Foundation

public enum TerminalDropItem: Equatable, Sendable {
    case file(String)
    case text(String)
}

public enum TerminalDrop {
    public static func text(_ items: [TerminalDropItem]) -> String? {
        let parts = items.map { item in
            switch item {
            case let .file(path): quote(path)
            case let .text(text): text
            }
        }
        let joined = parts.joined(separator: " ")
        return joined.isEmpty ? nil : joined
    }

    public static func quote(_ word: String) -> String {
        if !word.isEmpty, word.allSatisfy(isSafe) { return word }
        return "'" + word.replacingOccurrences(of: "'", with: #"'"'"'"#) + "'"
    }

    private static func isSafe(_ character: Character) -> Bool {
        character.isLetter || character.isNumber || "_@%+=:,./-".contains(character)
    }
}
