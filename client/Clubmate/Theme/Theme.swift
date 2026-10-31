import AppKit
import SwiftUI

import ClubmateKit

struct Theme: Sendable {
    var background: Color
    var foreground: Color
    var accent: Color
    var success: Color
    var warning: Color
    var danger: Color
    var fontFamily: String?
    var uiSize: CGFloat = 12
    var name: String?
    var isDark = true
    var backgroundHex = "#1f1f28"
    var foregroundHex = "#dcd7ba"
    var palette: [String] = []

    var muted: Color { foreground.opacity(0.55) }
    var border: Color { foreground.opacity(0.10) }

    var ui: Font { font(size: uiSize) }
    var uiSmall: Font { font(size: uiSize - 1) }
    var title: Font { font(size: uiSize + 1).weight(.semibold) }
    var glyph: Font { .system(size: uiSize - 4, weight: .semibold) }

    var rowHeight: CGFloat { (uiSize * 5 / 3).rounded() }
    let gutter: CGFloat = 8
    let indent: CGFloat = 12
    let spacing = Spacing()

    struct Spacing: Sendable {
        let xs: CGFloat = 2
        let s: CGFloat = 4
        let m: CGFloat = 6
        let l: CGFloat = 8
        let overlay: CGFloat = 48
        let capsule = EdgeInsets(top: 3, leading: 10, bottom: 3, trailing: 10)
        let chip = EdgeInsets(top: 2, leading: 7, bottom: 2, trailing: 7)
        let badge = EdgeInsets(top: 1, leading: 6, bottom: 1, trailing: 6)
        let field = EdgeInsets(top: 0, leading: 6, bottom: 0, trailing: 6)
    }

    var event: ThemeEvent? {
        guard palette.count >= 16 else { return nil }
        return ThemeEvent(name: name, dark: isDark, background: backgroundHex, foreground: foregroundHex, palette: palette)
    }

    func tone(_ name: String?) -> Color? {
        switch name {
        case "accent": accent
        case "success": success
        case "warning": warning
        case "danger": danger
        default: nil
        }
    }

    func scaled(_ size: Double) -> Theme {
        var theme = self
        theme.uiSize = size
        return theme
    }

    func nsFont(size: CGFloat, bold: Bool) -> NSFont {
        let weight: NSFont.Weight = bold ? .semibold : .regular
        guard let fontFamily, !fontFamily.isEmpty, let font = NSFont(name: fontFamily, size: size) else { return .monospacedSystemFont(ofSize: size, weight: weight) }
        return bold ? NSFontManager.shared.convert(font, toHaveTrait: .boldFontMask) : font
    }

    private func font(size: CGFloat) -> Font {
        guard let fontFamily, !fontFamily.isEmpty else { return .system(size: size, design: .monospaced) }
        return .custom(fontFamily, size: size)
    }

    static let fallback = Theme(
        background: Color(hex: 0x1F1F28),
        foreground: Color(hex: 0xDCD7BA),
        accent: Color(hex: 0x7E9CD8),
        success: Color(hex: 0x98BB6C),
        warning: Color(hex: 0xE6C384),
        danger: Color(hex: 0xE46876),
        fontFamily: nil
    )
}

extension Color {
    init(hex: UInt32) {
        self.init(
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255
        )
    }
}

private struct ThemeKey: EnvironmentKey {
    static let defaultValue = Theme.fallback
}

extension EnvironmentValues {
    var theme: Theme {
        get { self[ThemeKey.self] }
        set { self[ThemeKey.self] = newValue }
    }
}

extension Text.TruncationMode {
    static let label: Self = .tail
    static let path: Self = .head
}
