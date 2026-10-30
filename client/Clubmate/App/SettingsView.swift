import AppKit
import SwiftUI

import ClubmateKit

struct SettingsView: View {
    @Bindable var settings: AppSettings

    @Environment(AppModel.self) private var app
    @State private var hooksCopied = false
    @State private var tab = SettingsTab.general
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
                Toggle("Auto-hide in Code (experimental)", isOn: $settings.autoHideSidebar)
            }
            Section("Editor") {
                Toggle("nvim navigation keys", isOn: navigationKeys)
                NvimConfigSettings(model: app.nvimConfig)
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
        .task { await app.nvimConfig.load() }
        .sheet(isPresented: nvimSheet) { NvimConfigSheetView(model: app.nvimConfig) }
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

    private var navigationKeys: Binding<Bool> {
        Binding { settings.navigationKeys ?? false } set: { settings.navigationKeys = $0 }
    }

    private var nvimSheet: Binding<Bool> {
        Binding { app.nvimConfig.sheet != nil } set: { if !$0 { app.nvimConfig.dismiss() } }
    }

    private func points(_ size: Double) -> String {
        "\(size.formatted()) pt"
    }
}

private enum SettingsTab: Hashable {
    case general
    case plugins
}

private struct PluginsSettings: View {
    let supervisor: ConnectionSupervisor
    let active: Bool

    @State private var plugins: [PluginStatus]?
    @State private var error: String?

    var body: some View {
        Form {
            if let plugins {
                if plugins.isEmpty {
                    Text("No plugins found").foregroundStyle(.secondary)
                }
                ForEach(plugins) { PluginRow(plugin: $0) }
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
        while active, !Task.isCancelled {
            await load()
            try? await Task.sleep(for: .seconds(2))
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

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            HStack(spacing: theme.spacing.l) {
                Text(plugin.name).fontWeight(.medium)
                Text("\(plugin.id) \(plugin.manifest.version)")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                Spacer(minLength: theme.spacing.l)
                HStack(spacing: theme.spacing.s) {
                    Circle().fill(color).frame(width: 7, height: 7)
                    Text(plugin.stateLabel)
                }
                .font(.caption)
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

private struct NvimConfigSettings: View {
    let model: NvimConfigModel

    var body: some View {
        if let info = model.info {
            Picker("Neovim", selection: binary(info)) {
                Text(info.bundledLabel).tag(NvimBinary.bundled)
                Text("System").tag(NvimBinary.system)
            }
            .pickerStyle(.segmented)
            .disabled(model.busy)
            Text(info.systemCaption)
                .font(.caption)
                .foregroundStyle(.secondary)
            Picker("Neovim config", selection: mode(info)) {
                Text("Shared (~/.config/nvim)").tag(NvimConfigMode.shared)
                Text("Clubmate only").tag(NvimConfigMode.clubmate)
            }
            .pickerStyle(.segmented)
            .disabled(model.busy)
            Text("Paths hard-coded to ~/.config/nvim in your config still point there.")
                .font(.caption)
                .foregroundStyle(.secondary)
            if info.mode == .clubmate {
                Button("Reveal Config in Finder") { reveal(info.configDir) }
            }
        } else {
            LabeledContent("Neovim config") {
                if model.loading { ProgressView().controlSize(.small) }
            }
        }
        if !model.pending.isEmpty {
            Text("Restarts when you switch to \(model.pending.map(\.name).formatted(.list(type: .and))).")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        if model.needsRestart {
            LabeledContent("Restart Neovim to apply") {
                Button("Restart Now") { Task { await model.restartAll() } }
                    .disabled(model.busy)
            }
        }
        if let error = model.restartError {
            Text(error)
                .font(.caption)
                .foregroundStyle(.red)
                .textSelection(.enabled)
        }
        if let error = model.error {
            Text(error)
                .font(.caption)
                .foregroundStyle(.red)
                .textSelection(.enabled)
        }
    }

    private func mode(_ info: NvimConfigInfo) -> Binding<NvimConfigMode> {
        Binding { info.mode } set: { mode in Task { await model.choose(mode) } }
    }

    private func binary(_ info: NvimConfigInfo) -> Binding<NvimBinary> {
        Binding { info.binary } set: { binary in Task { await model.chooseBinary(binary) } }
    }

    private func reveal(_ path: String) {
        let url = URL(filePath: path)
        if FileManager.default.fileExists(atPath: path) {
            NSWorkspace.shared.activateFileViewerSelecting([url])
        } else {
            NSWorkspace.shared.open(url.deletingLastPathComponent())
        }
    }
}

private struct NvimConfigSheetView: View {
    let model: NvimConfigModel

    @State private var shown = NvimConfigSheet.offer

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            switch model.sheet ?? shown {
            case .offer:
                offer
            case .copying:
                copying
            case .failed(let message, let useExisting):
                failed(message, useExisting: useExisting)
            }
        }
        .padding(20)
        .frame(width: 420)
        .interactiveDismissDisabled(model.sheet == .copying)
        .onChange(of: model.sheet, initial: true) { _, sheet in
            if let sheet { shown = sheet }
        }
    }

    @ViewBuilder private var offer: some View {
        Text("Separate Neovim config").font(.headline)
        Text("Start the Clubmate config as a copy of your current one, or start empty. State and cache always start fresh.")
            .fixedSize(horizontal: false, vertical: true)
        if let info = model.info {
            LabeledContent("Config (~/.config/nvim)", value: NvimConfigModel.size(info.sourceConfigBytes))
            if info.dataExists {
                Text("\(info.dataDir) already exists and is kept; only the config is copied, to \(info.configDir).")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                LabeledContent("Plugins and data (~/.local/share/nvim)", value: NvimConfigModel.size(info.sourceDataBytes))
                Text("Copies to \(info.configDir) and \(info.dataDir).")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        HStack {
            Button("Cancel") { model.dismiss() }
                .keyboardShortcut(.cancelAction)
            Spacer()
            Button("Start Empty") { Task { await model.switchWithoutCopy() } }
            Button("Copy") { Task { await model.copy() } }
                .keyboardShortcut(.defaultAction)
        }
    }

    @ViewBuilder private var copying: some View {
        Text("Copying Neovim config…").font(.headline)
        if let fraction = model.progress?.fraction {
            ProgressView(value: fraction)
        } else {
            ProgressView().progressViewStyle(.linear)
        }
        if let progress = model.progress, progress.total > 0 {
            Text("\(NvimConfigModel.size(progress.copied)) of \(NvimConfigModel.size(progress.total))")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        if let notice = model.copyNotice {
            Text(notice)
                .font(.caption)
                .foregroundStyle(.red)
        }
        HStack {
            Spacer()
            Button("Cancel") { Task { await model.cancelCopy() } }
                .keyboardShortcut(.cancelAction)
        }
    }

    @ViewBuilder private func failed(_ message: String, useExisting: Bool) -> some View {
        Text("Copy failed").font(.headline)
        Text(message)
            .textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)
        HStack {
            Button("Cancel") { model.dismiss() }
                .keyboardShortcut(.cancelAction)
            Spacer()
            Button(useExisting ? "Use Existing Config" : "Start Empty") { Task { await model.switchWithoutCopy() } }
                .keyboardShortcut(.defaultAction)
        }
    }
}
