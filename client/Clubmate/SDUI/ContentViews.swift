import AppKit
import SwiftUI

import ClubmateKit

enum SymbolName {
    static func resolve(_ name: String?) -> String {
        guard let name, NSImage(systemSymbolName: name, accessibilityDescription: nil) != nil else {
            return "questionmark.square"
        }
        return name
    }
}

struct TextNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        let text = Text(node.string("text") ?? "")
        switch node.string("style") {
        case "muted": text.font(theme.ui).foregroundStyle(theme.muted)
        case "title": text.font(theme.title).foregroundStyle(theme.foreground)
        case "mono": text.font(.system(size: 12, design: .monospaced)).foregroundStyle(theme.foreground)
        default: text.font(theme.ui).foregroundStyle(theme.foreground)
        }
    }
}

struct BadgeNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    private var tone: Color {
        switch node.string("tone") {
        case "accent": theme.accent
        case "success": theme.success
        case "warning": theme.warning
        case "danger": theme.danger
        default: theme.muted
        }
    }

    var body: some View {
        BadgeLabel(text: node.string("text") ?? "", tone: tone)
    }
}

struct BadgeLabel: View {
    let text: String
    let tone: Color

    @Environment(\.theme) private var theme

    var body: some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(tone)
            .padding(.horizontal, 6)
            .padding(.vertical, 1)
            .background(Capsule().fill(tone.opacity(0.18)))
    }
}

struct IconNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        Image(systemName: SymbolName.resolve(node.string("name")))
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
    }
}

struct MarkdownNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    private var content: AttributedString {
        let source = node.string("text") ?? ""
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        return (try? AttributedString(markdown: source, options: options)) ?? AttributedString(source)
    }

    var body: some View {
        Text(content)
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .tint(theme.accent)
            .textSelection(.enabled)
    }
}

struct EmptyNodeView: View {
    let text: String

    @Environment(\.theme) private var theme

    var body: some View {
        Text(text)
            .font(theme.ui)
            .foregroundStyle(theme.muted)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

struct UnsupportedNodeView: View {
    let type: String

    @Environment(\.theme) private var theme

    var body: some View {
        Text("unsupported: \(type)")
            .font(theme.uiSmall)
            .foregroundStyle(theme.muted)
    }
}
