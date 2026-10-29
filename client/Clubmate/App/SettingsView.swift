import AppKit
import SwiftUI

import ClubmateKit

struct SettingsView: View {
    @Bindable var settings: AppSettings

    @Environment(AppModel.self) private var app

    var body: some View {
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
        .frame(width: 420)
        .fixedSize(horizontal: false, vertical: true)
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

private struct NvimConfigSettings: View {
    let model: NvimConfigModel

    var body: some View {
        if let info = model.info {
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
