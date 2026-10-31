import SwiftUI

import ClubmateKit

struct ToolActionsView: View {
    let cards: [SDUIChild]

    @Environment(\.theme) private var theme

    var body: some View {
        let summary = ToolActionsSummary(cards.map(\.node))
        let ids = cards.map(\.id)
        let state = ToolExpansion.shared.state
        let expanded = state.isExpanded(group: ids)
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            HStack(spacing: theme.spacing.s) {
                Button { ToolExpansion.shared.toggle(group: ids) } label: {
                    HStack(spacing: theme.spacing.s) {
                        Chevron(expanded: expanded)
                        Text(summary.text)
                            .lineLimit(1)
                            .truncationMode(.label)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(summary.text)
                .accessibilityValue(expanded ? "Expanded" : "Collapsed")
                if summary.failed > 0 {
                    Label("\(summary.failed) failed", systemImage: "xmark.circle.fill")
                        .foregroundStyle(theme.danger)
                }
                if let running = summary.running {
                    ProgressView().controlSize(.mini)
                    if !expanded {
                        Text("running: \(running)")
                            .lineLimit(1)
                            .truncationMode(.label)
                    }
                }
            }
            .font(theme.uiSmall)
            .foregroundStyle(theme.muted)
            if expanded {
                LazyVStack(alignment: .leading, spacing: theme.spacing.xs) {
                    ForEach(cards) { card in
                        ToolActionLine(id: card.id, node: card.node, expanded: state.isExpanded(card: card.id)).equatable()
                    }
                }
                .padding(.leading, theme.indent)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct ToolActionLine: View, Equatable {
    let id: String
    let node: SDUINode
    let expanded: Bool

    nonisolated static func == (lhs: ToolActionLine, rhs: ToolActionLine) -> Bool {
        lhs.id == rhs.id && lhs.expanded == rhs.expanded && lhs.node == rhs.node
    }

    @Environment(\.theme) private var theme

    var body: some View {
        let toolBody = node.toolBody()
        let actions = node.cardActions()
        let truncated = node.bool("truncated") == true
        let expandable = toolBody != nil || truncated || !actions.isEmpty
        let title = ToolLineTitle(node)
        let fullTitle = node.string("title") ?? ""
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            HStack(spacing: theme.spacing.l) {
                if expandable {
                    Button { ToolExpansion.shared.toggle(card: id) } label: {
                        HStack(spacing: theme.spacing.s) {
                            Chevron(expanded: expanded)
                            kindIcon
                            if !expanded { heading(title.text, changes: toolBody?.changes) }
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(fullTitle)
                    .accessibilityValue(expanded ? "Expanded" : "Collapsed")
                    if expanded {
                        heading(fullTitle, changes: toolBody?.changes)
                            .textSelection(.enabled)
                    }
                } else {
                    HStack(spacing: theme.spacing.s) {
                        Chevron(expanded: false).hidden()
                        kindIcon
                        heading(title.text, changes: nil)
                    }
                }
                Spacer(minLength: theme.gutter)
                ToolStatusView(status: node.toolStatus)
            }
            .help(title.help ?? fullTitle)
            if expanded, expandable {
                VStack(alignment: .leading, spacing: 0) {
                    if let toolBody {
                        ToolBodyView(toolBody: toolBody).equatable()
                    }
                    if truncated {
                        TruncatedNote()
                    }
                    if !actions.isEmpty {
                        if toolBody != nil || truncated { theme.border.frame(height: 1) }
                        HStack(spacing: theme.spacing.m) {
                            ToolCardButtons(node: node)
                            Spacer(minLength: 0)
                        }
                        .padding(.horizontal, theme.gutter + theme.spacing.s)
                        .padding(.vertical, theme.spacing.m)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .agentCard()
            }
        }
        .font(theme.ui)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var kindIcon: some View {
        Image(systemName: node.toolKind.symbol)
            .foregroundStyle(theme.muted)
            .frame(width: theme.indent + theme.spacing.s)
            .accessibilityHidden(true)
    }

    private func heading(_ text: String, changes: (added: Int, removed: Int)?) -> some View {
        HStack(spacing: theme.spacing.s) {
            Text(text)
                .foregroundStyle(node.toolStatus == .cancelled ? theme.muted : theme.foreground)
                .lineLimit(expanded ? 3 : 1)
                .truncationMode(.label)
            if let changes {
                Text("+\(changes.added)").foregroundStyle(theme.success)
                Text("−\(changes.removed)").foregroundStyle(theme.danger)
            }
        }
    }
}

private struct Chevron: View {
    let expanded: Bool

    @Environment(\.theme) private var theme

    var body: some View {
        Image(systemName: expanded ? "chevron.down" : "chevron.right")
            .font(theme.glyph)
            .foregroundStyle(theme.muted)
            .accessibilityHidden(true)
    }
}
