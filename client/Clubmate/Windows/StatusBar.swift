import SwiftUI

enum ConnectionStatus {
    case connected
    case connecting
    case reconnecting(attempt: Int)
    case failed(String)
}

struct StatusBar<Trailing: View>: View {
    let connection: ConnectionStatus
    let section: String
    let retry: () -> Void
    @ViewBuilder let trailing: Trailing

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.gutter * 1.5) {
            Text(label).foregroundStyle(tone)
            if case .failed = connection {
                Button("Retry", action: retry)
                    .buttonStyle(.plain)
                    .foregroundStyle(theme.accent)
            }
            Text(section).foregroundStyle(theme.muted)
            Spacer(minLength: 0)
            trailing
                .lineLimit(1)
                .fixedSize(horizontal: true, vertical: false)
                .frame(height: 20)
                .clipped()
        }
        .font(theme.uiSmall)
        .lineLimit(1)
        .padding(.horizontal, theme.gutter)
        .frame(height: 20)
        .overlay(alignment: .top) { theme.border.frame(height: 1) }
    }

    private var label: String {
        switch connection {
        case .connected: "● connected"
        case .connecting: "○ connecting"
        case .reconnecting(let attempt): "○ reconnecting (attempt \(attempt))"
        case .failed(let message): "○ \(message)"
        }
    }

    private var tone: Color {
        switch connection {
        case .connected: theme.success
        case .connecting, .reconnecting: theme.warning
        case .failed: theme.danger
        }
    }
}
