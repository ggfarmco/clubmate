import SwiftUI

struct StatusBar: View {
    enum Connection {
        case connected
        case reconnecting
        case disconnected
    }

    let connection: Connection
    let section: String

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.gutter * 1.5) {
            Text(label).foregroundStyle(tone)
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
        case .disconnected: "○ disconnected"
        }
    }

    private var tone: Color {
        switch connection {
        case .connected: theme.success
        case .reconnecting: theme.warning
        case .disconnected: theme.danger
        }
    }
}
