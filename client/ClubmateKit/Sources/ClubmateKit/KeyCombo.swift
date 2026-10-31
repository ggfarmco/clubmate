import Foundation

public struct KeyCombo: Equatable, Sendable {
    public let keyCode: UInt16
    public let command: Bool
    public let control: Bool
    public let option: Bool
    public let shift: Bool
    public let characters: String
    public let charactersIgnoringModifiers: String

    public init?(_ combo: String) {
        var parts = combo.lowercased().split(separator: "+", omittingEmptySubsequences: false).map(String.init)
        guard let name = parts.popLast(), let key = Self.keys[name] else { return nil }
        var command = false, control = false, option = false, shift = false
        for part in parts {
            switch part {
            case "cmd": command = true
            case "ctrl": control = true
            case "opt": option = true
            case "shift": shift = true
            default: return nil
            }
        }
        let typed = shift ? key.shifted ?? key.text : key.text
        self.keyCode = key.code
        self.command = command
        self.control = control
        self.option = option
        self.shift = shift
        charactersIgnoringModifiers = typed
        if control, let scalar = typed.lowercased().unicodeScalars.first, ("a"..."z").contains(scalar) {
            characters = String(UnicodeScalar(UInt8(scalar.value & 0x1f)))
        } else {
            characters = typed
        }
    }

    private struct Key {
        let code: UInt16
        let text: String
        let shifted: String?
    }

    private static let keys: [String: Key] = {
        var keys: [String: Key] = [
            "return": Key(code: 36, text: "\r", shifted: nil),
            "tab": Key(code: 48, text: "\t", shifted: nil),
            "space": Key(code: 49, text: " ", shifted: nil),
            "delete": Key(code: 51, text: "\u{7f}", shifted: nil),
            "esc": Key(code: 53, text: "\u{1b}", shifted: nil),
            "left": Key(code: 123, text: "\u{f702}", shifted: nil),
            "right": Key(code: 124, text: "\u{f703}", shifted: nil),
            "down": Key(code: 125, text: "\u{f701}", shifted: nil),
            "up": Key(code: 126, text: "\u{f700}", shifted: nil),
        ]
        let letters: [(String, UInt16)] = [
            ("a", 0), ("s", 1), ("d", 2), ("f", 3), ("h", 4), ("g", 5), ("z", 6), ("x", 7), ("c", 8), ("v", 9), ("b", 11), ("q", 12), ("w", 13),
            ("e", 14), ("r", 15), ("y", 16), ("t", 17), ("o", 31), ("u", 32), ("i", 34), ("p", 35), ("l", 37), ("j", 38), ("k", 40), ("n", 45), ("m", 46),
        ]
        for (letter, code) in letters {
            keys[letter] = Key(code: code, text: letter, shifted: letter.uppercased())
        }
        let symbols: [(String, UInt16, String)] = [
            ("1", 18, "!"), ("2", 19, "@"), ("3", 20, "#"), ("4", 21, "$"), ("6", 22, "^"), ("5", 23, "%"), ("=", 24, "+"), ("9", 25, "("), ("7", 26, "&"),
            ("-", 27, "_"), ("8", 28, "*"), ("0", 29, ")"), ("]", 30, "}"), ("[", 33, "{"), ("'", 39, "\""), (";", 41, ":"), ("\\", 42, "|"), (",", 43, "<"),
            ("/", 44, "?"), (".", 47, ">"), ("`", 50, "~"),
        ]
        for (symbol, code, shifted) in symbols {
            keys[symbol] = Key(code: code, text: symbol, shifted: shifted)
        }
        return keys
    }()
}
