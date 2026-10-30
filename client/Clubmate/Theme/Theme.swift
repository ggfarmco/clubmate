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
    var uiSize: CGFloat = 12

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
        let capsule = EdgeInsets(top: 3, leading: 10, bottom: 3, trailing: 10)
        let chip = EdgeInsets(top: 2, leading: 7, bottom: 2, trailing: 7)
        let field = EdgeInsets(top: 0, leading: 6, bottom: 0, trailing: 6)
    }

    func scaled(_ size: Double) -> Theme {
        var theme = self
        theme.uiSize = size
        return theme
    }

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
