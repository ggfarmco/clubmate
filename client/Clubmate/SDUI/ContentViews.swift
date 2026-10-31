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
        let text = node.string("text") ?? ""
        if node.string("style") == "title" {
            Text(text)
                .font(theme.title)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .truncationMode(.label)
                .help(text)
                .layoutPriority(1)
                .padding(.horizontal, theme.gutter)
        } else {
            Text(text)
                .font(theme.ui)
                .foregroundStyle(node.string("style") == "muted" ? theme.muted : theme.foreground)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, theme.gutter)
        }
    }
}

struct BadgeNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        BadgeLabel(text: node.string("text") ?? "", tone: theme.tone(node.string("tone")))
            .padding(.horizontal, theme.gutter)
    }
}

struct BadgeLabel: View {
    let text: String
    let tone: Color?

    @Environment(\.theme) private var theme

    var body: some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(tone ?? theme.text2)
            .padding(theme.spacing.badge)
            .background(Capsule().fill(tone.map(theme.toneBg) ?? theme.hover))
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
        case .hunk: theme.accentInk
        case .context: theme.foreground
        }
    }
}

struct HeaderNodeView: View {
    let node: SDUINode

    @State private var width: CGFloat = 0
    @Environment(\.theme) private var theme

    var body: some View {
        let title = node.string("title") ?? ""
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                if let icon = node.string("icon") {
                    Image(systemName: SymbolName.resolve(icon))
                        .foregroundStyle(theme.muted)
                }
                Text(title)
                    .font(theme.heading)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(2)
                    .truncationMode(.label)
                    .fixedSize(horizontal: false, vertical: true)
                    .help(title)
                    .layoutPriority(1)
                ForEach(Array(node.headerBadges().enumerated()), id: \.offset) { _, badge in
                    BadgeLabel(text: badge.text, tone: theme.tone(badge.tone))
                        .fixedSize()
                }
                Spacer(minLength: 0)
            }
            if let meta = node.string("meta") {
                Text(meta)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !node.children.isEmpty {
                if width > 0 && width < 480 {
                    overflowRow
                } else {
                    ViewThatFits(in: .horizontal) {
                        HStack(spacing: theme.spacing.m) {
                            SDUIChildren(node: node)
                        }
                        overflowRow
                    }
                }
            }
        }
        .padding(.horizontal, theme.gutter)
        .padding(.vertical, theme.spacing.l)
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
    }

    private var overflowRow: some View {
        let actions = node.headerOverflow()
        return HStack(alignment: .top, spacing: theme.spacing.m) {
            FlowLayout(spacing: theme.spacing.m) {
                ForEach(actions.shown) { child in
                    SDUINodeView(node: child.node)
                }
            }
            .layoutPriority(1)
            if !actions.more.isEmpty {
                Spacer(minLength: 0)
                HeaderMoreMenu(id: "\(node.id ?? "header").more", actions: actions.more.map(\.node))
            }
        }
    }
}

private struct HeaderMoreMenu: View {
    let id: String
    let actions: [SDUINode]

    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    var body: some View {
        let presses = actions.map { ButtonPress(node: $0, dispatch: dispatch, form: form) }
        Menu {
            ForEach(Array(presses.enumerated()), id: \.offset) { _, press in
                Button(role: press.node.string("variant") == "danger" ? .destructive : nil, action: press.click) {
                    Text(title(press))
                }
                .disabled(press.disabled)
            }
        } label: {
            Image(systemName: "ellipsis")
                .font(theme.ui)
                .foregroundStyle(theme.text2)
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .fixedSize()
        .frame(height: theme.control)
        .environment(\.colorScheme, theme.isDark ? .dark : .light)
        .help("More actions")
        .background {
            ForEach(Array(presses.enumerated()), id: \.offset) { _, press in
                Color.clear.keyMonitor(active: press.combo != nil && !press.disabled, handle: press.handle)
            }
        }
        .testMenu(id, presses.map { LayoutMenuItem(title: title($0), enabled: !$0.disabled, destructive: $0.node.string("variant") == "danger") })
    }

    private func title(_ press: ButtonPress) -> String {
        let label = press.node.string("label") ?? ""
        return press.combo.map { "\(label)    \($0.label)" } ?? label
    }
}

struct EmptyNodeView: View {
    let node: SDUINode

    init(node: SDUINode) {
        self.node = node
    }

    init(text: String) {
        node = SDUINode(type: "empty", props: ["text": .string(text)])
    }

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            if let icon = node.string("icon") {
                Image(systemName: SymbolName.resolve(icon))
                    .font(.system(size: 16))
                    .foregroundStyle(theme.muted)
            }
            if let title = node.string("title") {
                Text(title)
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
            }
            Text(node.string("text") ?? "")
                .font(theme.ui)
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            if let hint = node.string("hint") {
                Text(hint)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.text2)
                    .textSelection(.enabled)
                    .padding(.horizontal, theme.spacing.m)
                    .padding(.vertical, theme.spacing.xs)
                    .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(theme.surface2))
            }
            SDUIChildren(node: node)
        }
        .padding(theme.gutter * 2)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
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
