import SwiftUI

import CompanionKit

struct ConnectionSheet: Identifiable, Hashable {
    let computer: String

    var id: String { computer }
}

struct ConnectionView: View {
    let computerID: String
    @Environment(CompanionModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @Environment(AppNavigation.self) private var navigation
    @State private var check: ConnectionCheck?
    @State private var copied = false

    var body: some View {
        NavigationStack {
            if let connection = model.connection(computerID) {
                content(connection)
            } else {
                ContentUnavailableView("Mac Removed", systemImage: "desktopcomputer")
            }
        }
    }

    private func content(_ connection: ComputerConnection) -> some View {
        let check = check ?? connection.makeCheck(probe: Self.probe)
        return List {
            Section {
                LabeledContent("Path", value: pathValue(connection))
                    .accessibilityIdentifier("connection.path")
            } footer: {
                if !connection.pathChoices.isEmpty {
                    Text("Change it in \(connection.computer.name)'s details › Connect through.")
                }
            }
            Section {
                Button {
                    self.check = check
                    Task { await check.run() }
                } label: {
                    HStack(spacing: 10) {
                        if check.running {
                            ProgressView()
                            Text("Checking…")
                        } else {
                            Image(systemName: "stethoscope")
                            Text(check.finishedAt == nil ? "Run check" : "Run again")
                        }
                    }
                    .frame(maxWidth: .infinity)
                    .contentShape(.rect)
                }
                .disabled(check.running)
                .accessibilityIdentifier("connection.run")
                ForEach(check.steps) { step in
                    CheckStepRow(step: step)
                        .listRowDensity()
                }
            } footer: {
                Text(checkFooter(check, connection))
            }
            Section {
                let attempts = connection.log.attempts.reversed()
                if attempts.isEmpty {
                    Text("No attempts yet")
                        .foregroundStyle(Color.captionInk)
                } else {
                    ForEach(attempts) { attempt in
                        AttemptRow(attempt: attempt)
                            .listRowDensity()
                    }
                }
            } header: {
                Text("Recent attempts")
            } footer: {
                Text("The last \(ConnectionLog.capacity) tries to reach \(connection.computer.name), newest first. Kept on this phone.")
            }
        }
        .listStyle(.insetGrouped)
        .environment(\.defaultMinListRowHeight, 44)
        .navigationTitle("Connection")
        .navigationBarTitleDisplayMode(.inline)
        .navigationSubtitle(connection.computer.name + " · " + connection.presence)
        .sheet(item: Binding(get: { navigation.reportSheet }, set: { navigation.reportSheet = $0 })) { sheet in
            ReportView(sheet: sheet)
        }
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Done") { dismiss() }
            }
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Button(copied ? "Copied" : "Copy as Text", systemImage: copied ? "checkmark" : "doc.on.doc") {
                        UIPasteboard.general.string = report(connection, check)
                        copied = true
                        Task {
                            try? await Task.sleep(for: .seconds(1.5))
                            copied = false
                        }
                    }
                    Button("Report a Problem…", systemImage: "exclamationmark.bubble") {
                        navigation.reportSheet = ReportSheet(computer: computerID, about: nil)
                    }
                    .accessibilityIdentifier("connection.report")
                } label: {
                    Image(systemName: "ellipsis")
                }
                .accessibilityLabel("More")
            }
        }
    }

    private func pathValue(_ connection: ComputerConnection) -> String {
        let active = connection.activePath?.label ?? "Not connected"
        return connection.pathChoices.isEmpty ? active : active + " · " + connection.computer.path.label
    }

    private func checkFooter(_ check: ConnectionCheck, _ connection: ComputerConnection) -> String {
        guard let finishedAt = check.finishedAt else {
            return "Checks each step between this phone and \(connection.computer.name) and says what to fix."
        }
        let verdict = check.failed ? "Something's off — see the steps above." : "Everything answered. Reconnecting now."
        return verdict + " Checked at " + ComputerConnection.clock(finishedAt) + "."
    }

    private func report(_ connection: ComputerConnection, _ check: ConnectionCheck) -> String {
        var parts = ["\(connection.computer.name) · \(connection.presence) · port \(connection.computer.port) · path \(connection.activePath?.rawValue ?? "none") (\(connection.computer.path.rawValue))"]
        if !check.steps.isEmpty { parts.append("Check\n" + check.text) }
        parts.append("Recent attempts\n" + (connection.log.attempts.isEmpty ? "none" : connection.log.text))
        return parts.joined(separator: "\n\n")
    }

    private static var probe: any NetworkProbe {
        #if DEBUG
        DebugLaunch.probe() ?? SystemProbe()
        #else
        SystemProbe()
        #endif
    }
}

private struct CheckStepRow: View {
    let step: CheckStep

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            glyph
                .frame(width: 22)
            VStack(alignment: .leading, spacing: 3) {
                Text(step.title)
                    .lineLimit(2)
                if let detail = step.detail {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(step.verdict == .fail ? Color.dangerInk : Color.captionInk)
                }
                if let hint = step.hint {
                    Text(hint)
                        .font(.footnote)
                }
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("connection.step." + step.id)
    }

    @ViewBuilder
    private var glyph: some View {
        switch step.verdict {
        case .running:
            ProgressView()
        case .pass:
            Image(systemName: "checkmark.circle.fill").foregroundStyle(Color.successInk)
        case .fail:
            Image(systemName: "xmark.circle.fill").foregroundStyle(Color.dangerInk)
        case .skipped:
            Image(systemName: "minus.circle").foregroundStyle(.tertiary)
        }
    }
}

private struct AttemptRow: View {
    let attempt: ConnectionAttempt

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Image(systemName: attempt.ok ? "checkmark.circle.fill" : "xmark.circle.fill")
                .foregroundStyle(attempt.ok ? Color.successInk : Color.dangerInk)
                .frame(width: 22)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(attempt.title)
                        .lineLimit(1)
                        .layoutPriority(1)
                    Spacer(minLength: 8)
                    Text(when)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
                Text(summary)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(3)
                ForEach(Array(attempt.tries.enumerated().filter { !$0.element.ok }), id: \.offset) { _, entry in
                    Text(verbatim: entry.host + " · " + Self.seconds(entry.milliseconds) + " · " + entry.outcome)
                        .font(.caption.monospaced())
                        .lineLimit(3)
                        .textSelection(.enabled)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var when: String {
        var parts = [ComputerConnection.clock(attempt.repeats > 1 ? attempt.last : attempt.at)]
        if attempt.repeats > 1 { parts.append("\(attempt.repeats)×") }
        if attempt.background { parts.append("background") }
        return parts.joined(separator: " · ")
    }

    private var summary: String {
        if attempt.ok {
            let via = attempt.host.map { "via \($0)" } ?? "connected"
            return via + " · " + Self.seconds(attempt.milliseconds)
        }
        var parts = [attempt.error ?? "failed"]
        if attempt.repeats > 1 { parts.append("since " + ComputerConnection.clock(attempt.at)) }
        if attempt.streak > 1 { parts.append("\(attempt.streak) failures in a row") }
        return parts.joined(separator: " · ")
    }

    private static func seconds(_ milliseconds: Int) -> String {
        milliseconds < 1000 ? "\(milliseconds) ms" : String(format: "%.1f s", Double(milliseconds) / 1000)
    }
}
