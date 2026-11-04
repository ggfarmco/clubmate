import SwiftUI

import ClubmateKit

struct CardNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme
    @Environment(\.sheetInset) private var sheetInset
    @Environment(\.inTranscript) private var inTranscript

    var body: some View {
        if node.bool("rows") == true {
            rows
        } else {
            plain
        }
    }

    private var rows: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(node.identifiedChildren.enumerated()), id: \.element.id) { index, child in
                if index > 0 {
                    theme.border.frame(height: 1)
                }
                SDUINodeView(node: child.node)
                    .equatable()
                    .environment(\.sduiGutter, false)
                    .environment(\.cardRowInset, theme.gutter + theme.spacing.s)
                    .padding(child.node.type == "choice" ? 0 : theme.gutter)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .font(theme.ui)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.surface2))
        .clipShape(RoundedRectangle(cornerRadius: theme.radius.l))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.border, lineWidth: 1))
        .padding(.horizontal, inTranscript ? 0 : theme.gutter)
    }

    private var plain: some View {
        let tone = theme.tone(node.string("tone"))
        return HStack(alignment: .firstTextBaseline, spacing: theme.spacing.l) {
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

extension EnvironmentValues {
    @Entry var cardRowInset: CGFloat = 0
    @Entry var inTranscript = false
}
