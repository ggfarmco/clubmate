import Foundation

public enum GhosttyThemeName {
    public static func resolve(_ value: String?, dark: Bool) -> String? {
        guard let value = value?.trimmingCharacters(in: .whitespaces), !value.isEmpty else { return nil }
        guard value.contains(":") else { return value }
        let side = dark ? "dark" : "light"
        for part in value.split(separator: ",") {
            let pair = part.split(separator: ":", maxSplits: 1).map { $0.trimmingCharacters(in: .whitespaces) }
            if pair.count == 2, pair[0] == side, !pair[1].isEmpty { return pair[1] }
        }
        return nil
    }
}

public struct ThemeEvent: Codable, Equatable, Sendable {
    public static let topic = "theme.changed"

    public var name: String?
    public var dark: Bool
    public var background: String
    public var foreground: String
    public var palette: [String]

    public init(name: String?, dark: Bool, background: String, foreground: String, palette: [String]) {
        self.name = name
        self.dark = dark
        self.background = background
        self.foreground = foreground
        self.palette = Array(palette.prefix(16))
    }

    public static func hex(red: UInt8, green: UInt8, blue: UInt8) -> String {
        String(format: "#%02x%02x%02x", red, green, blue)
    }

    private enum CodingKeys: String, CodingKey {
        case name, dark, background, foreground, palette
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(name, forKey: .name)
        try container.encode(dark, forKey: .dark)
        try container.encode(background, forKey: .background)
        try container.encode(foreground, forKey: .foreground)
        try container.encode(palette, forKey: .palette)
    }
}
