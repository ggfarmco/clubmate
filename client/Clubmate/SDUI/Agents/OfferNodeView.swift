import SwiftUI

import ClubmateKit

struct OfferNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        let text = node.string("text") ?? ""
        HStack(spacing: theme.spacing.m) {
            if let icon = node.string("icon") {
                Image(systemName: icon)
                    .font(theme.glyph)
                    .foregroundStyle(theme.muted)
            }
            Text(text)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .truncationMode(.label)
                .help(text)
                .frame(maxWidth: .infinity, alignment: .leading)
                .layoutPriority(1)
            ForEach(node.identifiedChildren) { child in
                SDUINodeView(node: child.node)
                    .equatable()
                    .fixedSize()
            }
        }
        .font(theme.ui)
        .frame(minHeight: theme.control + theme.spacing.m)
        .padding(.leading, theme.gutter)
        .padding(.trailing, theme.spacing.s)
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).strokeBorder(theme.borderStrong, style: StrokeStyle(lineWidth: 1, dash: [4, 3])))
        .padding(.bottom, theme.spacing.l)
    }
}
