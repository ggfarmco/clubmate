import SwiftUI

import ClubmateKit

struct RowLayout: Layout {
    let hasDetail: Bool
    let lead: CGFloat
    let spacing: CGFloat
    let keptLabel: CGFloat
    let keptDetail: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let sizes = subviews.map { $0.sizeThatFits(.unspecified) }
        let height = sizes.map(\.height).max() ?? 0
        if let width = proposal.width, width.isFinite { return CGSize(width: max(width, 0), height: height) }
        let parts = parts(sizes.map(\.width))
        let detail = parts.detail.map { $0 + gap(parts) } ?? 0
        return CGSize(width: parts.label + trailing(parts) + detail, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard let first = subviews.first else { return }
        let parts = parts(subviews.map { $0.sizeThatFits(.unspecified).width })
        let available = bounds.width - trailing(parts)
        let fit = RowFit.widths(available: available, label: parts.label, detail: parts.detail, gap: gap(parts), keptLabel: keptLabel, keptDetail: keptDetail)
        first.place(at: CGPoint(x: bounds.minX, y: bounds.midY), anchor: .leading, proposal: ProposedViewSize(width: fit.label, height: bounds.height))
        var x = bounds.maxX
        if hasDetail, let detail = subviews.last {
            detail.place(at: CGPoint(x: x, y: bounds.midY), anchor: .trailing, proposal: ProposedViewSize(width: fit.detail ?? 0, height: bounds.height))
            if let shown = fit.detail { x -= shown + spacing }
        }
        for badge in subviews.dropFirst().prefix(parts.badges.count).reversed() {
            let size = badge.sizeThatFits(.unspecified)
            badge.place(at: CGPoint(x: x, y: bounds.midY), anchor: .trailing, proposal: ProposedViewSize(size))
            x -= size.width + spacing
        }
    }

    private struct Parts {
        let label: CGFloat
        let badges: [CGFloat]
        let detail: CGFloat?
    }

    private func parts(_ widths: [CGFloat]) -> Parts {
        guard let label = widths.first else { return Parts(label: 0, badges: [], detail: nil) }
        let rest = Array(widths.dropFirst())
        return Parts(label: label, badges: hasDetail ? rest.dropLast() : rest, detail: hasDetail ? rest.last : nil)
    }

    private func trailing(_ parts: Parts) -> CGFloat {
        parts.badges.isEmpty ? 0 : lead + parts.badges.reduce(0, +) + spacing * CGFloat(parts.badges.count - 1)
    }

    private func gap(_ parts: Parts) -> CGFloat {
        parts.badges.isEmpty ? lead : spacing
    }
}
