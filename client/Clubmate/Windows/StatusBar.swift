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
    let awake: KeepAwakeModel?
    let hints: String?
    var mode: String?
    var pairPhone: (() -> Void)?
    let retry: () -> Void
    let awakeSettings: () -> Void
    let openSession: (_ session: String, _ workspace: String) -> Void
    @ViewBuilder let trailing: Trailing

    @State private var width: CGFloat = 0
    @State private var reconnectShown = false
    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.gutter * 1.5) {
            if let voice {
                VoiceButton(voice: voice)
            }
            if let mode {
                Text(mode)
                    .font(theme.uiSmall.weight(.semibold))
                    .foregroundStyle(theme.text2)
                    .fixedSize()
                    .testNode("statusbar.mode", role: "text", label: mode)
            }
            HStack(spacing: theme.spacing.s) {
                Text(dot).foregroundStyle(tone)
                Text(label).foregroundStyle(quiet ? theme.muted : tone)
            }
            .help(failure ?? "")
            if failure != nil {
                Button(action: retry) {
                    HStack(spacing: theme.spacing.s) {
                        Text("Retry")
                        Text(Shortcut.label(AppShortcuts.retry.key)).foregroundStyle(theme.muted)
                    }
                }
                .buttonStyle(.plain)
                .foregroundStyle(theme.accentInk)
            } else if reconnectShown, case .reconnecting(let attempt) = connection, attempt >= 3 {
                Button("Retry now", action: retry)
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
            if connected, let awake {
                KeepAwakeItem(model: awake, barWidth: width, settings: awakeSettings, openSession: openSession)
            }
            if connected, let pairPhone {
                Button(action: pairPhone) {
                    Label("Pair iPhone", systemImage: "iphone")
                        .labelStyle(StatusItemLabel())
                }
                .buttonStyle(.plain)
                .foregroundStyle(theme.muted)
                .fixedSize()
                .help("Pair an iPhone or iPad")
                .accessibilityIdentifier("statusbar.pair-phone")
            }
            trailing
                .lineLimit(1)
                .fixedSize()
                .frame(height: 22)
                .clipped()
            if let hints {
                Text(hints)
                    .foregroundStyle(theme.muted)
                    .testNode("statusbar.hints", role: "text", label: hints)
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
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .background(theme.surface1, ignoresSafeAreaEdges: [])
        .overlay(alignment: .top) { theme.border.frame(height: 1) }
        .task(id: reconnecting) {
            reconnectShown = false
            guard reconnecting else { return }
            try? await Task.sleep(for: Self.reconnectQuiet)
            reconnectShown = !Task.isCancelled
        }
    }

    private static var reconnectQuiet: Duration { .milliseconds(500) }

    private var connected: Bool {
        if case .connected = connection { return true }
        return false
    }

    private var reconnecting: Bool {
        if case .reconnecting = connection { return true }
        return false
    }

    private var quiet: Bool {
        connected || reconnecting && !reconnectShown
    }

    private var failure: String? {
        if case .failed(let message) = connection { return message }
        return nil
    }

    private var dot: String {
        quiet || failure != nil ? "●" : "○"
    }

    private var label: String {
        switch connection {
        case .connected: "core"
        case .connecting: "core connecting"
        case .reconnecting(let attempt): reconnectShown ? "core reconnecting (attempt \(attempt))" : "core"
        case .failed: "core stopped"
        }
    }

    private var tone: Color {
        switch connection {
        case .connected: theme.muted
        case .connecting: theme.warning
        case .reconnecting: reconnectShown ? theme.warning : theme.muted
        case .failed: theme.danger
        }
    }
}

private struct StatusItemLabel: LabelStyle {
    @Environment(\.theme) private var theme

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: theme.spacing.s) {
            configuration.icon
            configuration.title
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
            if tally.open > 0 {
                if active && tally.active > 0 || tally.needYou > 0 { Text(" · ").foregroundStyle(theme.muted) }
                Text("\(tally.open) open").foregroundStyle(theme.muted)
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
