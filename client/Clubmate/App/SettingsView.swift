import AppKit
import SwiftUI

import ClubmateKit

struct SettingsView: View {
    @Bindable var settings: AppSettings

    @Environment(AppModel.self) private var app
    @State private var hooksCopied = false
    @State private var tab = SettingsTab.initial
    @Environment(\.appearsActive) private var appearsActive

    private static let tool = CommandLineTool.name(flavor: ClaudeConnector.bundleFlavor())

    var body: some View {
        TabView(selection: $tab) {
            general
                .tabItem { Label("General", systemImage: "gearshape") }
                .tag(SettingsTab.general)
            PluginsSettings(supervisor: app.supervisor, active: appearsActive && tab == .plugins)
                .tabItem { Label("Plugins", systemImage: "puzzlepiece.extension") }
                .tag(SettingsTab.plugins)
            DevicesSettings(model: app.remoteDevices, client: app.supervisor.client, active: tab == .devices)
                .tabItem { Label("Devices", systemImage: "iphone") }
                .tag(SettingsTab.devices)
        }
        .frame(width: 460, height: 560)
    }

    private var general: some View {
        Form {
            Section("Fonts") {
                Stepper(value: $settings.interfaceFontSize, in: AppSettings.interfaceFontRange, step: 1) {
                    LabeledContent("Interface font size", value: points(settings.interfaceFontSize))
                }
                Stepper(value: terminalFontSize, in: AppSettings.terminalFontRange, step: 1) {
                    LabeledContent("Terminal & editor font size", value: points(app.terminalFontSize))
                }
                .disabled(settings.terminalFontSize == nil)
                Toggle("Use ghostty config", isOn: usesGhosttyConfig)
            }
            Section("Sidebar") {
                Picker("Side", selection: $settings.sidebarSide) {
                    ForEach(SidebarSide.allCases, id: \.self) { side in
                        Text(side.rawValue.capitalized).tag(side)
                    }
                }
                .pickerStyle(.segmented)
            }
            if app.claude.phase != .missing {
                Section("Agents") {
                    LabeledContent("Claude Code") {
                        switch app.claude.phase {
                        case .checking, .missing:
                            ProgressView().controlSize(.small)
                        case .disconnected:
                            Button("Connect Claude Code") { Task { await app.claude.connect() } }
                                .disabled(app.claude.busy)
                        case .other(let command):
                            HStack {
                                Text("\(app.claude.serverName) runs \(command.isEmpty ? "another command" : command)")
                                    .lineLimit(1)
                                    .truncationMode(.label)
                                Button("Reconnect") { Task { await app.claude.connect() } }
                                    .disabled(app.claude.busy)
                            }
                        case .connected:
                            HStack {
                                Text("Connected")
                                Button("Reconnect") { Task { await app.claude.connect() } }
                                    .disabled(app.claude.busy)
                            }
                        }
                    }
                    AgentFinishSettings(model: app.agentFinish)
                    LabeledContent("Notifications") {
                        Button(hooksCopied ? "Copied" : "Copy notification hooks") { copyHooks() }
                    }
                    Text("Merge into ~/.claude/settings.json; needs the “\(Self.tool)” command and jq.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    if !app.claude.output.isEmpty {
                        Text(app.claude.output)
                            .font(.caption.monospaced())
                            .textSelection(.enabled)
                    }
                }
            }
        }
        .task { await app.claude.refresh() }
        .task { await app.agentFinish.load() }
        .formStyle(.grouped)
    }

    private func copyHooks() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(ClaudeHooks.snippet(command: Self.tool), forType: .string)
        hooksCopied = true
        Task {
            try? await Task.sleep(for: .seconds(2))
            hooksCopied = false
        }
    }

    private var terminalFontSize: Binding<Double> {
        Binding { app.terminalFontSize } set: { settings.terminalFontSize = $0 }
    }

    private var usesGhosttyConfig: Binding<Bool> {
        Binding { settings.terminalFontSize == nil } set: { settings.terminalFontSize = $0 ? nil : app.terminalFontSize }
    }

    private func points(_ size: Double) -> String {
        "\(size.formatted()) pt"
    }
}

private enum SettingsTab: String, Hashable {
    case general
    case plugins
    case devices

    static var initial: SettingsTab {
        #if DEBUG
        if DevicesPreview.state == "settings" { return .devices }
        let arguments = CommandLine.arguments
        guard let index = arguments.firstIndex(of: "--settings-tab"), arguments.indices.contains(index + 1) else { return .general }
        return SettingsTab(rawValue: arguments[index + 1]) ?? .general
        #else
        .general
        #endif
    }
}

