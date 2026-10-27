import SwiftUI

struct StatusBar: View {
    enum Connection {
        case connected
        case reconnecting
        case failed(String)
    }

    let connection: Connection
    let section: String
    let retry: () -> Void

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
        case .reconnecting: "○ reconnecting"
        case .failed(let message): "○ \(message)"
        }
    }

    private var tone: Color {
        switch connection {
        case .connected: theme.success
        case .reconnecting: theme.warning
        case .failed: theme.danger
        }
    }
}
