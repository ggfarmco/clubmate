import SwiftUI

import ClubmateKit

enum ConnectionStatus {
    case connected
    case connecting
    case reconnecting(attempt: Int)
    case failed(String)
}

struct StatusBar<Trailing: View>: View {
    let connection: ConnectionStatus
    let section: String
    let usage: SubscriptionUsage?
    let retry: () -> Void
    @ViewBuilder let trailing: Trailing

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.gutter * 1.5) {
            Text(label).foregroundStyle(tone)
            if case .failed = connection {
                Button("Retry", action: retry)
                    .buttonStyle(.plain)
                    .foregroundStyle(theme.accentInk)
            }
            Text(section).foregroundStyle(theme.muted)
            UsageStatus(usage: usage)
                .layoutPriority(1)
            Spacer(minLength: 0)
            trailing
                .lineLimit(1)
                .fixedSize(horizontal: true, vertical: false)
                .frame(height: 22)
                .clipped()
        }
        .font(theme.uiSmall)
        .lineLimit(1)
        .padding(.horizontal, theme.gutter)
        .frame(height: 22)
        .background(theme.surface1, ignoresSafeAreaEdges: [])
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

private struct UsageStatus: View {
    let usage: SubscriptionUsage?

    @Environment(\.theme) private var theme

    var body: some View {
        TimelineView(.everyMinute) { context in
            let windows = usage?.current(now: context.date) ?? []
            if !windows.isEmpty {
                HStack(spacing: 0) {
                    ForEach(Array(windows.enumerated()), id: \.element.kind) { index, window in
                        if index > 0 { Text(" · ").foregroundStyle(theme.muted) }
                        Text("\(window.label) \(Image(systemName: "arrow.clockwise"))\(window.resetLabel(now: context.date))").foregroundStyle(tone(window.status))
                    }
                }
                .fixedSize()
                .help(help(windows, now: context.date))
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(windows.map { $0.detail(now: context.date) }.joined(separator: ". "))
            }
        }
    }

    private func tone(_ status: UsageWindow.Status) -> Color {
        switch status {
        case .allowed: theme.muted
        case .warning: theme.warning
        case .rejected: theme.danger
        }
    }

    private func help(_ windows: [UsageWindow], now: Date) -> String {
        var lines = windows.map { $0.detail(now: now) }
        if let updated = usage?.updatedAt {
            lines.append("Updated \(updated.formatted(date: .omitted, time: .shortened)) from the latest agent reply")
        }
        return lines.joined(separator: "\n")
    }
}
