import SwiftUI

import ClubmateKit

struct TranscriptNodeView: View {
    let node: SDUINode

    @State private var position = ScrollPosition(edge: .bottom)
    @State private var pinned = true
    @Environment(\.theme) private var theme

    var body: some View {
        let slack = theme.rowHeight
        let items = node.transcriptItems()
        ScrollView {
            LazyVStack(alignment: .leading, spacing: theme.gutter + theme.spacing.s) {
                ForEach(items) { item in
                    TranscriptItemView(item: item)
                }
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
            ToolExpansion.shared.prune(node)
        }
        .onChange(of: ToolExpansion.shared.opened) { _, opened in
            guard let opened, let last = items.last, last.contains(opened) else { return }
            Task { position.scrollTo(edge: .bottom) }
        }
        .onAppear {
            guard let id = node.id, let saved = TranscriptScroll.saved[id], !saved.pinned else { return }
            pinned = false
            position.scrollTo(y: saved.offset)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .clipped()
    }
}

private struct TranscriptItemView: View {
    let item: TranscriptItem

    var body: some View {
        switch item {
        case let .node(child): SDUINodeView(node: child.node)
        case let .action(child): ToolActionLine(id: child.id, node: child.node, expanded: ToolExpansion.shared.state.isExpanded(card: child.id)).equatable()
        case let .actions(_, cards): ToolActionsView(cards: cards)
        }
    }
}

@MainActor @Observable private final class ToolExpansion {
    static let shared = ToolExpansion()

    private(set) var state = ToolExpansionState()
    private(set) var opened: String?

    func toggle(card id: String) {
        opened = state.toggle(card: id) ? id : nil
    }

    func toggle(group ids: [String]) {
        opened = state.toggle(group: ids) ? ids.first : nil
    }

    func prune(_ transcript: SDUINode) {
        var next = state
        next.prune(transcript)
        if next != state { state = next }
    }
}

private extension TranscriptItem {
    func contains(_ id: String) -> Bool {
        if case let .actions(_, cards) = self, cards.contains(where: { $0.id == id }) { return true }
        return self.id == id
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

private struct ToolActionsView: View {
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
                            .truncationMode(.tail)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(summary.text)
                .accessibilityValue(expanded ? "Expanded" : "Collapsed")
                if let running = summary.running {
                    ProgressView().controlSize(.mini)
                    if !expanded {
                        Text("running: \(running)")
                            .lineLimit(1)
                            .truncationMode(.middle)
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

private struct ToolActionLine: View, Equatable {
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
                ToolStatusView(status: node.string("status"))
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
        Image(systemName: ToolCardNodeView.icon(node.string("kind")))
            .foregroundStyle(theme.muted)
            .frame(width: theme.indent + theme.spacing.s)
            .accessibilityHidden(true)
    }

    private func heading(_ text: String, changes: (added: Int, removed: Int)?) -> some View {
        HStack(spacing: theme.spacing.s) {
            Text(text)
                .foregroundStyle(node.string("status") == "cancelled" ? theme.muted : theme.foreground)
                .lineLimit(expanded ? 3 : 1)
                .truncationMode(.middle)
            if let changes {
                Text("+\(changes.added)").foregroundStyle(theme.success)
                Text("−\(changes.removed)").foregroundStyle(theme.danger)
            }
        }
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
                    AttachmentLabels(attachments: node.attachments())
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

private struct AttachmentLabels: View {
    let attachments: [SDUIAttachment]

    @Environment(\.theme) private var theme

    var body: some View {
        ForEach(attachments) { attachment in
            Text(attachment.label)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
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

    @Environment(\.theme) private var theme

    private var oneLine: Bool { ["read", "search"].contains(node.string("kind")) }

    var body: some View {
        let toolBody = oneLine ? nil : node.toolBody()
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: theme.spacing.l) {
                Image(systemName: Self.icon(node.string("kind")))
                    .foregroundStyle(theme.muted)
                    .frame(width: theme.indent + theme.spacing.s)
                Text(node.string("title") ?? "")
                    .foregroundStyle(node.string("status") == "cancelled" ? theme.muted : theme.foreground)
                    .lineLimit(oneLine ? 1 : 3)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
                Spacer(minLength: theme.gutter)
                if oneLine { ToolCardButtons(node: node) }
                ToolStatusView(status: node.string("status"))
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

    static func icon(_ kind: String?) -> String {
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

private struct ToolCardButtons: View {
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

private struct TruncatedNote: View {
    @Environment(\.theme) private var theme

    var body: some View {
        Text("Output truncated")
            .font(theme.uiSmall)
            .foregroundStyle(theme.muted)
            .padding(.horizontal, theme.gutter + theme.spacing.s)
            .padding(.vertical, theme.spacing.s)
    }
}

private struct ToolStatusView: View {
    let status: String?

    @Environment(\.theme) private var theme

    var body: some View {
        switch status {
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
