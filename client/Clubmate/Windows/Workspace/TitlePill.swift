import SwiftUI

struct TitlePill<Content: View>: View {
    var pressed = false
    @ViewBuilder let content: Content

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.m) {
            content
        }
        .font(theme.ui)
        .foregroundStyle(theme.text2)
        .padding(.horizontal, theme.spacing.l)
        .frame(height: theme.control)
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(pressed ? theme.pressed : hovering ? theme.hover : .clear))
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.surface2))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(theme.border, lineWidth: 1))
        .contentShape(Rectangle())
        .onHover { hovering = $0 }
    }
}

struct CountBadge: View {
    let count: Int
    let needsYou: Bool

    @Environment(\.theme) private var theme

    var body: some View {
        if count > 0 {
            Text("\(count)")
                .font(theme.count.monospacedDigit())
                .foregroundStyle(needsYou ? theme.onWarning : theme.text2)
                .padding(.horizontal, theme.spacing.s)
                .frame(minWidth: 16, minHeight: 16)
                .background(Capsule().fill(needsYou ? theme.warningFill : theme.hover))
                .accessibilityLabel(needsYou ? "\(count), needs you" : "\(count) unread")
        }
    }
}
