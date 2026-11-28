import SwiftUI

import ClubmateKit

struct SessionHeaderRow: View {
    let node: SDUINode

    @State private var detailsShown = false
    @Environment(\.theme) private var theme

    private struct Fold: Hashable {
        var caption = true
        var titleWidth: CGFloat?
        var allBadges = true
        var inline: Int
        var percent = true
        var compactStop = false
    }

    static let titleMin: CGFloat = 160
    static let titleMinNarrow: CGFloat = 120

    var body: some View {
        ViewThatFits(in: .horizontal) {
            ForEach(folds, id: \.self) { row($0) }
        }
        .frame(height: theme.control)
    }

    private var actions: [SDUIChild] { node.headerBar }

    private var forcedMore: [SDUIChild] {
        node.headerButtons.filter { $0.node.bool("overflow") == true }
    }

    private var folds: [Fold] {
        let n = actions.count
        var out = [Fold(inline: n), Fold(caption: false, inline: n), Fold(caption: false, titleWidth: Self.titleMin, inline: n)]
        for k in stride(from: n, through: 0, by: -1) {
            out.append(Fold(caption: false, titleWidth: Self.titleMin, allBadges: false, inline: k))
        }
        out.append(Fold(caption: false, titleWidth: Self.titleMinNarrow, allBadges: false, inline: 0))
        out.append(Fold(caption: false, titleWidth: Self.titleMinNarrow, allBadges: false, inline: 0, percent: false))
        out.append(Fold(caption: false, titleWidth: Self.titleMinNarrow, allBadges: false, inline: 0, percent: false, compactStop: true))
        return out
    }

    private func row(_ fold: Fold) -> some View {
        HStack(spacing: theme.spacing.m) {
            if let icon = node.string("icon") {
                Image(systemName: SymbolName.resolve(icon))
                    .foregroundStyle(theme.tone(node.headerStatusTone) ?? theme.muted)
            }
            title(fold.titleWidth)
            if fold.caption, let caption {
                Text(caption)
                    .font(theme.uiSmall)
                    .foregroundStyle(segments.isEmpty ? theme.muted : theme.faint)
                    .lineLimit(1)
                    .fixedSize()
            }
            if let pill = node.headerPill {
                HeaderPillView(node: pill.node)
            }
            badges(all: fold.allBadges)
            Spacer(minLength: theme.spacing.m)
            if let strip = node.headerStrip {
                SDUINodeView(node: strip.node)
                    .equatable()
                    .fixedSize()
            }
            ContextRing(node: node, caption: caption, percent: fold.percent, shown: $detailsShown)
            let shown = Array(actions.prefix(fold.inline))
            let more = Array(actions.dropFirst(fold.inline)) + forcedMore
            if !shown.isEmpty || !more.isEmpty {
                rule
            }
            ForEach(shown) { child in
                SDUINodeView(node: Self.compact(child.node))
                    .equatable()
                    .fixedSize()
            }
            if !more.isEmpty {
                HeaderMoreMenu(id: "\(node.id ?? "header").more", actions: more.map(\.node))
            }
            if let stop = node.headerStop {
                rule
                SDUINodeView(node: fold.compactStop ? Self.iconOnly(stop.node) : stop.node)
                    .equatable()
                    .fixedSize()
            }
            if let corner = node.headerCorner {
                SDUINodeView(node: corner.node)
                    .equatable()
                    .fixedSize()
            }
        }
    }

    @ViewBuilder
    private func title(_ width: CGFloat?) -> some View {
        let text = node.string("title") ?? ""
        let segments = self.segments
        let help = segments.isEmpty ? text : ([text] + segments).joined(separator: " · ")
        if let width {
            ViewThatFits(in: .horizontal) {
                ForEach(Array(stride(from: segments.count, to: 1, by: -1)), id: \.self) { shown in
                    titleText(text, segments: segments.prefix(shown)).fixedSize()
                }
                titleText(text, segments: segments.prefix(1))
                    .truncationMode(segments.isEmpty ? .label : .tail)
            }
            .help(help)
            .frame(minWidth: width, idealWidth: width, maxWidth: .infinity, alignment: .leading)
            .layoutPriority(1)
        } else {
            titleText(text, segments: segments[...])
                .fixedSize()
                .help(help)
        }
    }

    private var segments: [String] {
        node.props["segments"]?.arrayValue?.compactMap(\.stringValue) ?? []
    }

    private func titleText(_ text: String, segments: ArraySlice<String>) -> some View {
        (Text(text).font(theme.title).foregroundStyle(theme.foreground)
            + (segments.isEmpty ? Text("") : Text("  " + segments.joined(separator: " · ")).font(theme.uiSmall).foregroundStyle(theme.text2)))
            .lineLimit(1)
    }

    @ViewBuilder
    private func badges(all: Bool) -> some View {
        let list = node.headerBadges()
        let shown = all ? list : Array(list.prefix(1))
        ForEach(Array(shown.enumerated()), id: \.offset) { _, badge in
            HeaderBadge(badge: badge)
        }
        if shown.count < list.count {
            BadgeLabel(text: "+\(list.count - shown.count)", tone: nil)
                .fixedSize()
                .help(list.dropFirst(shown.count).map(\.text).joined(separator: "\n"))
        }
    }

