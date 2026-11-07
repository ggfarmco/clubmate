import SwiftUI

import ClubmateKit

struct ReposCardView: View {
    let node: SDUINode

    @State private var width: CGFloat = 0
    @Environment(\.theme) private var theme
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.keysFocused) private var focused
    @Environment(\.inTranscript) private var inTranscript

    private static let ordinal: CGFloat = 20
    private static let tailRows = 10
    private static let failedTint = 0.05
    private static let threeColumns: CGFloat = 520

    private var collapsed: Bool { node.bool("collapsed") == true }
    private var showsRule: Bool { width == 0 || width >= Self.threeColumns }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            head
            if !collapsed {
                columnTitles
                ForEach(node.reposRows()) { row in
                    theme.border.frame(height: 1)
                    repoRow(row)
                }
                footer
            }
        }
        .font(theme.ui)
        .frame(maxWidth: .infinity, alignment: .leading)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .agentCard()
        .padding(.horizontal, inTranscript ? 0 : theme.gutter)
    }

    private var head: some View {
        Button {
            dispatch(node, .click)
        } label: {
            ViewThatFits(in: .horizontal) {
                headRow(branch: !collapsed)
                headRow(branch: false)
            }
            .padding(.horizontal, theme.gutter)
            .frame(minHeight: theme.rowHeight2)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .focusable(false)
    }

    private func headRow(branch: Bool) -> some View {
        HStack(spacing: theme.spacing.m) {
            Image(systemName: SymbolName.resolve("folder"))
                .foregroundStyle(theme.text2)
            Text(node.string("title") ?? "")
                .font(theme.strong)
                .foregroundStyle(theme.foreground)
                .fixedSize()
            summary
                .font(theme.uiSmall)
                .lineLimit(1)
                .truncationMode(.label)
            Spacer(minLength: theme.spacing.m)
            if branch, let name = node.string("branch") {
                Text(name)
                    .font(theme.monoSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .fixedSize()
                if let note = node.string("branchNote") {
                    Text(note)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.faint)
                        .fixedSize()
                }
            }
            Image(systemName: SymbolName.resolve(collapsed ? "chevron.right" : "chevron.down"))
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
        }
    }

    private var summary: Text {
        let segments = node.reposSummary()
        return segments.enumerated().reduce(Text("")) { text, item in
            let word = Text(item.element.text).foregroundStyle(theme.tone(item.element.tone) ?? theme.muted)
            return item.offset == 0 ? word : text + Text(" · ").foregroundStyle(theme.muted) + word
        }
    }

    private var columnWeights: [CGFloat] { showsRule ? [1.1, 1, 1.6] : [1, 1.2] }

    private var columnTitles: some View {
        let titles = (node.props["columns"]?.arrayValue ?? []).compactMap(\.stringValue)
        return ReposRowLayout(lead: Self.ordinal, gap: theme.spacing.l, weights: columnWeights) {
            Color.clear.frame(height: 1)
            ForEach(Array(titles.enumerated()), id: \.offset) { index, title in
                if showsRule || index != 1 {
                    Text(title)
                        .lineLimit(1)
                }
            }
        }
        .font(theme.uiSmall)
        .foregroundStyle(theme.faint)
        .padding(.horizontal, theme.gutter)
        .padding(.bottom, theme.spacing.xs)
    }

    private func repoRow(_ row: SDUIReposRow) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            line(row)
            if row.open {
                detail(row)
            }
        }
    }

    private func line(_ row: SDUIReposRow) -> some View {
        Button {
            if row.opens { dispatch(node, .named(node.string("rowAction") ?? "", .string(row.id))) }
        } label: {
            ReposRowLayout(lead: Self.ordinal, gap: theme.spacing.l, weights: columnWeights) {
                Text(String(row.order))
                    .font(theme.count)
                    .foregroundStyle(theme.text2)
                    .frame(width: Self.ordinal - 2, height: Self.ordinal - 2)
                    .overlay(Circle().stroke(theme.borderStrong, lineWidth: 1))
                repoName(row)
                if showsRule {
                    rule(row)
                }
                state(row)
            }
            .padding(.horizontal, theme.gutter)
            .frame(minHeight: theme.header)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .focusable(false)
        .background(rowFill(row))
        .overlay(alignment: .leading) {
            if let bar = rowBar(row) {
                bar.frame(width: 2)
            }
        }
    }

    private func rowFill(_ row: SDUIReposRow) -> Color {
        if row.tone == "danger" { return theme.danger.opacity(Self.failedTint) }
        return row.cursor && focused ? theme.selected : .clear
    }

    private func rowBar(_ row: SDUIReposRow) -> Color? {
        if row.tone == "danger" { return theme.danger }
        return row.cursor && focused ? theme.accent : nil
    }

    private func repoName(_ row: SDUIReposRow) -> some View {
        HStack(spacing: theme.spacing.s) {
            Image(systemName: SymbolName.resolve("folder"))
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
            Text(row.name)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .truncationMode(.label)
        }
        .help(row.name)
    }

    private func rule(_ row: SDUIReposRow) -> some View {
        let full = [row.landsBy, row.source].compactMap { $0 }.joined(separator: " · ")
        return HStack(spacing: theme.spacing.s) {
            if let icon = row.landsIcon {
                Image(systemName: SymbolName.resolve(icon))
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
            }
            ViewThatFits(in: .horizontal) {
                ruleText(row)
                Text(row.landsBy)
                    .foregroundStyle(theme.muted)
                    .truncationMode(.label)
            }
            .lineLimit(1)
        }
        .help(full)
    }

    private func ruleText(_ row: SDUIReposRow) -> Text {
        let rule = Text(row.landsBy).foregroundStyle(theme.muted)
        guard let source = row.source else { return rule }
        return rule + Text(" · " + source).font(theme.uiSmall).foregroundStyle(theme.faint)
    }

    private func state(_ row: SDUIReposRow) -> some View {
        let ink = theme.tone(row.tone) ?? theme.text2
        return HStack(spacing: theme.spacing.s) {
            if let icon = row.icon {
                Image(systemName: SymbolName.resolve(icon))
                    .font(theme.uiSmall)
                    .foregroundStyle(ink)
            }
            Text(row.state)
                .foregroundStyle(ink)
                .lineLimit(1)
                .truncationMode(.label)
        }
        .help(row.state)
    }

    private var detailInset: CGFloat { theme.gutter + Self.ordinal + theme.spacing.l }

    private func detail(_ row: SDUIReposRow) -> some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            if let tail = row.tail {
                CappedScroll(rows: Self.tailRows) {
                    Text(tail)
                        .font(theme.monoSmall)
                        .foregroundStyle(theme.text2)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, theme.spacing.xl)
                        .padding(.vertical, theme.spacing.m)
                }
                .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.background))
            }
            if let actions = row.actions, let bar = node.identifiedChildren.first(where: { $0.id == actions }) {
                ViewThatFits(in: .horizontal) {
                    SDUINodeView(node: bar.node)
                        .equatable()
                        .environment(\.sduiGutter, false)
                    FlowLayout(spacing: theme.spacing.m) {
                        ForEach(bar.node.identifiedChildren.filter { $0.node.type != "spacer" }) { child in
                            SDUINodeView(node: child.node)
                                .environment(\.sduiGutter, false)
                        }
                    }
                }
            }
            if let note = row.note {
                Text(note)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(.leading, detailInset)
        .padding(.trailing, theme.gutter)
        .padding(.vertical, theme.spacing.m)
    }

    private var footer: some View {
        VStack(spacing: 0) {
            theme.border.frame(height: 1)
            ViewThatFits(in: .horizontal) {
                footerRow(hint: true)
                footerRow(hint: false)
            }
            .font(theme.uiSmall)
            .padding(.horizontal, theme.gutter)
            .frame(minHeight: theme.header)
        }
    }

    private func footerRow(hint: Bool) -> some View {
        HStack(spacing: theme.spacing.m) {
            if let text = node.string("footer") {
                Text(text)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(text)
            }
            Spacer(minLength: 0)
            if hint, let keys = node.string("hint") {
                Text(keys)
                    .foregroundStyle(theme.faint)
                    .fixedSize()
            }
        }
    }
}

