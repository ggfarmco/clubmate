import SwiftUI

import ClubmateKit

struct ImpactNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    private static let iconWidth: CGFloat = 18
    private static let keyWidth: CGFloat = 150

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(node.impactRows()) { row in
                theme.border.frame(height: 1)
                rowView(row)
            }
        }
        .font(theme.ui)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func rowView(_ row: SDUIImpactRow) -> some View {
        HStack(alignment: .top, spacing: theme.spacing.xl) {
            Image(systemName: SymbolName.resolve(row.icon))
                .foregroundStyle(theme.tone(row.tone) ?? theme.muted)
                .frame(width: Self.iconWidth, alignment: .center)
            key(row)
                .frame(width: Self.keyWidth, alignment: .leading)
            if let body = node.children.first(where: { $0.id == row.id }) {
                SDUINodeView(node: body)
                    .environment(\.sduiGutter, false)
                    .foregroundStyle(theme.text2)
                    .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
                    .clipped()
            }
        }
        .padding(.vertical, theme.spacing.l)
    }

    private func key(_ row: SDUIImpactRow) -> Text {
        let title = Text(row.title).font(theme.strong).foregroundColor(theme.foreground)
        guard let count = row.count else { return title }
        return Text("\(title) \(Text("· \(count)").foregroundColor(theme.muted))")
    }
}
