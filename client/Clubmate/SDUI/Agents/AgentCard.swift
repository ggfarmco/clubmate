import SwiftUI

struct CardButton: View {
    enum Tone {
        case plain
        case primary
        case danger
    }

    let label: String
    var icon: String?
    var tone = Tone.plain
    var disabled = false
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            HStack(spacing: theme.spacing.s) {
                if let icon {
                    Image(systemName: SymbolName.resolve(icon))
                }
                Text(label)
                    .lineLimit(1)
            }
            .font(theme.uiSmall)
            .foregroundStyle(foreground)
            .padding(theme.spacing.chip)
            .background(RoundedRectangle(cornerRadius: 5).fill(fill))
            .overlay(RoundedRectangle(cornerRadius: 5).stroke(tone == .primary ? .clear : theme.border, lineWidth: 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled && tone != .primary ? 0.6 : 1)
        .onHover { hovering = $0 }
    }

    private var foreground: Color {
        switch tone {
        case .primary: theme.background
        case .danger: theme.danger
        case .plain: theme.foreground
        }
    }

    private var fill: Color {
        switch tone {
        case .primary: theme.accent.opacity(hovering && !disabled ? 0.85 : 1)
        default: theme.foreground.opacity(hovering && !disabled ? 0.08 : 0.03)
        }
    }
}

extension View {
    func agentCard() -> some View {
        modifier(AgentCard())
    }
}

private struct AgentCard: ViewModifier {
    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        content
            .background(RoundedRectangle(cornerRadius: 8).fill(theme.foreground.opacity(0.03)))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).stroke(theme.border, lineWidth: 1))
    }
}