struct ReposRowLayout: Layout {
    let lead: CGFloat
    let gap: CGFloat
    let weights: [CGFloat]

    private func widths(_ total: CGFloat, count: Int) -> [CGFloat] {
        let free = max(total - lead - gap * CGFloat(max(count - 1, 0)), 0)
        let sum = weights.reduce(0, +)
        return [lead] + weights.map { sum > 0 ? free * $0 / sum : 0 }
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let ideal = subviews.reduce(0) { $0 + $1.sizeThatFits(.unspecified).width + gap }
        let total = proposal.width.flatMap { $0.isFinite ? $0 : nil } ?? ideal
        let columns = widths(total, count: subviews.count)
        let height = zip(subviews, columns).map { $0.sizeThatFits(ProposedViewSize(width: $1, height: nil)).height }.max() ?? 0
        return CGSize(width: total, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX
        for (subview, width) in zip(subviews, widths(bounds.width, count: subviews.count)) {
            subview.place(at: CGPoint(x: x, y: bounds.midY), anchor: .leading, proposal: ProposedViewSize(width: width, height: nil))
            x += width + gap
        }
    }
}

struct HeaderPillView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme
    @Environment(\.sduiDispatch) private var dispatch

    var body: some View {
        Button {
            dispatch(node, .click)
        } label: {
            ViewThatFits(in: .horizontal) {
                pill(full: true)
                pill(full: false)
            }
        }
        .buttonStyle(.plain)
        .focusable(false)
        .help(node.string("label") ?? "")
    }

    private func pill(full: Bool) -> some View {
        let detail = node.string("detail")
        return HStack(spacing: theme.spacing.s) {
            if full || detail == nil {
                if let icon = node.string("icon") {
                    Image(systemName: SymbolName.resolve(icon))
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
                Text(node.string("label") ?? "")
                    .foregroundStyle(theme.text2)
            }
            if full, detail != nil {
                Text("·").foregroundStyle(theme.muted)
            }
            if let detail {
                Text(detail)
                    .foregroundStyle(theme.tone(node.string("detailTone")) ?? theme.text2)
            }
            Image(systemName: SymbolName.resolve("chevron.down"))
                .foregroundStyle(theme.muted)
        }
        .font(theme.uiSmall)
        .lineLimit(1)
        .fixedSize()
        .padding(.horizontal, theme.spacing.m)
        .frame(height: theme.rowHeight)
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.surface1))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(theme.borderStrong, lineWidth: 1))
    }
}
