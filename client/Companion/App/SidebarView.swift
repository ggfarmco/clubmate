import SwiftUI

import CompanionKit

enum MacsRow: Hashable {
    case workspace(computer: String, workspace: String)
    case plugin(SidebarItem)
}

struct SidebarView: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @State private var removing: Computer?

    var body: some View {
        let needs = model.needsYou()
        List(selection: Binding(get: { selection }, set: select)) {
            if !needs.isEmpty {
                Section {
                    ForEach(needs) { item in
                        NeedsYouRow(item: item)
                    }
                } header: {
                    HStack {
                        Text("Needs you")
                        Spacer()
                        Text("\(needs.count)")
                    }
                }
            }
            ForEach(model.computers) { computer in
                if let connection = model.connection(computer.id) {
                    ComputerSection(connection: connection, removing: $removing)
                }
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle("Macs")
        .overlay {
            if model.computers.isEmpty {
                NoMacsView()
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

    private var selection: MacsRow? {
        guard let item = navigation.sidebar else { return nil }
        guard CompanionPlugins.plugin(item.plugin)?.inWorkspace == true, let connection = model.connection(item.computer), let workspace = navigation.workspace(for: connection) else { return .plugin(item) }
        return .workspace(computer: item.computer, workspace: workspace.id)
    }

    private func select(_ row: MacsRow?) {
        guard row != selection else { return }
        switch row {
        case .workspace(let computer, let workspace):
            navigation.workspaces[computer] = workspace
            navigation.open(computer: computer, plugin: AgentsPluginInfo.id)
        case .plugin(let item):
            navigation.open(computer: item.computer, plugin: item.plugin)
        case nil:
            navigation.sidebar = nil
            navigation.detail = nil
        }
    }
}

struct NeedsYouRow: View {
    let item: NeedsYouItem
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        Button {
            navigation.workspaces[item.computer] = item.workspace.id
            navigation.open(computer: item.computer, plugin: AgentsPluginInfo.id, detail: item.session.id)
        } label: {
            TimelineView(.periodic(from: .now, by: 30)) { timeline in
                HStack(spacing: 12) {
                    SessionGlyphView(glyph: item.session.glyph)
                    VStack(alignment: .leading, spacing: 2) {
                        title
                            .font(.body.weight(.semibold))
                            .lineLimit(1)
                        Text(item.subtitle)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    Text(ShortAge.since(item.at, now: timeline.date))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .frame(minHeight: 52)
                .contentShape(.rect)
            }
        }
        .buttonStyle(.plain)
    }

    private var title: Text {
        guard let command = item.command else { return Text(item.title) }
        return Text("Run \(Text(command).monospaced())?")
    }
}

struct ComputerSection: View {
    let connection: ComputerConnection
    @Binding var removing: Computer?
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        Section {
            let plugins = CompanionPlugins.supported(by: connection)
            let workspaces = plugins.contains { $0.id == AgentsPluginInfo.id } ? connection.workspaces : []
            ForEach(workspaces) { workspace in
                if let store = model.agents(computer: connection.computer.id, workspace: workspace.id) {
                    WorkspaceRow(connection: connection, workspace: workspace, store: store)
                        .tag(MacsRow.workspace(computer: connection.computer.id, workspace: workspace.id))
                }
            }
            ForEach(plugins.filter { !(workspaces.count > 0 && $0.inWorkspace) }, id: \.id) { plugin in
                PluginRow(plugin: plugin, context: PluginContext(model: model, connection: connection, navigation: navigation))
                    .tag(MacsRow.plugin(SidebarItem(computer: connection.computer.id, plugin: plugin.id)))
            }
            if connection.hello == nil || connection.state == .unauthorized {
                StatusRow(connection: connection)
            }
        } header: {
            HStack(spacing: 8) {
                Image(systemName: "desktopcomputer")
                Text(connection.computer.name)
                Spacer()
                Text(connection.presence)
                    .font(.subheadline)
                    .textCase(nil)
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

struct WorkspaceRow: View {
    let connection: ComputerConnection
    let workspace: Workspace
    let store: AgentsStore

    var body: some View {
        let activity = WorkspaceActivity(store.sessions)
        HStack(spacing: 12) {
            RowIcon(systemImage: "folder")
            VStack(alignment: .leading, spacing: 2) {
                Text(workspace.name)
                    .lineLimit(1)
                Group {
                    if connection.state != .online {
                        Text(activity.offlineText(asOf: connection.lastSeen))
                    } else if store.loaded {
                        activityText(activity)
                    }
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            }
        }
        .frame(minHeight: 52)
        .opacity(connection.state == .online ? 1 : 0.5)
        .task(id: connection.state == .online) {
            if connection.state == .online && !store.loaded { await store.refresh() }
        }
    }

    private func activityText(_ activity: WorkspaceActivity) -> Text {
        let waiting = Text(activity.waitingText ?? "").foregroundStyle(Color.warningInk)
        let separator = Text(activity.waitingText != nil && activity.restText != nil ? " · " : "")
        return Text("\(waiting)\(separator)\(activity.restText ?? "")")
    }
}

struct PluginRow: View {
    let plugin: any CompanionPluginUI
    let context: PluginContext

    var body: some View {
        let badge = plugin.badge(context: context)
        HStack(spacing: 12) {
            RowIcon(systemImage: plugin.id == InboxPluginInfo.id ? "tray" : plugin.systemImage)
            Text(plugin.title)
        }
        .frame(minHeight: 44)
        .badge(badge > 0 ? Text(plugin.id == InboxPluginInfo.id ? "\(badge) unread" : "\(badge)") : nil)
        .opacity(context.connection.state == .online ? 1 : 0.5)
    }
}

struct RowIcon: View {
    let systemImage: String

    var body: some View {
        Image(systemName: systemImage)
            .font(.system(size: 14, weight: .medium))
            .foregroundStyle(.secondary)
            .frame(width: 28, height: 28)
            .background(Color(.tertiarySystemFill), in: .circle)
    }
}

struct NoMacsView: View {
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        ContentUnavailableView {
            Label("No Macs yet", systemImage: "desktopcomputer")
        } description: {
            Text("Open Clubmate on your Mac, then Settings › Devices › Pair Device…")
        } actions: {
            Button("Scan pairing code", systemImage: "qrcode.viewfinder") { navigation.showingAddComputer = true }
                .buttonStyle(.glassProminent)
                .controlSize(.large)
        }
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
                        Text(reason ?? "Tap to retry").font(.footnote).lineLimit(2)
                    }
                } icon: {
                    Image(systemName: "wifi.slash")
                }
                .foregroundStyle(Color.secondary)
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
