import SwiftUI

import ClubmateKit

struct FlowLayout: Layout {
    let spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        let size = FlowRows.size(sizes: sizes(subviews, width: width), width: width, spacing: spacing)
        return CGSize(width: proposal.width ?? size.width, height: size.height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let sizes = sizes(subviews, width: bounds.width)
        let origins = FlowRows.origins(sizes: sizes, width: bounds.width, spacing: spacing)
        for (subview, (origin, size)) in zip(subviews, zip(origins, sizes)) {
            subview.place(at: CGPoint(x: bounds.minX + origin.x, y: bounds.minY + origin.y), proposal: ProposedViewSize(size))
        }
    }

    private func sizes(_ subviews: Subviews, width: CGFloat) -> [CGSize] {
        subviews.map { subview in
            let ideal = subview.sizeThatFits(.unspecified)
            return ideal.width > width ? subview.sizeThatFits(ProposedViewSize(width: width, height: nil)) : ideal
        }
    }
}