    private var rule: some View {
        theme.border.frame(width: 1, height: theme.uiSize + 2)
    }

    private var caption: String? {
        let parts = ["agent", "branch", "mode"].compactMap { node.string($0) }.filter { !$0.isEmpty }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    static func compact(_ button: SDUINode) -> SDUINode {
        let variant = button.string("variant")
        guard button.string("icon") != nil, variant == nil || variant == "ghost" else { return button }
        return iconOnly(button)
    }

    static func iconOnly(_ button: SDUINode) -> SDUINode {
        var props = button.props
        props["iconOnly"] = .bool(true)
        return SDUINode(type: button.type, id: button.id, props: props, children: button.children)
    }
}

private struct ContextRing: View {
    let node: SDUINode
    let caption: String?
    let percent: Bool
    @Binding var shown: Bool

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        let details = node.headerDetails()
        let context = node.headerContext
        if context != nil || !details.isEmpty {
            Button { shown.toggle() } label: {
                HStack(spacing: theme.spacing.s) {
                    if let context {
                        ring(context)
                        if percent {
                            Text("\(context.fill)%")
                                .font(theme.uiSmall)
                                .foregroundStyle(theme.tone(context.tone) ?? theme.muted)
                                .monospacedDigit()
                        }
                    } else {
                        Image(systemName: "info.circle")
                            .foregroundStyle(theme.muted)
                    }
                }
                .padding(.horizontal, theme.spacing.s)
                .frame(height: theme.rowHeight)
                .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(hovering || shown ? theme.hover : .clear))
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .focusable(false)
            .onHover { hovering = $0 }
            .fixedSize()
            .help("Session details")
            .accessibilityLabel(node.string("meta") ?? "Session details")
            .testNode("header.details", role: "button", label: "Session details")
            .popover(isPresented: $shown, arrowEdge: .bottom) {
                SessionDetailsPopover(caption: caption, details: details, context: context, contextButton: contextButton)
                    .environment(\.theme, theme)
            }
        }
    }

    private var contextButton: SDUINode? {
        guard let button = node.headerButtons.first(where: { $0.node.string("action") == "context" })?.node else { return nil }
        var props = button.props
        if let key = props.removeValue(forKey: "shortcut") {
            props["hint"] = key
        }
        props["variant"] = .string("secondary")
        return SDUINode(type: button.type, id: button.id.map { $0 + ".popover" }, props: props, children: button.children)
    }

    private func ring(_ context: SDUIHeaderContext) -> some View {
        let size = theme.uiSize
        return ZStack {
            Circle().stroke(theme.border, lineWidth: 2)
            Circle()
                .trim(from: 0, to: min(1, CGFloat(context.fill) / 100))
                .stroke(theme.tone(context.tone) ?? theme.accent, style: StrokeStyle(lineWidth: 2, lineCap: .round))
                .rotationEffect(.degrees(-90))
        }
        .frame(width: size, height: size)
    }
}

private struct SessionDetailsPopover: View {
    let caption: String?
    let details: [SDUIHeaderDetail]
    let context: SDUIHeaderContext?
    let contextButton: SDUINode?

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            if let caption {
                Text(caption)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            Grid(alignment: .leading, horizontalSpacing: theme.spacing.xl, verticalSpacing: theme.spacing.m) {
                ForEach(Array(details.enumerated()), id: \.offset) { _, row in
                    GridRow {
                        Text(row.label)
                            .foregroundStyle(theme.muted)
                        HStack(spacing: theme.spacing.m) {
                            Text(row.value)
                                .foregroundStyle(theme.foreground)
                                .textSelection(.enabled)
                            if row.label == "Context", let context {
                                ProgressBar(fraction: Double(context.fill) / 100, tone: theme.tone(context.tone) ?? theme.accent)
                            }
                        }
                    }
                    .font(theme.ui)
                }
            }
            if let contextButton {
                theme.border.frame(height: 1)
                SDUINodeView(node: contextButton)
                    .equatable()
                    .fixedSize()
            }
        }
        .padding(theme.spacing.xl)
        .frame(minWidth: 240, alignment: .leading)
        .background(theme.surface3)
    }
}

private struct ProgressBar: View {
    let fraction: Double
    let tone: Color

    @Environment(\.theme) private var theme

    var body: some View {
        GeometryReader { proxy in
            ZStack(alignment: .leading) {
                Capsule().fill(theme.hover)
                Capsule().fill(tone).frame(width: proxy.size.width * min(1, max(0, fraction)))
            }
        }
        .frame(width: 80, height: 4)
    }
}

private struct HeaderBadge: View {
    let badge: SDUIHeaderBadge

    @Environment(\.theme) private var theme

    var body: some View {
        if let id = badge.id, let action = badge.action {
            HeaderBadgeButton(badge: badge, id: id, action: action)
        } else {
            BadgeLabel(text: badge.text, tone: theme.tone(badge.tone), outline: badge.outline)
                .fixedSize()
        }
    }
}
