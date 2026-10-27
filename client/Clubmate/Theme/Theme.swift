import SwiftUI

struct Theme: Sendable {
    var background: Color
    var foreground: Color
    var selection: Color
    var accent: Color
    var success: Color
    var warning: Color
    var danger: Color
    var fontFamily: String?

    var muted: Color { foreground.opacity(0.55) }
    var border: Color { foreground.opacity(0.10) }

    var ui: Font { font(size: 12) }
    var uiSmall: Font { font(size: 11) }
    var title: Font { font(size: 13).weight(.semibold) }

    let rowHeight: CGFloat = 22
    let gutter: CGFloat = 8

    private func font(size: CGFloat) -> Font {
        guard let fontFamily, !fontFamily.isEmpty else { return .system(size: size, design: .monospaced) }
        return .custom(fontFamily, size: size)
    }

    static let fallback = Theme(
        background: Color(hex: 0x1F1F28),
        foreground: Color(hex: 0xDCD7BA),
        selection: Color(hex: 0x2D4F67),
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
