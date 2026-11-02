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
            Divider()
            detail
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .frame(width: 720, height: 560)
        .onAppear { if page == .general { page = .initial } }
        .task(id: SettingsFollowKey(client: app.supervisor.client.map(ObjectIdentifier.init), active: appearsActive)) {
            await plugins.follow(app.supervisor.client, active: appearsActive)
        }
    }

    @ViewBuilder private var detail: some View {
        switch page {
        case .general:
            GeneralSettings(settings: settings)
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

    var body: some View {
        Form {
            Section("Appearance") {
                Stepper(value: $settings.interfaceFontSize, in: AppSettings.interfaceFontRange, step: 1) {
                    LabeledContent {
                        Text(settingsPoints(settings.interfaceFontSize))
                    } label: {
                        Text("Interface font size")
                        Text("Sidebars, lists and forms. A pane you zoomed with ⌘+ keeps its own size.")
                    }
                }
                Picker("Sidebar side", selection: $settings.sidebarSide) {
                    ForEach(SidebarSide.allCases, id: \.self) { side in
                        Text(side.rawValue.capitalized).tag(side)
                    }
                }
                .pickerStyle(.segmented)
                LabeledContent {
                    Button("Reset All") { settings.resetPaneFonts() }
                        .disabled(settings.zoomedPaneCount == 0)
                } label: {
                    Text("Zoomed panes")
                    Text(zoomed)
                }
            }
        }
        .formStyle(.grouped)
    }

    private var zoomed: String {
        switch settings.zoomedPaneCount {
        case 0: "No pane has its own size."
        case 1: "1 pane has its own size."
        case let count: "\(count) panes have their own size."
        }
    }
}

func settingsPoints(_ size: Double) -> String {
    "\(size.formatted()) pt"
}
