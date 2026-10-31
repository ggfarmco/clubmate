import SwiftUI

import ClubmateKit

struct ToolCardNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    private var oneLine: Bool { node.toolKind == .read || node.toolKind == .search }

    var body: some View {
        let toolBody = oneLine ? nil : node.toolBody()
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: theme.spacing.l) {
                Image(systemName: node.toolKind.symbol)
                    .foregroundStyle(theme.muted)
                    .frame(width: theme.indent + theme.spacing.s)
                Text(node.string("title") ?? "")
                    .foregroundStyle(node.toolStatus == .cancelled ? theme.muted : theme.foreground)
                    .lineLimit(oneLine ? 1 : 3)
                    .truncationMode(.label)
                    .textSelection(.enabled)
                Spacer(minLength: theme.gutter)
                if oneLine { ToolCardButtons(node: node) }
                ToolStatusView(status: node.toolStatus)
            }
            .padding(.horizontal, theme.gutter + theme.spacing.s)
            .padding(.vertical, theme.spacing.m)
            if let toolBody {
                theme.border.frame(height: 1)
                ToolBodyView(toolBody: toolBody).equatable()
            }
            if node.bool("truncated") == true {
                TruncatedNote()
            }
            if !oneLine, !node.cardActions().isEmpty {
                theme.border.frame(height: 1)
                HStack(spacing: theme.spacing.m) {
                    ToolCardButtons(node: node)
                    Spacer(minLength: 0)
                }
                .padding(.horizontal, theme.gutter + theme.spacing.s)
                .padding(.vertical, theme.spacing.m)
            }
        }
        .font(theme.ui)
        .frame(maxWidth: .infinity, alignment: .leading)
        .agentCard()
    }
}

struct ToolCardButtons: View {
    let node: SDUINode

    @Environment(\.sduiDispatch) private var dispatch

    var body: some View {
        ForEach(node.cardActions()) { action in
            CardButton(label: action.label, icon: action.icon) {
                dispatch(node, .named(action.action, action.value))
            }
        }
    }
}

struct TruncatedNote: View {
    @Environment(\.theme) private var theme

    var body: some View {
        Text("Output truncated")
            .font(theme.uiSmall)
            .foregroundStyle(theme.muted)
            .padding(.horizontal, theme.gutter + theme.spacing.s)
            .padding(.vertical, theme.spacing.s)
    }
}

struct ToolStatusView: View {
    let status: ToolStatus

    @Environment(\.theme) private var theme

    var body: some View {
        switch status {
        case .pending, .inProgress:
            ProgressView().controlSize(.mini)
        case .failed:
            Label("Failed", systemImage: "xmark.circle.fill")
                .font(theme.uiSmall)
                .foregroundStyle(theme.danger)
        case .cancelled:
            Text("Cancelled")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
        case .completed:
            Image(systemName: "checkmark")
                .font(theme.uiSmall)
                .foregroundStyle(theme.success)
        case .other:
            EmptyView()
        }
    }
}

struct ToolBodyView: View, Equatable {
    let toolBody: SDUIToolBody

    @Environment(\.theme) private var theme

    nonisolated static func == (lhs: ToolBodyView, rhs: ToolBodyView) -> Bool {
        lhs.toolBody == rhs.toolBody
    }

    var body: some View {
        LazyVStack(alignment: .leading, spacing: 0) {
            ForEach(Array(toolBody.lines.enumerated()), id: \.offset) { _, line in
                Text(line.text.isEmpty ? " " : line.text)
                    .foregroundStyle(color(line.kind))
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, theme.gutter + theme.spacing.s)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(tint(line.kind), ignoresSafeAreaEdges: [])
            }
        }
        .font(theme.ui.monospaced())
        .textSelection(.enabled)
        .padding(.vertical, theme.spacing.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(theme.foreground.opacity(0.04), ignoresSafeAreaEdges: [])
    }

    private func color(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: theme.success
        case .removed: theme.danger
        case .hunk: theme.accent
        case .context: theme.foreground
        }
    }

    private func tint(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: theme.success.opacity(0.12)
        case .removed: theme.danger.opacity(0.12)
        default: .clear
        }
    }
}
