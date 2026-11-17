import SwiftUI

import CompanionKit

struct UsageSheet: Identifiable, Hashable {
    let computer: String

    var id: String { computer }
}

extension UsageTone {
    var ink: Color {
        switch self {
        case .normal: .captionInk
        case .warning: .warningInk
        case .danger: .dangerInk
        }
    }
}

extension UsageLine {
    var styled: Text {
        let lead = label.map { Text($0 + " · ").foregroundStyle(Color.captionInk) } ?? Text("")
        return parts.reduce(lead) { text, part in
            text + Text(part.text).fontWeight(part.strong ? .semibold : nil).foregroundStyle(tone.ink)
        }
    }
}

struct UsageHeaderLine: View {
    let computer: String
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        TimelineView(.periodic(from: .now, by: 60)) { timeline in
            if let line = model.claudeAccounts.line(for: computer, now: timeline.date) {
                Button {
                    navigation.usageSheet = UsageSheet(computer: computer)
                } label: {
                    line.styled
                        .font(.subheadline)
                        .lineLimit(1)
                        .padding(.vertical, 12)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .padding(.vertical, -12)
                .accessibilityLabel("Claude usage: " + line.text)
                .accessibilityHint("Shows the Claude usage of your Macs")
                .accessibilityIdentifier("usage.header")
            }
        }
    }
}

struct ClaudeUsageView: View {
    let computerID: String
    @Environment(CompanionModel.self) private var model
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            TimelineView(.periodic(from: .now, by: 30)) { timeline in
                let accounts = model.claudeAccounts
                let others = accounts.others(than: computerID)
                List {
                    if let mine = accounts.account(of: computerID) {
                        Section {
                            UsageWindowRows(usage: mine.usage, now: timeline.date)
                        } header: {
                            VStack(alignment: .leading, spacing: 12) {
                                Text("This Mac's account")
                                    .font(.subheadline.weight(.semibold))
                                    .foregroundStyle(.primary)
                                AccountHeader(name: mine.name, detail: seen(["via " + mine.via], mine, now: timeline.date))
                            }
                            .textCase(nil)
                        } footer: {
                            if others.isEmpty { note }
                        }
                    }
                    ForEach(Array(others.enumerated()), id: \.element.id) { index, account in
                        Section {
                            UsageWindowRows(usage: account.usage, now: timeline.date)
                        } header: {
                            VStack(alignment: .leading, spacing: 12) {
                                if index == 0 {
                                    Text("Your other Macs")
                                        .font(.subheadline.weight(.semibold))
                                        .foregroundStyle(.primary)
                                }
                                AccountHeader(name: account.name, detail: seen([account.macs.joined(separator: ", ")], account, now: timeline.date))
                            }
                            .textCase(nil)
                        } footer: {
                            if index == others.count - 1 { note }
                        }
                    }
                }
                .listStyle(.insetGrouped)
                .accessibilityIdentifier("usage.sheet")
            }
            .navigationTitle("Claude usage")
            .toolbarTitleDisplayMode(.inlineLarge)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(role: .close) { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private var note: some View {
        Text("Each account's windows are its own; the phone never adds them up. The label in a header is the email up to “@”, plus the domain's first part when two accounts share it.")
    }

    private func seen(_ lead: [String], _ account: UsageAccount, now: Date) -> String {
        (lead + [account.seen(now: now)].compactMap { $0 }).joined(separator: " · ")
    }
}

struct SessionInfoView: View {
    let session: AgentSession
    let context: String
    let computerID: String
    @Environment(CompanionModel.self) private var model
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            TimelineView(.periodic(from: .now, by: 30)) { timeline in
                List {
                    Section {
                        InfoRow(title: "Context", value: context)
                        InfoRow(title: "Share of 5-hour window", value: session.shareWord(.fiveHour))
                        InfoRow(title: "Share of 7-day window", value: session.shareWord(.sevenDay))
                    } header: {
                        Text("This session")
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(.primary)
                            .textCase(nil)
                    } footer: {
                        Text("≈ is an estimate: Claude reports only the account total, so each session gets a share of its growth by what it spent. Claude used outside Clubmate counts here too.")
                    }
                    if let account = model.claudeAccounts.account(of: computerID) {
                        Section {
                            InfoRow(title: "Account", value: account.email.isEmpty ? "—" : account.email)
                            UsageWindowRows(usage: account.usage, now: timeline.date)
                        } header: {
                            AccountHeader(name: "Claude account", detail: (["via " + account.via] + [account.seen(now: timeline.date)].compactMap { $0 }).joined(separator: " · "))
                                .textCase(nil)
                        }
                    }
                }
                .listStyle(.insetGrouped)
                .accessibilityIdentifier("session.info")
            }
            .navigationTitle("Session info")
            .toolbarTitleDisplayMode(.inlineLarge)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(role: .close) { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}

private struct AccountHeader: View {
    let name: String
    let detail: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(name)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.captionInk)
                .lineLimit(1)
                .truncationMode(.middle)
                .layoutPriority(1)
            Spacer(minLength: 8)
            Text(detail)
                .font(.footnote)
                .foregroundStyle(Color.captionInk)
                .lineLimit(1)
        }
    }
}

private struct UsageWindowRows: View {
    let usage: ClaudeUsage
    let now: Date

    var body: some View {
        ForEach(LimitWindowKind.allCases, id: \.self) { kind in
            if let window = usage.window(kind, now: now) {
                InfoRow(title: kind.title, value: window.value(now: now), ink: window.tone.ink)
            } else {
                InfoRow(title: kind.title, value: "—")
            }
        }
    }
}

private struct InfoRow: View {
    let title: String
    let value: String
    var ink: Color = .captionInk

    var body: some View {
        LabeledContent {
            Text(value)
                .foregroundStyle(ink)
                .lineLimit(1)
        } label: {
            Text(title)
                .lineLimit(1)
        }
        .listRowDensity()
    }
}
