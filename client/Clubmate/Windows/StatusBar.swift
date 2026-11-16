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
    let retry: () -> Void
    let awakeSettings: () -> Void
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
            if connected, let awake {
                KeepAwakeItem(model: awake, settings: awakeSettings)
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

private struct KeepAwakeItem: View {
    let model: KeepAwakeModel
    let settings: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        if let state = model.state {
            menu(state)
        }
    }

    private func menu(_ state: KeepAwake) -> some View {
        Menu {
            Button("Keep Awake for 1 Hour") { set(minutes: 60) }
            Button("Keep Awake for 4 Hours") { set(minutes: 240) }
            Button("Keep Awake Until Turned Off") { set(minutes: nil) }
            if state.manual {
                Button("Turn Off") { Task { await model.turnOff() } }
            }
            Divider()
            Toggle("While Agents Work", isOn: Binding(get: { state.agents }, set: { on in Task { await model.whileAgentsWork(on) } }))
            Divider()
            Button("Keep Awake Settings…", action: settings)
        } label: {
            HStack(spacing: theme.spacing.xs) {
                Image(systemName: state.on ? "cup.and.saucer.fill" : "cup.and.saucer")
                    .foregroundStyle(state.on ? theme.text2 : theme.faint)
                if state.on, !state.text.isEmpty {
                    Text(state.text)
                        .foregroundStyle(theme.muted)
                        .truncationMode(.tail)
                }
            }
            .font(theme.uiSmall)
            .contentShape(Rectangle())
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .fixedSize(horizontal: false, vertical: true)
        .help(help(state))
        .accessibilityLabel(state.on ? state.text : "Keep awake")
        .testMenu("awake", items(state))
    }

    private func set(minutes: Int?) {
        Task { await model.keepAwake(minutes: minutes) }
    }

    private func help(_ state: KeepAwake) -> String {
        [state.detail, model.error].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n")
    }

    private func items(_ state: KeepAwake) -> [LayoutMenuItem] {
        ["Keep Awake for 1 Hour", "Keep Awake for 4 Hours", "Keep Awake Until Turned Off"].map { LayoutMenuItem(title: $0) }
            + (state.manual ? [LayoutMenuItem(title: "Turn Off")] : [])
            + [LayoutMenuItem(title: "While Agents Work", checked: state.agents), LayoutMenuItem(title: "Keep Awake Settings…")]
    }
}
