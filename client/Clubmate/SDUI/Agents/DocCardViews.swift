import SwiftUI

import ClubmateKit

struct DocCards: View {
    let cards: [DocCard]
    let open: (String) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        DocCardsLayout(spacing: theme.spacing.m, minWidth: theme.uiSize * 20, maxWidth: theme.uiSize * 27) {
            ForEach(cards) { card in
                DocCardButton(card: card) { open(card.path) }
            }
        }
        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
    }
}

struct DocCardsLayout: Layout {
    let spacing: CGFloat
    let minWidth: CGFloat
    let maxWidth: CGFloat

    private func rows(_ subviews: Subviews, width: CGFloat?) -> (grid: DocCardGrid, heights: [CGFloat]) {
        let grid = DocCardGrid(count: subviews.count, proposed: width, spacing: spacing, minWidth: minWidth, maxWidth: maxWidth)
        let heights = stride(from: 0, to: subviews.count, by: grid.columns).map { start in
            subviews[start..<min(start + grid.columns, subviews.count)].map { $0.sizeThatFits(ProposedViewSize(width: grid.cardWidth, height: nil)).height }.max() ?? 0
        }
        return (grid, heights)
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let layout = rows(subviews, width: proposal.width)
        let height = layout.heights.reduce(0, +) + spacing * CGFloat(max(0, layout.heights.count - 1))
        return CGSize(width: layout.grid.width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let layout = rows(subviews, width: bounds.width)
        var y = bounds.minY
        for (row, height) in layout.heights.enumerated() {
            for column in 0..<layout.grid.columns {
                let index = row * layout.grid.columns + column
                guard index < subviews.count else { break }
                let x = bounds.minX + CGFloat(column) * (layout.grid.cardWidth + spacing)
                subviews[index].place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(width: layout.grid.cardWidth, height: height))
            }
            y += height + spacing
        }
    }
}

private struct DocCardButton: View {
    let card: DocCard
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: theme.spacing.l) {
                Image(systemName: card.kind.symbol)
                    .font(theme.ui)
                    .foregroundStyle(theme.accentInk)
                    .frame(width: theme.control, height: theme.control)
                    .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.toneBg(theme.accent)))
                VStack(alignment: .leading, spacing: theme.spacing.xs) {
                    Text(card.title)
                        .font(theme.strong)
                        .foregroundStyle(theme.foreground)
                        .lineLimit(1)
                        .truncationMode(.label)
                    HStack(spacing: 0) {
                        Text("\(card.kind.label) · ")
                            .fixedSize()
                        Text(card.rel)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .layoutPriority(1)
                Image(systemName: "arrow.up.forward.square")
                    .font(theme.ui)
                    .foregroundStyle(hovering ? theme.accentInk : theme.muted)
            }
            .padding(.horizontal, theme.gutter)
            .padding(.vertical, theme.spacing.m)
            .frame(maxHeight: .infinity)
            .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.surface2))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.l).fill(hovering ? theme.hover : .clear))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(hovering ? theme.borderStrong : theme.border, lineWidth: 1))
            .contentShape(RoundedRectangle(cornerRadius: theme.radius.l))
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help("\(card.title) — \(card.path)")
        .accessibilityLabel("Open \(card.title)")
    }
}
