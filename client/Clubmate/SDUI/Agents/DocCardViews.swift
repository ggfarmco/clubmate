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

    private func columns(_ count: Int, width: CGFloat) -> (count: Int, width: CGFloat) {
        let fit = max(1, Int((width + spacing) / (minWidth + spacing)))
        guard fit > 1 else { return (1, width) }
        let count = max(1, min(count, fit))
        return (count, min(maxWidth, (width - spacing * CGFloat(count - 1)) / CGFloat(count)))
    }

    private func rows(_ subviews: Subviews, width: CGFloat) -> (columns: Int, cardWidth: CGFloat, heights: [CGFloat]) {
        let (count, cardWidth) = columns(subviews.count, width: width)
        let heights = stride(from: 0, to: subviews.count, by: count).map { start in
            subviews[start..<min(start + count, subviews.count)].map { $0.sizeThatFits(ProposedViewSize(width: cardWidth, height: nil)).height }.max() ?? 0
        }
        return (count, cardWidth, heights)
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? maxWidth * CGFloat(subviews.count)
        let layout = rows(subviews, width: width)
        let height = layout.heights.reduce(0, +) + spacing * CGFloat(max(0, layout.heights.count - 1))
        return CGSize(width: width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let layout = rows(subviews, width: bounds.width)
        var y = bounds.minY
        for (row, height) in layout.heights.enumerated() {
            for column in 0..<layout.columns {
                let index = row * layout.columns + column
                guard index < subviews.count else { break }
                let x = bounds.minX + CGFloat(column) * (layout.cardWidth + spacing)
                subviews[index].place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(width: layout.cardWidth, height: height))
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
