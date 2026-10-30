import AppKit
import SwiftUI

import ClubmateKit

enum SymbolName {
    static func resolve(_ name: String?, fallback: String = "questionmark.square") -> String {
        guard let name, NSImage(systemSymbolName: name, accessibilityDescription: nil) != nil else {
            return fallback
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

    var body: some View {
        BadgeLabel(text: node.string("text") ?? "", tone: theme.tone(node.string("tone")) ?? theme.muted)
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

    var body: some View {
        Text(theme.inlineMarkdown(node.string("text") ?? ""))
            .safeLinks()
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .tint(theme.accent)
            .textSelection(.enabled)
            .padding(.horizontal, theme.gutter)
    }
}

struct DiffNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(Array(DiffLine.parse(node.diffText() ?? "").enumerated()), id: \.offset) { _, line in
                Text(line.text.isEmpty ? " " : line.text)
                    .foregroundStyle(color(line.kind))
            }
        }
        .font(theme.ui.monospaced())
        .textSelection(.enabled)
        .padding(.horizontal, theme.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func color(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: theme.success
        case .removed: theme.danger
        case .hunk: theme.accent
        case .context: theme.foreground
        }
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
