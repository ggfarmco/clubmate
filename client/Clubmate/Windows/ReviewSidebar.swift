import SwiftUI

import ClubmateKit

struct SectionSidebar: View {
    let session: ProjectSession
    let store: ProjectUIStore
    let section: String

    @Environment(\.theme) private var theme
    @Environment(\.focusRouter) private var router

    var body: some View {
        let review = store.slots[ReviewSlot.slot]?.tree
        let title = store.sections.first { $0.id == section }?.title ?? section
        let tabs = SidebarTabs.tabs(section: section, title: title, hosted: store.hostedSections, review: ReviewSlot.isActive(review))
        let current = store.sidebarTabs.current(section, in: tabs)
        VStack(spacing: 0) {
            if tabs.count > 1 {
                HStack(spacing: 0) {
                    ForEach(tabs) { tab in
                        TabButton(label: tab.title, selected: tab == current) { store.sidebarTabs.choose(tab.slot, in: section) }
                    }
                    Spacer(minLength: 0)
                }
                .frame(height: theme.rowHeight + 4)
                .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
            }
            if current?.slot == ReviewSlot.slot, let header = ReviewSlot.header(review) {
                ReviewHeaderView(session: session, header: header)
            }
            SlotPane(store: store, slot: current?.slot ?? "\(section).sidebar")
                .id(current?.slot)
                .environment(\.cycleSidebarTab, tabs.count > 1 ? cycler(tabs) : nil)
        }
    }

    private func cycler(_ tabs: [SidebarTab]) -> SidebarTabCycler {
        SidebarTabCycler { [store, section, router] delta in
            guard store.sidebarTabs.cycle(section, in: tabs, by: delta) != nil else { return }
            router?.request("\(section).sidebar")
        }
    }
}

private struct ReviewHeaderView: View {
    let session: ProjectSession
    let header: ReviewHeader

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            VStack(spacing: theme.spacing.s) {
                CompareField(caption: "base", title: header.title) {
                    Text(header.base)
                        .lineLimit(1)
                        .truncationMode(.middle)
                } action: {
                    session.chooseReviewBase(head: header.headRef)
                }
                CompareField(caption: "head", title: header.title) {
                    ViewThatFits(in: .horizontal) {
                        headLabel(suffix: header.headSuffix)
                        headLabel(suffix: header.headSuffixShort)
                    }
                } action: {
                    session.chooseReviewHead(base: header.baseRef)
                }
            }
            HStack(spacing: theme.spacing.l) {
                HeaderButton(label: "⇄ swap", help: "Swap base and head") { session.run(ReviewCommand.swap) }
                Spacer(minLength: 0)
                HeaderButton(label: "✕ end", help: "End Review") { session.run(ReviewCommand.end) }
            }
            if !header.warning.isEmpty {
                note(header.warning, tone: theme.warning)
            }
            if !header.error.isEmpty {
                note(header.error, tone: theme.danger)
            }
            FlowLayout(spacing: theme.spacing.s) {
                ForEach(header.chips) { chip in
                    ChipButton(label: chip.label, hidden: chip.on, help: help(chip)) {
                        session.run(ReviewCommand.filter(chip))
                    }
                }
                if !header.chips.contains(where: { $0.id == ReviewChip.glob }) {
                    ChipButton(label: "+ Glob…", hidden: false, help: "Hide files matching a glob") { session.askReviewGlob() }
                }
            }
            if !header.progress.isEmpty {
                note(header.progress, tone: theme.muted)
            }
            if let fraction = header.fraction {
                Capsule()
                    .fill(theme.border)
                    .frame(height: 2)
                    .overlay(alignment: .leading) {
                        GeometryReader { proxy in
                            Capsule().fill(theme.success).frame(width: proxy.size.width * fraction)
                        }
                    }
            }
        }
        .padding(theme.gutter)
        .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
    }

    private func headLabel(suffix: String) -> some View {
        HStack(spacing: theme.spacing.s) {
            Text(header.head)
                .lineLimit(1)
                .truncationMode(.middle)
            Text(suffix)
                .foregroundStyle(theme.muted)
                .fixedSize()
        }
    }

    private func help(_ chip: ReviewChip) -> String {
        if chip.id == ReviewChip.glob { return "Clear glob" }
        return chip.on ? "Show \(chip.label)" : "Hide \(chip.label)"
    }

    private func note(_ text: String, tone: Color) -> some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(tone)
            .fixedSize(horizontal: false, vertical: true)
    }
}

private struct FlowLayout: Layout {
    let spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        let size = FlowRows.size(sizes: sizes(subviews), width: width, spacing: spacing)
        return CGSize(width: proposal.width ?? size.width, height: size.height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let origins = FlowRows.origins(sizes: sizes(subviews), width: bounds.width, spacing: spacing)
        for (subview, origin) in zip(subviews, origins) {
            subview.place(at: CGPoint(x: bounds.minX + origin.x, y: bounds.minY + origin.y), proposal: .unspecified)
        }
    }

    private func sizes(_ subviews: Subviews) -> [CGSize] {
        subviews.map { $0.sizeThatFits(.unspecified) }
    }
}

private struct CompareField<Label: View>: View {
    let caption: String
    let title: String
    @ViewBuilder let label: Label
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: theme.spacing.m) {
                Text(caption)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .frame(width: 30, alignment: .leading)
                label
                    .font(theme.ui)
                    .foregroundStyle(theme.foreground)
                    .help(title)
                Spacer(minLength: 0)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 8, weight: .semibold))
                    .foregroundStyle(theme.muted)
            }
            .padding(theme.spacing.field)
            .frame(maxWidth: .infinity)
            .frame(height: theme.rowHeight + 2)
            .background(RoundedRectangle(cornerRadius: 4).fill(theme.foreground.opacity(hovering ? 0.08 : 0.04)))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help("Choose \(caption)")
    }
}

private struct HeaderButton: View {
    let label: String
    let help: String
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(theme.uiSmall)
                .foregroundStyle(hovering ? theme.foreground : theme.muted)
                .lineLimit(1)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(help)
        .fixedSize()
    }
}

private struct ChipButton: View {
    let label: String
    let hidden: Bool
    let help: String
    let action: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: theme.spacing.xs) {
                if hidden {
                    Image(systemName: "eye.slash")
                        .font(.system(size: 8, weight: .semibold))
                }
                Text(label)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .frame(maxWidth: 120)
            }
            .font(theme.uiSmall)
            .foregroundStyle(hidden ? theme.accent : theme.muted)
            .padding(theme.spacing.chip)
            .background(Capsule().fill(hidden ? theme.accent.opacity(0.18) : theme.foreground.opacity(0.05)))
            .overlay(Capsule().stroke(theme.border, lineWidth: 1))
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .help(help)
        .fixedSize()
    }
}
