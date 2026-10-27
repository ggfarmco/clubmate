import Foundation
import SwiftUI

import GhosttyKit

import ClubmateKit

enum GhosttyConfigReader {
    static func theme(from config: ghostty_config_t, dark: Bool, overlay: URL) -> Theme {
        let fallback = Theme.fallback
        let palette = palette(config)
        let text = GhosttyConfigText(sources(overlay: overlay).compactMap(read))
        return Theme(
            background: color(config, "background") ?? fallback.background,
            foreground: color(config, "foreground") ?? fallback.foreground,
            selection: selection(config, text: text, dark: dark) ?? fallback.selection,
            accent: palette?[4] ?? fallback.accent,
            success: palette?[2] ?? fallback.success,
            warning: palette?[3] ?? fallback.warning,
            danger: palette?[1] ?? fallback.danger,
            fontFamily: string(config, "font-family") ?? text.first("font-family")
        )
    }

    private static func color(_ config: ghostty_config_t, _ key: String) -> Color? {
        var value = ghostty_config_color_s()
        guard key.withCString({ ghostty_config_get(config, &value, $0, UInt(key.utf8.count)) }) else { return nil }
        return Color(red: Double(value.r) / 255, green: Double(value.g) / 255, blue: Double(value.b) / 255)
    }

    private static func string(_ config: ghostty_config_t, _ key: String) -> String? {
        var value: UnsafePointer<CChar>?
        guard key.withCString({ ghostty_config_get(config, &value, $0, UInt(key.utf8.count)) }), let value else { return nil }
        let string = String(cString: value)
        return string.isEmpty ? nil : string
    }

    private static func palette(_ config: ghostty_config_t) -> [Color]? {
        var value = ghostty_config_palette_s()
        let key = "palette"
        guard key.withCString({ ghostty_config_get(config, &value, $0, UInt(key.utf8.count)) }) else { return nil }
        return withUnsafeBytes(of: value.colors) { raw in
            raw.bindMemory(to: ghostty_config_color_s.self).prefix(16).map {
                Color(red: Double($0.r) / 255, green: Double($0.g) / 255, blue: Double($0.b) / 255)
            }
        }
    }

    private static func selection(_ config: ghostty_config_t, text: GhosttyConfigText, dark: Bool) -> Color? {
        if let color = color(config, "selection-background") { return color }
        let value = text.last("selection-background") ?? themeText(text, dark: dark)?.last("selection-background")
        return value.flatMap(GhosttyConfigText.hexColor).map { Color(hex: $0) }
    }

    private static func themeText(_ text: GhosttyConfigText, dark: Bool) -> GhosttyConfigText? {
        guard let value = text.last("theme") else { return nil }
        let name = GhosttyConfigText.themeName(value, dark: dark)
        guard !name.isEmpty else { return nil }
        let candidates = name.hasPrefix("/") ? [URL(filePath: name)] : [
            configHome.appending(path: "ghostty/themes/\(name)"),
            Bundle.main.resourceURL?.appending(path: "ghostty/themes/\(name)"),
        ].compactMap { $0 }
        return candidates.lazy.compactMap(read).first.map { GhosttyConfigText([$0]) }
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
