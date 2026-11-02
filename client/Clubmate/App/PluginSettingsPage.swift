import AppKit
import Observation
import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class SettingsPlugins {
    private(set) var list: [PluginStatus]?
    private(set) var error: String?
    private(set) var restarting: Set<String> = []
    private(set) var switching: [String: Bool] = [:]
    private(set) var editors: CapabilityProviders?
    private(set) var store: WorkspaceUIStore?

    @ObservationIgnored private var client: CoreClient?

    var sorted: [PluginStatus] {
        (list ?? []).sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }

    func icon(_ plugin: String) -> String {
        let manifest = list?.first { $0.id == plugin }?.manifest
        let section = manifest?.contributes.sections.first?.icon
        return SymbolName.resolve(manifest?.icon ?? section, fallback: SymbolName.resolve(section, fallback: "puzzlepiece.extension"))
    }

    func follow(_ client: CoreClient?, active: Bool) async {
        guard let client else {
            error = "Not connected to the core"
            return
        }
        if self.client !== client {
            await store?.stop()
            self.client = client
            store = nil
        }
        guard active || list == nil else { return }
        let events = active ? try? await client.busEvents(pattern: Topic.pluginChanged) : nil
        await load(client)
        await attach(client)
        guard let events else { return }
        for await _ in events {
            await load(client)
        }
    }

    static let editorCapability = "editor.open"

    func isEnabled(_ plugin: PluginStatus) -> Bool {
        switching[plugin.id] ?? plugin.isEnabled
    }

    func setEnabled(_ id: String, _ enabled: Bool) async -> String? {
        guard let client else { return "Not connected to the core" }
        switching[id] = enabled
        defer { switching[id] = nil }
        do {
            try await client.pluginSetEnabled(id: id, enabled: enabled)
            await load(client)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    func setDefaultEditor(_ plugin: String) async -> String? {
        guard let client else { return "Not connected to the core" }
        let previous = editors
        editors?.default = plugin
        do {
            try await client.capabilitySetDefault(name: Self.editorCapability, plugin: plugin)
            await load(client)
            return nil
        } catch {
            editors = previous
            return error.localizedDescription
        }
    }

    func restart(_ id: String) async -> String? {
        guard let client else { return "Not connected to the core" }
        restarting.insert(id)
        defer { restarting.remove(id) }
        do {
            try await client.pluginRestart(id: id)
            await load(client)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    private func load(_ client: CoreClient) async {
        do {
            let plugins = try await client.pluginList()
            if plugins != list { list = plugins }
            let editors = try? await client.capabilityProviders(name: Self.editorCapability)
            if editors != self.editors { self.editors = editors }
            if error != nil { error = nil }
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func attach(_ client: CoreClient) async {
        guard store == nil else { return }
        let store = WorkspaceUIStore(client: client, workspaceID: "")
        self.store = store
        do {
            try await store.start()
        } catch {
            if self.store === store { self.error = error.localizedDescription }
        }
    }
}

extension PluginStatus {
    var statusColor: Color {
        switch state {
        case "running": .green
        case "starting", "backoff": .orange
        case "failed": .red
        default: .secondary
        }
    }
}

struct PluginSidebarRow: View {
    let plugin: PluginStatus
    let icon: String

    var body: some View {
        HStack {
            Label(plugin.name, systemImage: icon)
                .lineLimit(1)
                .foregroundStyle(plugin.isEnabled ? .primary : .secondary)
            Spacer(minLength: 4)
            Circle()
                .fill(plugin.statusColor)
                .frame(width: 6, height: 6)
                .help(plugin.stateLabel)
        }
    }
}

struct PluginSettingsPage: View {
    let plugin: PluginStatus
    let plugins: SettingsPlugins
    @Bindable var settings: AppSettings
    let select: (SettingsPage) -> Void

    @Environment(AppModel.self) private var app
    @Environment(EventBanner.self) private var banner: EventBanner?
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(spacing: 0) {
            PluginSettingsHeader(plugin: plugin, icon: plugins.icon(plugin.id), restarting: plugins.restarting.contains(plugin.id), switching: plugins.switching[plugin.id] != nil, enabled: enabled) {
                Task {
                    if let error = await plugins.restart(plugin.id) { banner?.show(error) }
                }
            }
            Divider()
            if !plugin.isEnabled {
                Form {
                    Section {
                        Text("\(plugin.name) is off. It doesn't run, and its section, commands and agent tools are gone until you turn it on.")
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .formStyle(.grouped)
            } else if plugin.id == "remote" {
                DevicesSettings(model: app.remoteDevices, client: app.supervisor.client, active: true)
            } else {
                Form {
                    Section {
                        PluginNativeRows(plugin: plugin.id, settings: settings, select: select)
                        pluginView
                    } header: {
                        if let sectionTitle { Text(sectionTitle) }
                    }
                    if plugin.id == "agents" {
                        ClaudeCodeSettings()
                    }
                    shortcuts
                }
                .formStyle(.grouped)
                .task(id: plugin.id) { if plugin.id == "agents" { await app.claude.refresh() } }
            }
        }
    }

    private var enabled: Binding<Bool> {
        Binding { plugins.isEnabled(plugin) } set: { [plugins, banner, id = plugin.id] on in
            Task {
                if let error = await plugins.setEnabled(id, on) { banner?.show(error) }
            }
        }
    }

    private var sectionTitle: String? {
        switch plugin.id {
        case "terminals", "editor-nvim": "Font"
        case "agents": "Chat"
        default: nil
        }
    }

    private var slot: String { SettingsPage.slot(plugin: plugin.id) }

    @ViewBuilder private var shortcuts: some View {
        if !plugin.shortcuts.shortcuts.isEmpty {
            Section("Shortcuts") {
                ShortcutRows(group: plugin.shortcuts)
            }
        }
    }

    @ViewBuilder private var pluginView: some View {
        if let store = plugins.store, let view = store.slot(slot) {
            SDUISettingsRows(tree: view.tree, slot: slot, rev: view.rev) { [store] event in
                await store.send(event)
            }
            .environment(\.theme, theme.withSystemFonts(size: NSFont.systemFontSize).withoutGutter())
        } else if plugin.declaresSettings, plugin.healthy || plugin.state == "starting" {
            ProgressView().controlSize(.small)
        } else if plugin.declaresSettings {
            Text("Shows once \(plugin.name) is running").foregroundStyle(.secondary)
        } else if !PluginNativeRows.has(plugin.id) {
            Text("No settings.").foregroundStyle(.secondary)
        }
    }
}

private struct PluginSettingsHeader: View {
    let plugin: PluginStatus
    let icon: String
    let restarting: Bool
    let switching: Bool
    @Binding var enabled: Bool
    let restart: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 10) {
                Image(systemName: icon)
                    .foregroundStyle(.secondary)
                Text(plugin.name)
                    .font(.title3.weight(.semibold))
                    .lineLimit(1)
                    .layoutPriority(1)
                Text("\(plugin.id) \(plugin.manifest.version)")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 10)
                HStack(spacing: 6) {
                    Circle().fill(plugin.statusColor).frame(width: 7, height: 7)
                    Text(plugin.stateLabel).lineLimit(1)
                }
                .font(.caption)
                if plugin.isEnabled {
                    Button("Restart", action: restart)
                        .disabled(restarting || switching || plugin.state == "starting")
                        .help("Stop \(plugin.name) and start it again")
                }
                Toggle("Enabled", isOn: $enabled)
                    .toggleStyle(.switch)
                    .labelsHidden()
                    .disabled(switching)
                    .help(enabled ? "Turn \(plugin.name) off" : "Turn \(plugin.name) on")
            }
            if plugin.isEnabled, !plugin.healthy, !plugin.lastError.isEmpty {
                Text(plugin.lastError)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .lineLimit(2)
                    .textSelection(.enabled)
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 12)
    }
}

private struct PluginNativeRows: View {
    let plugin: String
    @Bindable var settings: AppSettings
    let select: (SettingsPage) -> Void

    @Environment(AppModel.self) private var app

    static func has(_ plugin: String) -> Bool {
        ["terminals", "agents", "editor-nvim"].contains(plugin)
    }

    var body: some View {
        switch plugin {
        case "terminals":
            SettingsToggle(title: "Use ghostty config", detail: "Font and size come from your ghostty config.", isOn: usesGhosttyConfig)
            SettingsStepper(title: "Font size", detail: "Every terminal and nvim. ⌘+ in a terminal changes this too.", value: terminalFontSize, range: AppSettings.terminalFontRange)
                .disabled(settings.terminalFontSize == nil)
        case "agents":
            SettingsRow(title: "Chat font size", detail: "Every agent chat. ⌘+ in the chat changes this too.") {
                HStack(spacing: 10) {
                    if settings.paneFontSizes[PaneFont.chat] != nil {
                        Button("Reset") { settings.resetPaneFont(PaneFont.chat) }
                            .help("Follow the interface font size again")
                    }
                    Text(settingsPoints(settings.chatFontSize))
                        .monospacedDigit()
                    Stepper("Chat font size", value: $settings.chatFontSize, in: AppSettings.interfaceFontRange, step: 1)
                        .labelsHidden()
                }
                .fixedSize()
            }
        case "editor-nvim":
            SettingsRow(title: "Font size", detail: "nvim runs in a terminal and uses Terminals → Font size.") {
                Button("Open Terminals") { select(.plugin("terminals")) }
            }
        default:
            EmptyView()
        }
    }

    private var terminalFontSize: Binding<Double> {
        Binding { app.terminalFontSize } set: { settings.terminalFontSize = $0 }
    }

    private var usesGhosttyConfig: Binding<Bool> {
        Binding { settings.terminalFontSize == nil } set: { settings.terminalFontSize = $0 ? nil : app.terminalFontSize }
    }
}

private struct ClaudeCodeSettings: View {
    @Environment(AppModel.self) private var app
    @State private var hooksCopied = false

    private static let tool = CommandLineTool.name(flavor: ClaudeConnector.bundleFlavor())

    @ViewBuilder var body: some View {
        if app.claude.phase != .missing {
            Section("Claude Code") {
                LabeledContent("Connection") {
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
                                .truncationMode(.middle)
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
                SettingsRow(title: "Notification hooks", detail: "Merge into ~/.claude/settings.json; needs the “\(Self.tool)” command and jq.") {
                    Button(hooksCopied ? "Copied" : "Copy Hooks") { copyHooks() }
                }
                if !app.claude.output.isEmpty {
                    Text(app.claude.output)
                        .font(.caption.monospaced())
                        .textSelection(.enabled)
                }
            }
        }
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
}
