import Foundation

import GhosttyKit

import ClubmateKit

enum GhosttyConfigReader {
    static let defaults = "theme = dark:Clubmate,light:Clubmate Light"

    static var systemConfig: URL? {
        systemSources.first { FileManager.default.fileExists(atPath: $0.path(percentEncoded: false)) }
    }

    static func theme(from config: ghostty_config_t, file: URL, dark: Bool) -> Theme {
        var theme = Theme.mate(dark: dark)
        let text = GhosttyConfigText([defaults] + [file].compactMap(read))
        theme.fontFamily = string(config, "font-family") ?? text.first("font-family")
        guard let background = color(config, "background"), let foreground = color(config, "foreground"), let palette = palette(config), palette.count >= 16 else { return theme }
        theme.terminal = ThemeEvent(
            name: GhosttyThemeName.resolve(text.last("theme"), dark: dark),
            dark: rgb(background).isDark,
            background: hex(background),
            foreground: hex(foreground),
            palette: palette.map(hex)
        )
        return theme
    }

    static func fontSize(_ config: ghostty_config_t) -> Double? {
        var value: Float = 0
        let key = "font-size"
        guard key.withCString({ ghostty_config_get(config, &value, $0, UInt(key.utf8.count)) }), value > 0 else { return nil }
        return Double(value)
    }

    private static func color(_ config: ghostty_config_t, _ key: String) -> ghostty_config_color_s? {
        var value = ghostty_config_color_s()
        guard key.withCString({ ghostty_config_get(config, &value, $0, UInt(key.utf8.count)) }) else { return nil }
        return value
    }

    private static func rgb(_ value: ghostty_config_color_s) -> RGB {
        RGB(red: Double(value.r) / 255, green: Double(value.g) / 255, blue: Double(value.b) / 255)
    }

    private static func hex(_ value: ghostty_config_color_s) -> String {
        ThemeEvent.hex(red: value.r, green: value.g, blue: value.b)
    }

    private static func string(_ config: ghostty_config_t, _ key: String) -> String? {
        var value: UnsafePointer<CChar>?
        guard key.withCString({ ghostty_config_get(config, &value, $0, UInt(key.utf8.count)) }), let value else { return nil }
        let string = String(cString: value)
        return string.isEmpty ? nil : string
    }

    private static func palette(_ config: ghostty_config_t) -> [ghostty_config_color_s]? {
        var value = ghostty_config_palette_s()
        let key = "palette"
        guard key.withCString({ ghostty_config_get(config, &value, $0, UInt(key.utf8.count)) }) else { return nil }
        return withUnsafeBytes(of: value.colors) { raw in
            Array(raw.bindMemory(to: ghostty_config_color_s.self).prefix(16))
        }
    }

    private static var systemSources: [URL] {
        let support = FileManager.default.homeDirectoryForCurrentUser.appending(path: "Library/Application Support/com.mitchellh.ghostty")
        return [
            configHome.appending(path: "ghostty/config"),
            configHome.appending(path: "ghostty/config.ghostty"),
            support.appending(path: "config"),
            support.appending(path: "config.ghostty"),
        ]
    }

    private static var configHome: URL {
        ProcessInfo.processInfo.environment["XDG_CONFIG_HOME"].flatMap { $0.isEmpty ? nil : URL(filePath: $0) }
            ?? FileManager.default.homeDirectoryForCurrentUser.appending(path: ".config")
    }

    private static func read(_ url: URL) -> String? {
        try? String(contentsOf: url, encoding: .utf8)
    }
}