private struct PluginsSettings: View {
    let supervisor: ConnectionSupervisor
    let active: Bool

    @State private var plugins: [PluginStatus]?
    @State private var restarting: Set<String> = []
    @State private var error: String?

    var body: some View {
        Form {
            if let plugins {
                if plugins.isEmpty {
                    Text("No plugins found").foregroundStyle(.secondary)
                }
                ForEach(plugins) { plugin in
                    PluginRow(plugin: plugin, restarting: restarting.contains(plugin.id)) {
                        Task { await restart(plugin.id) }
                    }
                }
            } else if error == nil {
                ProgressView().controlSize(.small)
            }
            if let error {
                Text(error)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .textSelection(.enabled)
            }
        }
        .formStyle(.grouped)
        .task(id: PollKey(client: supervisor.client.map(ObjectIdentifier.init), active: active)) { await poll() }
    }

    private func poll() async {
        if plugins == nil { await load() }
        while active, !Task.isCancelled {
            await load()
            try? await Task.sleep(for: .seconds(2))
        }
    }

    private func restart(_ id: String) async {
        guard let client = supervisor.client else {
            error = "Not connected to the core"
            return
        }
        restarting.insert(id)
        defer { restarting.remove(id) }
        do {
            try await client.pluginRestart(id: id)
            await load()
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func load() async {
        guard let client = supervisor.client else {
            error = "Not connected to the core"
            return
        }
        do {
            let list = try await client.pluginList()
            if list != plugins { plugins = list }
            if error != nil { error = nil }
        } catch {
            self.error = error.localizedDescription
        }
    }
}

private struct PollKey: Equatable {
    let client: ObjectIdentifier?
    let active: Bool
}

private struct PluginRow: View {
    let plugin: PluginStatus
    let restarting: Bool
    let restart: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            HStack(spacing: theme.spacing.l) {
                Text(plugin.name)
                    .fontWeight(.medium)
                    .lineLimit(1)
                    .layoutPriority(1)
                Text("\(plugin.id) \(plugin.manifest.version)")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.label)
                Spacer(minLength: theme.spacing.l)
                HStack(spacing: theme.spacing.s) {
                    Circle().fill(color).frame(width: 7, height: 7)
                    Text(plugin.stateLabel).lineLimit(1)
                }
                .font(.caption)
                Button("Restart", action: restart)
                    .controlSize(.small)
                    .disabled(restarting || plugin.state == "starting")
                    .help("Stop \(plugin.name) and start it again")
            }
            Text(summary)
                .font(.caption)
                .foregroundStyle(.secondary)
            if !plugin.healthy, !plugin.lastError.isEmpty {
                Text(plugin.lastError)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .lineLimit(2)
                    .textSelection(.enabled)
            }
        }
    }

    private var summary: String {
        let sections = plugin.manifest.contributes.sections.map(\.title)
        let commands = plugin.manifest.contributes.commands.count
        var parts = [sections.isEmpty ? "No sections" : "Sections: \(sections.joined(separator: ", "))"]
        parts.append(commands == 1 ? "1 command" : "\(commands) commands")
        if plugin.restarts > 0 { parts.append(plugin.restarts == 1 ? "1 restart" : "\(plugin.restarts) restarts") }
        return parts.joined(separator: " · ")
    }

    private var color: Color {
        switch plugin.state {
        case "running": .green
        case "starting", "backoff": .orange
        case "failed": .red
        default: .secondary
        }
    }
}

private struct AgentFinishSettings: View {
    let model: AgentFinishModel

    var body: some View {
        Picker("When a task is done", selection: mode) {
            ForEach(AgentFinishMode.allCases, id: \.self) { mode in
                Text(mode.label).tag(Optional(mode))
            }
        }
        .disabled(model.mode == nil)
        Text("Default for new sessions; change it per session in the composer options. Merge runs the tests, merges into main and removes the worktree.")
            .font(.caption)
            .foregroundStyle(.secondary)
        if let error = model.error {
            Text(error)
                .font(.caption)
                .foregroundStyle(.red)
                .textSelection(.enabled)
        }
    }

    private var mode: Binding<AgentFinishMode?> {
        Binding { model.mode } set: { mode in
            guard let mode else { return }
            Task { await model.choose(mode) }
        }
    }
}
