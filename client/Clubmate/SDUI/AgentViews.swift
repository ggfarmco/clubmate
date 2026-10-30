import SwiftUI

import ClubmateKit

struct TranscriptNodeView: View {
    let node: SDUINode

    @State private var position = ScrollPosition(edge: .bottom)
    @State private var pinned = true
    @Environment(\.theme) private var theme

    var body: some View {
        let slack = theme.rowHeight
        ScrollView {
            LazyVStack(alignment: .leading, spacing: theme.gutter + theme.spacing.s) {
                SDUIChildren(node: node)
            }
            .padding(.horizontal, theme.gutter * 2)
            .padding(.vertical, theme.gutter + theme.spacing.s)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .onScrollGeometryChange(for: TranscriptScroll.self) { geometry in
            TranscriptScroll(offset: geometry.contentOffset.y, pinned: geometry.contentOffset.y + geometry.containerSize.height >= geometry.contentSize.height - slack)
        } action: { _, scroll in
            pinned = scroll.pinned
            if let id = node.id { TranscriptScroll.saved[id] = scroll }
        }
        .onChange(of: node.children) {
            if pinned { position.scrollTo(edge: .bottom) }
        }
        .onAppear {
            guard let id = node.id, let saved = TranscriptScroll.saved[id], !saved.pinned else { return }
            pinned = false
            position.scrollTo(y: saved.offset)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

private struct TranscriptScroll: Equatable {
    let offset: CGFloat
    let pinned: Bool

    @MainActor static var saved: [String: TranscriptScroll] = [:]
}

struct MessageNodeView: View, Equatable {
    let node: SDUINode

    nonisolated static func == (lhs: MessageNodeView, rhs: MessageNodeView) -> Bool {
        lhs.node == rhs.node
    }

    @Environment(\.theme) private var theme

    var body: some View {
        let text = node.string("text") ?? ""
        switch node.string("role") {
        case "user":
            HStack(spacing: 0) {
                Spacer(minLength: theme.gutter * 8)
                VStack(alignment: .leading, spacing: theme.spacing.s) {
                    MarkdownText(text: text).equatable()
                    ForEach(node.attachments()) { attachment in
                        Text(attachment.label)
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.muted)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                }
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, theme.gutter + theme.spacing.s)
                .padding(.vertical, theme.gutter)
                .background(UnevenRoundedRectangle(topLeadingRadius: 10, bottomLeadingRadius: 10, bottomTrailingRadius: 3, topTrailingRadius: 10).fill(theme.selection))
            }
        case "thought":
            ThoughtView(text: text)
        default:
            MarkdownText(text: text)
                .equatable()
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

private struct ThoughtView: View {
    let text: String

    @State private var expanded = false
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            Button { expanded.toggle() } label: {
                HStack(spacing: theme.spacing.s) {
                    Image(systemName: expanded ? "chevron.down" : "chevron.right")
                        .font(theme.glyph)
                    Text("Thinking")
                        .italic()
                }
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if expanded {
                MarkdownText(text: text, muted: true)
                    .equatable()
                    .padding(.leading, theme.indent)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

struct PlanNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            CardHeading(text: "Plan")
            ForEach(Array(node.planEntries().enumerated()), id: \.offset) { _, entry in
                HStack(alignment: .firstTextBaseline, spacing: theme.spacing.l) {
                    Image(systemName: icon(entry.status))
                        .foregroundStyle(entry.status == .inProgress ? theme.accent : theme.muted)
                    Text(entry.content)
                        .strikethrough(entry.status == .completed)
                        .fontWeight(entry.status == .inProgress ? .semibold : .regular)
                        .foregroundStyle(entry.status == .completed ? theme.muted : theme.foreground)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .agentCard()
    }

    private func icon(_ status: SDUIPlanEntry.Status) -> String {
        switch status {
        case .completed: "checkmark.circle.fill"
        case .inProgress: "play.circle"
        case .pending: "circle"
        }
    }
}

struct ToolCardNodeView: View {
    let node: SDUINode

    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    private var kind: String { node.string("kind") ?? "other" }
    private var oneLine: Bool { kind == "read" || kind == "search" }

    var body: some View {
        let actions = node.cardActions()
        let toolBody = oneLine ? nil : node.toolBody()
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: theme.spacing.l) {
                Image(systemName: icon)
                    .foregroundStyle(theme.muted)
                    .frame(width: theme.indent + theme.spacing.s)
                Text(node.string("title") ?? "")
                    .foregroundStyle(node.string("status") == "cancelled" ? theme.muted : theme.foreground)
                    .lineLimit(oneLine ? 1 : 3)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
                Spacer(minLength: theme.gutter)
                if oneLine { buttons(actions) }
                status
            }
            .padding(.horizontal, theme.gutter + theme.spacing.s)
            .padding(.vertical, theme.spacing.m)
            if let toolBody {
                theme.border.frame(height: 1)
                ToolBodyView(toolBody: toolBody).equatable()
            }
            if node.bool("truncated") == true {
                Text("Output truncated")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .padding(.horizontal, theme.gutter + theme.spacing.s)
                    .padding(.vertical, theme.spacing.s)
            }
            if !oneLine, !actions.isEmpty {
                theme.border.frame(height: 1)
                HStack(spacing: theme.spacing.m) {
                    buttons(actions)
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

    private func buttons(_ actions: [SDUICardAction]) -> some View {
        ForEach(actions) { action in
            CardButton(label: action.label, icon: action.icon) {
                dispatch(node, .named(action.action, action.value))
            }
        }
    }

    @ViewBuilder private var status: some View {
        switch node.string("status") {
        case "pending", "in_progress":
            ProgressView().controlSize(.mini)
        case "failed":
            Label("Failed", systemImage: "xmark.circle.fill")
                .font(theme.uiSmall)
                .foregroundStyle(theme.danger)
        case "cancelled":
            Text("Cancelled")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
        case "completed":
            Image(systemName: "checkmark")
                .font(theme.uiSmall)
                .foregroundStyle(theme.success)
        default:
            EmptyView()
        }
    }

    private var icon: String {
        switch kind {
        case "read": "doc.text"
        case "edit": "pencil"
        case "delete": "trash"
        case "move": "arrow.left.arrow.right"
        case "search": "magnifyingglass"
        case "execute": "terminal"
        case "think": "brain"
        case "fetch": "globe"
        case "switch_mode": "arrow.triangle.swap"
        default: "wrench.and.screwdriver"
        }
    }
}

private struct ToolBodyView: View, Equatable {
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

struct PermissionCardNodeView: View {
    let node: SDUINode

    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        let outcome = node.string("outcome")
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.l) {
                Image(systemName: "hand.raised.fill")
                    .foregroundStyle(theme.warning)
                Text(node.string("text") ?? "")
                    .foregroundStyle(theme.foreground)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: theme.spacing.m) {
                ForEach(node.permissionOptions()) { option in
                    CardButton(
                        label: option.label,
                        icon: outcome == option.id ? "checkmark" : nil,
                        tone: tone(option, outcome: outcome),
                        disabled: outcome != nil
                    ) {
                        dispatch(node, .permission(request: node.string("request") ?? "", option: option.id))
                    }
                }
                if outcome == "cancelled" {
                    Text("Cancelled")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
            }
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8).fill(theme.warning.opacity(outcome == nil ? 0.10 : 0.04)))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(theme.warning.opacity(outcome == nil ? 0.6 : 0.25), lineWidth: 1))
    }

    private func tone(_ option: SDUIPermissionOption, outcome: String?) -> CardButton.Tone {
        if let outcome { return outcome == option.id ? .primary : .plain }
        if option.kind == "allow_once" { return .primary }
        return option.rejects ? .danger : .plain
    }
}

struct CardButton: View {
    enum Tone {
        case plain
        case primary
        case danger
    }

    let label: String
    var icon: String?
    var tone = Tone.plain
    var disabled = false
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: theme.spacing.s) {
                if let icon {
                    Image(systemName: SymbolName.resolve(icon))
                }
                Text(label)
                    .lineLimit(1)
            }
            .font(theme.uiSmall)
            .foregroundStyle(foreground)
            .padding(theme.spacing.chip)
            .background(RoundedRectangle(cornerRadius: 5).fill(fill))
            .overlay(RoundedRectangle(cornerRadius: 5).stroke(tone == .primary ? .clear : theme.border, lineWidth: 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled && tone != .primary ? 0.6 : 1)
        .onHover { hovering = $0 }
    }

    private var foreground: Color {
        switch tone {
        case .primary: theme.background
        case .danger: theme.danger
        case .plain: theme.foreground
        }
    }

    private var fill: Color {
        switch tone {
        case .primary: theme.accent.opacity(hovering && !disabled ? 0.85 : 1)
        default: theme.foreground.opacity(hovering && !disabled ? 0.08 : 0.03)
        }
    }
}

private struct CardHeading: View {
    let text: String

    @Environment(\.theme) private var theme

    var body: some View {
        Text(text.uppercased())
            .font(theme.uiSmall.weight(.semibold))
            .foregroundStyle(theme.muted)
    }
}

extension View {
    func agentCard() -> some View {
        modifier(AgentCard())
    }
}

private struct AgentCard: ViewModifier {
    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        content
            .background(RoundedRectangle(cornerRadius: 8).fill(theme.foreground.opacity(0.03)))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).stroke(theme.border, lineWidth: 1))
    }
}
