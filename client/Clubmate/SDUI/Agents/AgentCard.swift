import SwiftUI

struct CardButton: View {
    enum Tone {
        case plain
        case primary
        case danger
    }

    let label: String
    var icon: String?
    var shortcut: String?
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
                if let shortcut {
                    Text(shortcut)
                        .opacity(0.7)
                }
            }
            .font(theme.uiSmall)
            .foregroundStyle(foreground)
            .padding(theme.spacing.chip)
            .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(fill))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(stroke, lineWidth: 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled && tone != .primary ? 0.6 : 1)
        .onHover { hovering = $0 }
    }

    private var foreground: Color {
        switch tone {
        case .primary: theme.onAccent
        case .danger: theme.danger
        case .plain: theme.foreground
        }
    }

    private var stroke: Color {
        switch tone {
        case .primary: .clear
        case .danger: theme.toneBorder(theme.danger)
        case .plain: theme.borderStrong
        }
    }

    private var fill: Color {
        switch tone {
        case .primary: theme.accent.opacity(hovering && !disabled ? 0.85 : 1)
        default: hovering && !disabled ? theme.pressed : theme.surface2
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
            .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.surface2))
            .clipShape(RoundedRectangle(cornerRadius: theme.radius.l))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.border, lineWidth: 1))
    }
}
