import AppKit
import SwiftUI

import ClubmateDocument
import ClubmateKit

struct Theme: Sendable {
    var background: Color
    var foreground: Color
    var accent: Color
    var success: Color
    var warning: Color
    var warningFill: Color
    var onWarning: Color
    var danger: Color
    var fontFamily: String?
    var interfaceFamily = FontFamily.terminal
    var uiSize: CGFloat = 12
    var isDark: Bool
    var backgroundHex: String
    var foregroundHex: String
    var terminal: ThemeEvent?
    var surface1: Color
    var surface2: Color
    var surface3: Color
    var text2: Color
    var muted: Color
    var accentInk: Color
    var onAccent: Color
    private var documentBase: DocumentTheme

    init(background: RGB, foreground: RGB, accent: RGB, success: RGB, warning: RGB, danger: RGB, fontFamily: String?) {
        let surface1 = background.mixed(with: foreground, amount: 0.04)
        let surface3 = background.mixed(with: foreground, amount: 0.11)
        let mutedAlpha = ThemeContrast.alpha(of: foreground, over: surface3, reaching: 4.5)
        func inkRGB(_ tone: RGB) -> RGB { ThemeContrast.ink(tone, toward: foreground, on: background, reaching: 4.5) }
        func ink(_ tone: RGB) -> Color { Color(inkRGB(tone)) }
        func mix(_ amount: Double) -> String { background.mixed(with: foreground, amount: amount).eventHex }
        self.background = Color(background)
        self.foreground = Color(foreground)
        self.accent = Color(accent)
        self.success = ink(success)
        self.warning = ink(warning)
        self.warningFill = Color(warning)
        let dark = background.isDark
        self.onWarning = Color(ThemeContrast.on(warning, candidates: [dark ? background : foreground, RGB(hex: 0x1A1A1A)], reaching: 4.5))
        self.danger = ink(danger)
        self.fontFamily = fontFamily
        self.isDark = dark
        self.backgroundHex = background.eventHex
        self.foregroundHex = foreground.eventHex
        self.surface1 = Color(surface1)
        self.surface2 = Color(background.mixed(with: foreground, amount: 0.07))
        self.surface3 = Color(surface3)
        self.text2 = Color(foreground).opacity(ThemeContrast.alpha(of: foreground, over: surface3, reaching: 7))
        self.muted = Color(foreground).opacity(mutedAlpha)
        self.accentInk = ink(accent)
        self.onAccent = Color(ThemeContrast.on(accent, candidates: [background, RGB(hex: 0xFFFFFF)], reaching: 4.5))
        self.documentBase = DocumentTheme(
            background: background.eventHex, text: foreground.eventHex,
            muted: mix(mutedAlpha), accent: inkRGB(accent).eventHex,
            border: mix(0.12), codeBackground: mix(0.07),
            success: inkRGB(success).eventHex, warning: inkRGB(warning).eventHex, danger: inkRGB(danger).eventHex,
            dark: dark
        )
    }

    var document: DocumentTheme {
        var theme = documentBase
        theme.font = Self.css(uiFamily)
        theme.codeFont = Self.css(fontFamily)
        theme.fontSize = uiSize + 1
        return theme
    }

    var uiFamily: String? {
        interfaceFamily == FontFamily.terminal ? fontFamily : interfaceFamily
    }

    var faint: Color { foreground.opacity(isDark ? 0.42 : 0.52) }
    var border: Color { foreground.opacity(0.12) }
    var borderStrong: Color { foreground.opacity(0.22) }
    var hover: Color { foreground.opacity(0.06) }
    var pressed: Color { foreground.opacity(0.11) }
    var selected: Color { accent.opacity(0.20) }
    var selectedIdle: Color { foreground.opacity(0.09) }
    let focusRing: CGFloat = 1.5

    func toneBg(_ tone: Color) -> Color { tone.opacity(0.15) }
    func toneBorder(_ tone: Color) -> Color { tone.opacity(0.48) }

    var ui: Font { font(size: uiSize) }
    var uiSmall: Font { font(size: uiSize - 1) }
    var strong: Font { font(size: uiSize).weight(.semibold) }
    var title: Font { font(size: uiSize + 1).weight(.semibold) }
    var heading: Font { font(size: uiSize + 4).weight(.semibold) }
    var count: Font { font(size: max(9, uiSize - 2)).weight(.semibold) }
    var glyph: Font { .system(size: uiSize - 4, weight: .semibold) }
    var mono: Font { Self.font(fontFamily, size: uiSize) }
    var monoSmall: Font { Self.font(fontFamily, size: uiSize - 1) }

    var rowHeight: CGFloat { (uiSize * 5 / 3).rounded() }
    var labelMaxWidth: CGFloat { (uiSize * 12).rounded() }
    var rowHeight2: CGFloat { (uiSize * 3).rounded() }
    var control: CGFloat { (uiSize * 2).rounded() }
    var header: CGFloat { (uiSize * 8 / 3).rounded() }
    let rail: CGFloat = 40
    private(set) var gutter: CGFloat = 8
    let indent: CGFloat = 12
    let spacing = Spacing()
    let radius = Radius()

    struct Spacing: Sendable {
        let xs: CGFloat = 2
        let s: CGFloat = 4
        let m: CGFloat = 6
        let l: CGFloat = 8
        let xl: CGFloat = 12
        let xxl: CGFloat = 16
        let section: CGFloat = 24
        let overlay: CGFloat = 48
        let capsule = EdgeInsets(top: 3, leading: 10, bottom: 3, trailing: 10)
        let chip = EdgeInsets(top: 2, leading: 7, bottom: 2, trailing: 7)
        let badge = EdgeInsets(top: 1, leading: 6, bottom: 1, trailing: 6)
        let field = EdgeInsets(top: 0, leading: 6, bottom: 0, trailing: 6)
    }

