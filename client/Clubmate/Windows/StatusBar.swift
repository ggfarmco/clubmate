import SwiftUI

import ClubmateKit

enum ConnectionStatus {
    case connected
    case connecting
    case reconnecting(attempt: Int)
    case failed(String)
}

struct StatusBar<Trailing: View>: View {
    var voice: VoiceController?
    let connection: ConnectionStatus
    let agents: AgentTally?
    let usage: SubscriptionUsage?
    let hints: String?
    let retry: () -> Void
    @ViewBuilder let trailing: Trailing

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.gutter * 1.5) {
            if let voice {
                VoiceButton(voice: voice)
            }
            HStack(spacing: theme.spacing.s) {
                Text(dot).foregroundStyle(tone)
                Text(label).foregroundStyle(connected ? theme.muted : tone)
            }
            if case .failed = connection {
                Button("Retry", action: retry)
                    .buttonStyle(.plain)
                    .foregroundStyle(theme.accentInk)
            }
            if connected, let agents {
                AgentCounts(tally: agents)
            }
            UsageStatus(usage: usage)
                .layoutPriority(1)
            Spacer(minLength: 0)
                .layoutPriority(-2)
            trailing
                .lineLimit(1)
                .fixedSize()
                .frame(height: 22)
                .clipped()
            if let hints {
                Text(hints)
                    .foregroundStyle(theme.muted)
                    .truncationMode(.tail)
                    .help(hints)
                    .layoutPriority(-1)
            }
            Text("⌘K")
                .foregroundStyle(theme.muted)
                .fixedSize()
                .help("Command palette (⌘K)")
        }
        .font(theme.uiSmall)
        .lineLimit(1)
        .padding(.horizontal, theme.gutter)
        .frame(height: 22)
        .background(theme.surface1, ignoresSafeAreaEdges: [])
        .overlay(alignment: .top) { theme.border.frame(height: 1) }
    }

    private var connected: Bool {
        if case .connected = connection { return true }
        return false
    }

    private var dot: String {
        connected ? "●" : "○"
    }

    private var label: String {
        switch connection {
        case .connected: "core"
        case .connecting: "core connecting"
        case .reconnecting(let attempt): "core reconnecting (attempt \(attempt))"
        case .failed(let message): message
        }
    }

    private var tone: Color {
        switch connection {
        case .connected: theme.muted
        case .connecting, .reconnecting: theme.warning
        case .failed: theme.danger
        }
    }
}

private struct AgentCounts: View {
    let tally: AgentTally

    @Environment(\.theme) private var theme

    var body: some View {
        ViewThatFits(in: .horizontal) {
            counts(active: true)
            counts(active: false)
        }
    }

    private func counts(active: Bool) -> some View {
        HStack(spacing: 0) {
            if active, tally.active > 0 {
                Text(tally.active == 1 ? "1 agent" : "\(tally.active) agents").foregroundStyle(theme.muted)
                if tally.needYou > 0 { Text(" · ").foregroundStyle(theme.muted) }
            }
            if tally.needYou > 0 {
                Text(tally.needYouLabel).foregroundStyle(theme.warning)
            }
        }
        .monospacedDigit()
        .fixedSize()
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
