import SwiftUI
import UIKit

enum PadLayout {
    static let measure: CGFloat = 780
    static let margin: CGFloat = 28
    static let aside: CGFloat = 240
    static let asideGap: CGFloat = 40
    static let wide: CGFloat = measure + asideGap + aside
    static let asideFrom: CGFloat = wide + 2 * margin
    static let headerIdeal: CGFloat = 2000

    static func on(_ sizeClass: UserInterfaceSizeClass?) -> Bool {
        UIDevice.current.userInterfaceIdiom == .pad && sizeClass == .regular
    }

    static func prose(_ detail: CGFloat) -> CGFloat {
        max(0, min(measure, detail - 2 * margin))
    }

    static func asideShown(_ detail: CGFloat, aside: Bool) -> Bool {
        aside && detail >= asideFrom
    }

    static func proseLeft(_ detail: CGFloat, aside: Bool) -> CGFloat {
        asideShown(detail, aside: aside) ? (detail - wide) / 2 : max(0, (detail - prose(detail)) / 2)
    }

    static func hasAside(_ html: String?) -> Bool {
        html?.contains("<aside class=\"wiki-margin\"") == true
    }
}

struct PadProse: Layout {
    let aside: Bool

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? PadLayout.measure + 2 * PadLayout.margin
        let height = subviews.map { $0.sizeThatFits(ProposedViewSize(width: PadLayout.prose(width), height: nil)).height }.max() ?? 0
        return CGSize(width: width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let origin = CGPoint(x: bounds.minX + PadLayout.proseLeft(bounds.width, aside: aside), y: bounds.minY)
        for subview in subviews {
            subview.place(at: origin, proposal: ProposedViewSize(width: PadLayout.prose(bounds.width), height: bounds.height))
        }
    }
}

struct PadHeader<Subtitle: View>: View {
    let title: String
    @ViewBuilder let subtitle: Subtitle

    var body: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(title)
                .font(.title3.weight(.semibold))
                .lineLimit(1)
            subtitle
                .font(.subheadline)
                .lineLimit(1)
        }
        .frame(minWidth: 0, idealWidth: PadLayout.headerIdeal, maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("page.header")
    }
}
