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
                if toolBody.format == .diff {
                    DiffCardBody(node: node, toolBody: toolBody)
                } else {
                    ToolBodyView(toolBody: toolBody).equatable()
                }
            }
            if node.bool("truncated") == true {
                TruncatedNote()
            }
            if !oneLine, !ToolCardButtons.below(node, body: toolBody).isEmpty {
                theme.border.frame(height: 1)
                HStack(spacing: theme.spacing.m) {
                    ToolCardButtons(node: node, actions: ToolCardButtons.below(node, body: toolBody))
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
    var actions: [SDUICardAction]?
    var shortcuts: [String: String] = [:]
    var icons = true

    @Environment(\.sduiDispatch) private var dispatch

    var body: some View {
        ForEach(actions ?? node.cardActions()) { action in
            CardButton(label: action.label, icon: icons ? action.icon : nil, shortcut: shortcuts[action.action]) {
                dispatch(node, .named(action.action, action.value))
            }
        }
    }

    static func below(_ node: SDUINode, body: SDUIToolBody?) -> [SDUICardAction] {
        let actions = node.cardActions()
        guard body?.format == .diff else { return actions }
        return actions.filter { !SDUINode.fileBarActions.contains($0.action) }
    }
}

struct DiffCardBody: View {
    let node: SDUINode
    let toolBody: SDUIToolBody

    @Environment(\.theme) private var theme
    @Environment(\.fullDiffCard) private var fullDiffCard

    var body: some View {
        let files = toolBody.files
        let actions = node.cardActions().filter { SDUINode.fileBarActions.contains($0.action) }
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: theme.spacing.m) {
                if let first = files.first {
                    DiffFileHeading(file: first)
                }
                Spacer(minLength: theme.gutter)
                ToolCardButtons(node: node, actions: actions, shortcuts: node.id != nil && node.id == fullDiffCard ? ["review": FullDiffKey.label] : [:], icons: false)
            }
            .padding(.horizontal, theme.gutter + theme.spacing.s)
            .padding(.vertical, theme.spacing.s)
            .frame(minHeight: theme.rowHeight)
            theme.border.frame(height: 1)
            CappedScroll {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(files.enumerated()), id: \.offset) { index, file in
                        if index > 0 {
                            DiffFileHeading(file: file)
                                .padding(.horizontal, theme.gutter + theme.spacing.s)
                                .padding(.vertical, theme.spacing.s)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .background(theme.surface1, ignoresSafeAreaEdges: [])
                        }
                        DiffLines(file: file)
                    }
                }
                .font(theme.mono)
                .textSelection(.enabled)
                .padding(.vertical, theme.spacing.m)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.surface2, ignoresSafeAreaEdges: [])
        }
    }
}

private struct DiffFileHeading: View {
    let file: DiffFile

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.m) {
            if let path = file.path {
                Text(path)
                    .foregroundStyle(theme.text2)
                    .lineLimit(1)
                    .truncationMode(.path)
                    .help(path)
                    .layoutPriority(1)
            }
            Text("+\(file.added)").foregroundStyle(theme.success)
            Text("−\(file.removed)").foregroundStyle(theme.danger)
        }
        .font(theme.monoSmall)
    }
}

private struct DiffLines: View {
    let file: DiffFile

    @Environment(\.theme) private var theme

    var body: some View {
        let width = file.numberWidth
        ForEach(Array(file.lines.enumerated()), id: \.offset) { _, line in
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                if width > 0 {
                    Text(number(line.number, width: width))
                        .foregroundStyle(theme.faint)
                        .textSelection(.disabled)
                }
                Text(sign(line.kind))
                    .foregroundStyle(ink(line.kind))
                    .textSelection(.disabled)
                Text(line.text.isEmpty ? " " : line.text)
                    .foregroundStyle(line.kind == .hunk ? theme.accentInk : theme.foreground)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.horizontal, theme.gutter + theme.spacing.s)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(tint(line.kind), ignoresSafeAreaEdges: [])
        }
    }

    private func number(_ value: Int?, width: Int) -> String {
        let text = value.map(String.init) ?? ""
        return String(repeating: " ", count: max(0, width - text.count)) + text
    }

    private func sign(_ kind: DiffLine.Kind) -> String {
        switch kind {
        case .added: "+"
        case .removed: "−"
        default: " "
        }
    }

    private func ink(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: theme.success
        case .removed: theme.danger
        default: theme.faint
        }
    }

    private func tint(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: theme.toneBg(theme.success)
        case .removed: theme.toneBg(theme.danger)
        default: .clear
        }
    }
}

private struct FullDiffCardKey: EnvironmentKey {
    static let defaultValue: String? = nil
}

extension EnvironmentValues {
    var fullDiffCard: String? {
        get { self[FullDiffCardKey.self] }
        set { self[FullDiffCardKey.self] = newValue }
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
        CappedScroll {
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
            .font(theme.mono)
            .textSelection(.enabled)
            .padding(.vertical, theme.spacing.m)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(theme.surface2, ignoresSafeAreaEdges: [])
    }

    private func color(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: theme.success
        case .removed: theme.danger
        case .hunk: theme.accentInk
        case .context: theme.foreground
        }
    }

    private func tint(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: theme.toneBg(theme.success)
        case .removed: theme.toneBg(theme.danger)
        default: .clear
        }
    }
}