    struct Radius: Sendable {
        let s: CGFloat = 3
        let m: CGFloat = 5
        let l: CGFloat = 8
        let xl: CGFloat = 10
    }

    var event: ThemeEvent? { terminal }

    func tone(_ name: String?) -> Color? {
        switch name {
        case "accent": accentInk
        case "success": success
        case "warning": warning
        case "danger": danger
        default: nil
        }
    }

    func toneFill(_ name: String?) -> Color? {
        name == "warning" ? warningFill : tone(name)
    }

    static let identities = ["sky", "blue", "indigo", "purple", "magenta", "graphite"]

    func identity(_ token: String?) -> Color {
        Self.identity(token, dark: isDark)
    }

    static func identity(_ token: String?, dark isDark: Bool) -> Color {
        let hex: UInt32 = switch token {
        case "sky": isDark ? 0x5AC8FA : 0x0A78A8
        case "blue": isDark ? 0x3D8BFF : 0x1F62D0
        case "indigo": isDark ? 0x8582FF : 0x5450D6
        case "purple": isDark ? 0xBF7AF0 : 0x8540BF
        case "magenta": isDark ? 0xF06FC8 : 0xB02E8C
        default: isDark ? 0xA1A1A6 : 0x636369
        }
        return Color(RGB(hex: hex))
    }

    static func identityName(_ token: String?) -> String {
        (token.flatMap { identities.contains($0) ? $0 : nil } ?? "graphite").capitalized
    }

    func scaled(_ size: Double) -> Theme {
        var theme = self
        theme.uiSize = size
        return theme
    }

    func interface(family: String, size: Double) -> Theme {
        var theme = scaled(size)
        theme.interfaceFamily = family
        return theme
    }

    func withoutGutter() -> Theme {
        var theme = self
        theme.gutter = 0
        return theme
    }

    func nsFont(size: CGFloat, bold: Bool) -> NSFont {
        Self.nsFont(uiFamily, size: size, bold: bold)
    }

    func nsMono(size: CGFloat, bold: Bool) -> NSFont {
        Self.nsFont(fontFamily, size: size, bold: bold)
    }

    private func font(size: CGFloat) -> Font {
        Self.font(uiFamily, size: size)
    }

    private static func font(_ family: String?, size: CGFloat) -> Font {
        switch family {
        case FontFamily.system: .system(size: size)
        case nil, "", FontFamily.systemMonospaced: .system(size: size, design: .monospaced)
        case let family?: .custom(family, size: size)
        }
    }

    private static func nsFont(_ family: String?, size: CGFloat, bold: Bool) -> NSFont {
        let weight: NSFont.Weight = bold ? .semibold : .regular
        if family == FontFamily.system { return .systemFont(ofSize: size, weight: weight) }
        guard let family, !family.isEmpty, family != FontFamily.systemMonospaced,
              let font = NSFontManager.shared.font(withFamily: family, traits: bold ? .boldFontMask : [], weight: 5, size: size) ?? NSFont(name: family, size: size)
        else { return .monospacedSystemFont(ofSize: size, weight: weight) }
        return bold ? NSFontManager.shared.convert(font, toHaveTrait: .boldFontMask) : font
    }

    private static func css(_ family: String?) -> String {
        switch family {
        case FontFamily.system: DocumentTheme.systemFont
        case nil, "", FontFamily.systemMonospaced: DocumentTheme.monoFont
        case let family?: "\"\(family)\", \(FontFamilies.isFixedPitch(family) ? DocumentTheme.monoFont : DocumentTheme.systemFont)"
        }
    }

    static let fallback = mate(dark: true)

    static func mate(dark: Bool) -> Theme {
        dark
            ? Theme(background: RGB(hex: 0x151B17), foreground: RGB(hex: 0xE3E3D3), accent: RGB(hex: 0xA6D85B), success: RGB(hex: 0x4FB3A0), warning: RGB(hex: 0xF2B84B), danger: RGB(hex: 0xFF6B5E), fontFamily: nil)
            : Theme(background: RGB(hex: 0xF6F1E6), foreground: RGB(hex: 0x2B2A22), accent: RGB(hex: 0x357A27), success: RGB(hex: 0x1F7F70), warning: RGB(hex: 0xB5761E), danger: RGB(hex: 0xC93A30), fontFamily: nil)
    }
}

extension RGB {
    var isDark: Bool {
        0.2126 * red + 0.7152 * green + 0.0722 * blue < 128.0 / 255
    }

    var eventHex: String {
        ThemeEvent.hex(red: UInt8((red * 255).rounded()), green: UInt8((green * 255).rounded()), blue: UInt8((blue * 255).rounded()))
    }
}

extension Color {
    init(_ rgb: RGB) {
        self.init(red: rgb.red, green: rgb.green, blue: rgb.blue)
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

    init(_ truncate: SDUITruncate) {
        switch truncate {
        case .tail: self = .label
        case .middle: self = .middle
        case .head: self = .head
        }
    }
}

struct MacDot: View {
    let colour: String?
    var size: CGFloat = 8
    var dark: Bool?

    @Environment(\.theme) private var theme

    var body: some View {
        Circle()
            .fill(dark.map { Theme.identity(colour, dark: $0) } ?? theme.identity(colour))
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}
