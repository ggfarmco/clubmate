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
            Section("nvim") {
                Toggle("nvim navigation keys", isOn: navigationKeys)
            }
        }
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

    private func points(_ size: Double) -> String {
        "\(size.formatted()) pt"
    }
}
