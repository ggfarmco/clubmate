import SwiftUI

import ClubmateKit

struct CardNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme
    @Environment(\.sheetInset) private var sheetInset

    var body: some View {
        let tone = theme.tone(node.string("tone"))
        HStack(alignment: .firstTextBaseline, spacing: theme.spacing.l) {
            if let icon = node.string("icon") {
                Image(systemName: icon)
                    .foregroundStyle(tone ?? theme.text2)
            }
            VStack(alignment: .leading, spacing: theme.spacing.l) {
                SDUIChildren(node: node)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(tone.map(theme.toneBg) ?? theme.surface2))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(tone.map(theme.toneBorder) ?? theme.border, lineWidth: 1))
        .padding(.horizontal, sheetInset ? theme.gutter : theme.gutter * 2)
    }
}
