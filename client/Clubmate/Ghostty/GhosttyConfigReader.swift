import Foundation
import SwiftUI

import GhosttyKit

import ClubmateKit

enum GhosttyConfigReader {
    static func theme(from config: ghostty_config_t, overlay: URL, dark: Bool) -> Theme {
        let fallback = Theme.fallback
        let background = rgb(config, "background")
        let foreground = rgb(config, "foreground")
        let palette = palette(config)
        let colors = palette?.map(color)
        let text = GhosttyConfigText(sources(overlay: overlay).compactMap(read))
        return Theme(
            background: background.map(color) ?? fallback.background,
            foreground: foreground.map(color) ?? fallback.foreground,
            accent: colors?[4] ?? fallback.accent,
            success: colors?[2] ?? fallback.success,
            warning: colors?[3] ?? fallback.warning,
            danger: colors?[1] ?? fallback.danger,
            fontFamily: string(config, "font-family") ?? text.first("font-family"),
            name: GhosttyThemeName.resolve(text.last("theme"), dark: dark),
            isDark: background.map(isDark) ?? fallback.isDark,
            backgroundHex: background.map(hex) ?? fallback.backgroundHex,
            foregroundHex: foreground.map(hex) ?? fallback.foregroundHex,
            palette: palette?.map(hex) ?? []
        )
    }

    static func fontSize(_ config: ghostty_config_t) -> Double? {
        var value: Float = 0
        let key = "font-size"
        guard key.withCString({ ghostty_config_get(config, &value, $0, UInt(key.utf8.count)) }), value > 0 else { return nil }
        return Double(value)
    }

    private static func rgb(_ config: ghostty_config_t, _ key: String) -> ghostty_config_color_s? {
        var value = ghostty_config_color_s()
        guard key.withCString({ ghostty_config_get(config, &value, $0, UInt(key.utf8.count)) }) else { return nil }
        return value
    }

    private static func color(_ value: ghostty_config_color_s) -> Color {
        Color(red: Double(value.r) / 255, green: Double(value.g) / 255, blue: Double(value.b) / 255)
    }

    private static func hex(_ value: ghostty_config_color_s) -> String {
        ThemeEvent.hex(red: value.r, green: value.g, blue: value.b)
    }

    private static func isDark(_ value: ghostty_config_color_s) -> Bool {
        0.2126 * Double(value.r) + 0.7152 * Double(value.g) + 0.0722 * Double(value.b) < 128
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

    private static func sources(overlay: URL) -> [URL] {
        let support = FileManager.default.homeDirectoryForCurrentUser.appending(path: "Library/Application Support/com.mitchellh.ghostty")
        return [
            configHome.appending(path: "ghostty/config"),
            configHome.appending(path: "ghostty/config.ghostty"),
            support.appending(path: "config"),
            support.appending(path: "config.ghostty"),
            overlay,
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
