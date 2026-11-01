import Foundation

public struct DocumentTheme: Equatable, Sendable {
    public var background: String
    public var text: String
    public var muted: String
    public var accent: String
    public var border: String
    public var codeBackground: String
    public var success: String
    public var warning: String
    public var danger: String
    public var font: String
    public var codeFont: String
    public var fontSize: Double
    public var dark: Bool

    public init(
        background: String, text: String, muted: String, accent: String, border: String, codeBackground: String,
        success: String, warning: String, danger: String,
        font: String = DocumentTheme.systemFont, codeFont: String = DocumentTheme.monoFont, fontSize: Double = 15, dark: Bool
    ) {
        self.background = background
        self.text = text
        self.muted = muted
        self.accent = accent
        self.border = border
        self.codeBackground = codeBackground
        self.success = success
        self.warning = warning
        self.danger = danger
        self.font = font
        self.codeFont = codeFont
        self.fontSize = fontSize
        self.dark = dark
    }

    public static let systemFont = "-apple-system, system-ui, sans-serif"
    public static let monoFont = "ui-monospace, \"SF Mono\", Menlo, monospace"

    public static func system(dark: Bool) -> DocumentTheme {
        dark
            ? DocumentTheme(background: "#000000", text: "#FFFFFF", muted: "#98989F", accent: "#A6D85B", border: "#38383A", codeBackground: "#1C1C1E", success: "#4FB3A0", warning: "#F2B84B", danger: "#FF6B5E", fontSize: 17, dark: true)
            : DocumentTheme(background: "#FFFFFF", text: "#000000", muted: "#6C6C70", accent: "#357A27", border: "#D1D1D6", codeBackground: "#F2F2F7", success: "#1F7F70", warning: "#B5761E", danger: "#C93A30", fontSize: 17, dark: false)
    }

    public static func mate(dark: Bool) -> DocumentTheme {
        dark
            ? DocumentTheme(background: "#151B17", text: "#E3E3D3", muted: "#7D8077", accent: "#A6D85B", border: "#2E332E", codeBackground: "#232924", success: "#4FB3A0", warning: "#F2B84B", danger: "#FF6B5E", font: monoFont, fontSize: 13, dark: true)
            : DocumentTheme(background: "#F6F1E6", text: "#2B2A22", muted: "#7E7C72", accent: "#357A27", border: "#DED9CE", codeBackground: "#E8E3D8", success: "#1F7F70", warning: "#B5761E", danger: "#C93A30", font: monoFont, fontSize: 13, dark: false)
    }

    public var css: String {
        let vars = [
            ("bg", background), ("text", text), ("muted", muted), ("accent", accent), ("border", border), ("code-bg", codeBackground),
            ("success", success), ("warning", warning), ("danger", danger), ("font", font), ("code-font", codeFont),
            ("size", "\(fontSize)px"), ("dark", dark ? "1" : "0"),
        ]
        return ":root{color-scheme:\(dark ? "dark" : "light");" + vars.map { "--\($0.0):\($0.1);" }.joined() + "}"
    }
}
