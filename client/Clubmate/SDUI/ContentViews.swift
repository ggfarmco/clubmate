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
        Text(node.string("text") ?? "")
            .font(node.string("style") == "title" ? theme.title : theme.ui)
            .foregroundStyle(node.string("style") == "muted" ? theme.muted : theme.foreground)
            .padding(.horizontal, theme.gutter)
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
            .padding(.horizontal, theme.gutter)
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
            .padding(.horizontal, theme.gutter)
    }
}

struct MarkdownNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    private var content: AttributedString {
        let source = node.string("text") ?? ""
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        var content = (try? AttributedString(markdown: source, options: options)) ?? AttributedString(source)
        for run in content.runs where run.inlinePresentationIntent?.contains(.code) == true {
            content[run.range].font = theme.ui
            content[run.range].backgroundColor = theme.foreground.opacity(0.1)
        }
        return content
    }

    var body: some View {
        Text(content)
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .tint(theme.accent)
            .textSelection(.enabled)
            .padding(.horizontal, theme.gutter)
    }
}

struct EmptyNodeView: View {
    let text: String

    @Environment(\.theme) private var theme

    var body: some View {
        Text(text)
            .font(theme.ui)
            .foregroundStyle(theme.muted)
            .padding(.horizontal, theme.gutter)
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
            .padding(.horizontal, theme.gutter)
    }
}
