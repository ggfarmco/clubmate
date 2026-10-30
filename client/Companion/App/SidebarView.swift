import SwiftUI

import CompanionKit

struct SidebarView: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @State private var removing: Computer?

    var body: some View {
        List(selection: Binding(get: { navigation.sidebar }, set: { item in
            guard item != navigation.sidebar else { return }
            navigation.sidebar = item
            navigation.detail = nil
            navigation.preferredColumn = .content
        })) {
            ForEach(model.computers) { computer in
                if let connection = model.connection(computer.id) {
                    ComputerSection(connection: connection, removing: $removing)
                }
            }
        }
        .listStyle(.sidebar)
        .navigationTitle("Macs")
        .overlay {
            if model.computers.isEmpty {
                WelcomeView()
            }
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("Pair a Mac", systemImage: "plus") { navigation.showingAddComputer = true }
            }
        }
        .refreshable { model.reconnectAll() }
        .confirmationDialog("Remove \(removing?.name ?? "")?", isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }), titleVisibility: .visible) {
            Button("Remove", role: .destructive) {
                if let removing { try? model.remove(removing.id) }
                removing = nil
            }
        } message: {
            Text("This phone forgets the Mac and its access token. Revoke the device in Clubmate on the Mac to cut access there too.")
        }
    }
}

struct ComputerSection: View {
    let connection: ComputerConnection
    @Binding var removing: Computer?
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        Section {
            ForEach(CompanionPlugins.supported(by: connection), id: \.id) { plugin in
                let context = PluginContext(model: model, connection: connection, navigation: navigation)
                let badge = plugin.badge(context: context)
                Label(plugin.title, systemImage: plugin.systemImage)
                    .badge(badge)
                    .tag(SidebarItem(computer: connection.computer.id, plugin: plugin.id))
            }
            if connection.hello == nil || connection.state == .unauthorized {
                StatusRow(connection: connection)
            }
        } header: {
            HStack(spacing: 8) {
                Image(systemName: "desktopcomputer")
                Text(connection.computer.name)
                Spacer()
                StateDot(state: connection.state)
            }
            .contextMenu { actions }
        }
    }

    @ViewBuilder
    private var actions: some View {
        Button("Reconnect", systemImage: "arrow.clockwise") { connection.reconnectNow() }
            .disabled(connection.state == .online || connection.state == .unauthorized)
        Button("Remove…", systemImage: "trash", role: .destructive) { removing = connection.computer }
    }
}

struct StatusRow: View {
    let connection: ComputerConnection
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        switch connection.state {
        case .unauthorized:
            Button {
                navigation.showingAddComputer = true
            } label: {
                Label {
                    VStack(alignment: .leading) {
                        Text("Access revoked")
                        Text("Pair again").font(.footnote).foregroundStyle(.tint)
                    }
                } icon: {
                    Image(systemName: "lock.slash").foregroundStyle(.red)
                }
            }
        case .offline(let reason):
            Button {
                connection.reconnectNow()
            } label: {
                Label {
                    VStack(alignment: .leading) {
                        Text("Offline")
                        Text(reason ?? "Tap to retry").font(.footnote).foregroundStyle(.secondary).lineLimit(2)
                    }
                } icon: {
                    Image(systemName: "wifi.slash").foregroundStyle(.secondary)
                }
            }
        case .connecting, .online:
            Label {
                Text("Connecting…").foregroundStyle(.secondary)
            } icon: {
                ProgressView()
            }
        }
    }
}

struct StateDot: View {
    let state: ComputerConnection.State

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: 8, height: 8)
            .accessibilityLabel(state.label)
    }

    private var color: Color {
        switch state {
        case .online: .green
        case .connecting: .yellow
        case .offline: .gray
        case .unauthorized: .red
        }
    }
}
