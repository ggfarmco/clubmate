import SwiftUI

import ClubmateKit

struct SettingsView: View {
    @Bindable var settings: AppSettings

    @Environment(AppModel.self) private var app
    @Environment(\.appearsActive) private var appearsActive
    @Environment(\.theme) private var theme
    @State private var page = SettingsPage.initial
    @State private var plugins = SettingsPlugins()

    var body: some View {
        HStack(spacing: 0) {
            List(selection: $page) {
                Label("General", systemImage: "gearshape")
                    .tag(SettingsPage.general)
                Label("Shortcuts", systemImage: "command")
                    .tag(SettingsPage.shortcuts)
                Section("Plugins") {
                    ForEach(plugins.sorted) { plugin in
                        PluginSidebarRow(plugin: plugin, icon: plugins.icon(plugin.id))
                            .tag(SettingsPage.plugin(plugin.id))
                    }
                }
            }
            .listStyle(.sidebar)
            .scrollContentBackground(.hidden)
            .background(theme.surface1)
            .frame(width: 190)
            Rectangle().fill(theme.border).frame(width: 1)
            detail
                .scrollContentBackground(.hidden)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(theme.background)
        }
        .frame(width: 720, height: 560)
        .tint(theme.accentInk)
        .background(WindowObserver { } onAttach: { [surface = NSColor(theme.surface1), dark = theme.isDark] window in
            window.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
            window.titlebarAppearsTransparent = true
            window.backgroundColor = surface
        } onClose: { })
        .onAppear { if page == .general { page = .initial } }
        .task(id: SettingsFollowKey(client: app.supervisor.client.map(ObjectIdentifier.init), active: appearsActive)) {
            await plugins.follow(app.supervisor.client, active: appearsActive)
        }
    }

    @ViewBuilder private var detail: some View {
        switch page {
        case .general:
            GeneralSettings(settings: settings, plugins: plugins)
                .eventBanner()
        case .shortcuts:
            ShortcutsSettings(plugins: plugins)
        case .plugin(let id):
            if let plugin = plugins.list?.first(where: { $0.id == id }) {
                PluginSettingsPage(plugin: plugin, plugins: plugins, settings: settings) { page = $0 }
                    .eventBanner()
                    .id(id)
            } else if let error = plugins.error {
                SettingsNotice(text: error)
            } else if plugins.list == nil {
                ProgressView().controlSize(.small)
            } else {
                SettingsNotice(text: "No plugin “\(id)”")
            }
        }
    }
}

extension SettingsPage {
    static var initial: SettingsPage {
        #if DEBUG
        if DevicesPreview.state == "settings" { return .plugin("remote") }
        let arguments = CommandLine.arguments
        guard let index = arguments.firstIndex(of: "--settings-tab"), arguments.indices.contains(index + 1) else { return .general }
        return SettingsPage(argument: arguments[index + 1])
        #else
        .general
        #endif
    }
}

private struct SettingsFollowKey: Equatable {
    let client: ObjectIdentifier?
    let active: Bool
}

struct SettingsNotice: View {
    let text: String

    var body: some View {
        Text(text)
            .foregroundStyle(.secondary)
            .textSelection(.enabled)
            .padding()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

private struct GeneralSettings: View {
    @Bindable var settings: AppSettings
    let plugins: SettingsPlugins

    @Environment(EventBanner.self) private var banner: EventBanner?

    var body: some View {
        Form {
            Section("Appearance") {
                SettingsRow(title: "Interface font", detail: "Sidebars, lists, forms and chats. Code, commands and diffs use the Terminals font.") {
                    FontFamilyPicker(selection: interfaceFamily, inherited: nil)
                }
                SettingsStepper(title: "Interface font size", detail: "⌘+ in a pane sets that plugin's own size.", value: $settings.interfaceFontSize, range: AppSettings.interfaceFontRange)
                SettingsRow(title: "Sidebar side") {
                    Picker("Sidebar side", selection: $settings.sidebarSide) {
                        ForEach(SidebarSide.allCases, id: \.self) { side in
                            Text(side.rawValue.capitalized).tag(side)
                        }
                    }
                    .pickerStyle(.segmented)
                    .labelsHidden()
                    .fixedSize()
                }
                SettingsRow(title: "Plugin fonts", detail: overrides) {
                    Button("Reset All") { settings.resetPaneFonts() }
                        .disabled(settings.paneFontOverrideCount == 0)
                }
            }
            if let editors = plugins.editors, editors.enabled.count > 1 {
                Section("Files") {
                    SettingsRow(title: "Default editor", detail: "Files you open from a chat, a link or the palette open here.") {
                        Picker("Default editor", selection: defaultEditor(editors)) {
                            ForEach(editors.enabled) { editor in
                                Text(editor.name).tag(editor.plugin)
                            }
                        }
                        .labelsHidden()
                        .fixedSize()
                    }
                }
            }
        }
        .formStyle(.grouped)
    }

    private func defaultEditor(_ editors: CapabilityProviders) -> Binding<String> {
        Binding { editors.default } set: { [plugins, banner] plugin in
            Task {
                if let error = await plugins.setDefaultEditor(plugin) { banner?.show(error) }
            }
        }
    }

    private var interfaceFamily: Binding<String?> {
        Binding { settings.interfaceFontFamily } set: { settings.interfaceFontFamily = $0 ?? FontFamily.terminal }
    }

    private var overrides: String {
        switch settings.paneFontOverrideCount {
        case 0: "Every plugin uses the interface font. A plugin's page can set its own."
        case 1: "1 plugin has its own font or size."
        case let count: "\(count) plugins have their own font or size."
        }
    }
}

func settingsPoints(_ size: Double) -> String {
    "\(size.formatted()) pt"
}
