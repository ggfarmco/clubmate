import SwiftUI

import ClubmateKit

struct CodeSidebar: View {
    let session: ProjectSession
    let store: ProjectUIStore

    private enum Tab {
        case files
        case review
    }

    @State private var tab = Tab.files
    @Environment(\.theme) private var theme

    var body: some View {
        let review = store.slots[ReviewSlot.slot]?.tree
        let active = ReviewSlot.isActive(review)
        VStack(spacing: 0) {
            if active {
                HStack(spacing: 0) {
                    TabButton(label: "Files", selected: tab == .files) { tab = .files }
                    TabButton(label: "Review", selected: tab == .review) { tab = .review }
                    Spacer(minLength: 0)
                }
                .frame(height: theme.rowHeight + 4)
                .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
            }
            if active, tab == .review {
                if let header = ReviewSlot.header(review) {
                    ReviewHeaderView(session: session, header: header)
                }
                SlotPane(store: store, slot: ReviewSlot.slot)
            } else {
                SlotPane(store: store, slot: "\(CodeSection.id).sidebar")
            }
        }
        .onChange(of: active, initial: true) { _, active in
            tab = active ? .review : .files
        }
    }
}

private struct ReviewHeaderView: View {
    let session: ProjectSession
    let header: ReviewHeader

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .center, spacing: 6) {
                VStack(spacing: 3) {
                    CompareField(caption: "base", value: header.base) {
                        session.chooseReviewBase(head: header.headPath)
                    }
                    CompareField(caption: "head", value: header.headLabel) {
                        session.chooseReviewHead(base: header.baseRef)
                    }
                }
                VStack(spacing: 3) {
                    HeaderButton(label: "✕", help: "End Review") { session.run(ReviewCommand.end) }
                    HeaderButton(label: "⇄ swap", help: "Swap base and head") { session.run(ReviewCommand.swap) }
                }
            }
            .help(header.title)
            if !header.warning.isEmpty {
                note(header.warning, tone: theme.warning)
            }
            if !header.error.isEmpty {
                note(header.error, tone: theme.danger)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 4) {
                    ForEach(header.chips) { chip in
                        ChipButton(label: chip.label, hidden: chip.on, help: help(chip)) {
                            session.run(ReviewCommand.filter(chip))
                        }
                    }
                    if !header.chips.contains(where: { $0.id == ReviewChip.glob }) {
                        ChipButton(label: "+ Glob…", hidden: false, help: "Hide files matching a glob") { session.askReviewGlob() }
                    }
                }
            }
            if !header.progress.isEmpty {
                Text(header.progress)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
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

private struct CompareField: View {
    let caption: String
    let value: String
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Text(caption)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .frame(width: 30, alignment: .leading)
                Text(value)
                    .font(theme.ui)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 0)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 8, weight: .semibold))
                    .foregroundStyle(theme.muted)
            }
            .padding(.horizontal, 6)
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
                .frame(height: theme.rowHeight + 2)
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
            HStack(spacing: 3) {
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
            .padding(.horizontal, 7)
            .padding(.vertical, 2)
            .background(Capsule().fill(hidden ? theme.accent.opacity(0.18) : theme.foreground.opacity(0.05)))
            .overlay(Capsule().stroke(theme.border, lineWidth: 1))
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .help(help)
        .fixedSize()
    }
}
