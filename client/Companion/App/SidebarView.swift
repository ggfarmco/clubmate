import SwiftUI

import CompanionKit

enum MacsRow: Hashable {
    case needsYou
    case workspace(computer: String, workspace: String)
    case plugin(SidebarItem)
}

struct SidebarView: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @State private var removing: Computer?

    var body: some View {
        let needs = model.needsYou()
        let selected = navigation.layout == .compact ? nil : selection
        List(selection: Binding(get: { selection }, set: select)) {
            QueueSection()
            if !needs.isEmpty, navigation.layout != .compact {
                Section {
                    NeedsYouSummaryRow(count: needs.count)
                        .tag(MacsRow.needsYou)
                        .selectedRow(selected == .needsYou)
                }
            } else if !needs.isEmpty {
                Section {
                    ForEach(needs) { item in
                        NeedsYouRow(item: item)
                    }
                } header: {
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Text("Needs you")
                            Spacer()
                            Text("\(needs.count)")
                        }
                        ForEach(model.offlineMacNames(needs), id: \.self) { name in
                            Text("\(name) is offline — answer when it's back.")
                                .font(.footnote)
                                .foregroundStyle(Color.captionInk)
                        }
                    }
                }
            }
            ForEach(model.computers) { computer in
                if let connection = model.connection(computer.id) {
                    ComputerSection(connection: connection, selection: selected, removing: $removing)
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
        if navigation.needsYouList { return .needsYou }
        guard let item = navigation.sidebar else { return nil }
        guard CompanionPlugins.plugin(item.plugin)?.inWorkspace == true, let connection = model.connection(item.computer), let workspace = navigation.workspace(for: connection) else { return .plugin(item) }
        return .workspace(computer: item.computer, workspace: workspace.id)
    }

    private func select(_ row: MacsRow?) {
        guard row != selection else { return }
        switch row {
        case .needsYou:
            navigation.showNeedsYou()
        case .workspace(let computer, let workspace):
            navigation.workspaces[computer] = workspace
            navigation.open(computer: computer, plugin: AgentsPluginInfo.id)
        case .plugin(let item):
            navigation.open(computer: item.computer, plugin: item.plugin)
        case nil:
            navigation.needsYouList = false
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
                    SessionGlyphView(glyph: item.session.glyph, muted: item.offline)
                    VStack(alignment: .leading, spacing: 2) {
                        title
                            .font(.body.weight(.semibold))
                            .lineLimit(1)
                        item.mac.lead(item.subtitle)
                            .font(.subheadline)
                            .foregroundStyle(Color.captionInk)
                            .lineLimit(1)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    Text(ShortAge.since(item.at, now: timeline.date))
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
                    RowChevron()
                }
                .frame(minHeight: 44)
                .contentShape(.rect)
            }
        }
        .buttonStyle(.plain)
        .listRowDensity()
        .opacity(item.offline ? 0.5 : 1)
        .accessibilityHint(item.offline ? "Read-only until the Mac is back" : "")
    }

    private var title: Text {
        guard let command = item.command else { return Text(item.title) }
        return Text("Run \(Text(command).monospaced())?")
    }
}

struct ComputerSection: View {
    let connection: ComputerConnection
    let selection: MacsRow?
    @Binding var removing: Computer?
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        Section {
            let plugins = CompanionPlugins.supported(by: connection)
            let workspaces = plugins.contains { $0.id == AgentsPluginInfo.id } ? connection.workspaces : []
            ForEach(workspaces) { workspace in
                if let store = model.agents(computer: connection.computer.id, workspace: workspace.id) {
                    let tag = MacsRow.workspace(computer: connection.computer.id, workspace: workspace.id)
                    WorkspaceRow(connection: connection, workspace: workspace, store: store, selected: selection == tag)
                        .tag(tag)
                        .selectedRow(selection == tag)
                }
            }
            ForEach(plugins.filter { !(workspaces.count > 0 && $0.inWorkspace) }, id: \.id) { plugin in
                let tag = MacsRow.plugin(SidebarItem(computer: connection.computer.id, plugin: plugin.id))
                PluginRow(plugin: plugin, context: PluginContext(model: model, connection: connection, navigation: navigation))
                    .tag(tag)
                    .selectedRow(selection == tag)
            }
            if connection.hello == nil || connection.state == .unauthorized {
                StatusRow(connection: connection)
            }
        } header: {
            MacSectionHeader(connection: connection, tinted: model.namesMacs, inline: navigation.layout != .compact) { navigation.showMac(connection.computer.id) }
                .contextMenu { actions }
        }
    }

    @ViewBuilder
    private var actions: some View {
        Button("Details", systemImage: "info.circle") { navigation.showMac(connection.computer.id) }
        Button("Reconnect", systemImage: "arrow.clockwise") { connection.reconnectNow() }
            .disabled(connection.state == .online || connection.state == .unauthorized)
        Button("Remove…", systemImage: "trash", role: .destructive) { removing = connection.computer }
    }
}

struct WorkspaceRow: View {
    let connection: ComputerConnection
    let workspace: Workspace
    let store: AgentsStore
    var selected = false
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        let activity = WorkspaceActivity(store.sessions)
        Group {
            if navigation.layout == .compact {
                twoLines(activity)
            } else {
                oneLine(activity)
            }
        }
        .listRowDensity()
        .opacity(connection.state == .online ? 1 : 0.5)
        .task(id: connection.state == .online) {
            if connection.state == .online && !store.loaded { await store.refresh() }
        }
    }

    private func oneLine(_ activity: WorkspaceActivity) -> some View {
        HStack(spacing: 12) {
            RowIcon(systemImage: "folder", tint: selected ? .accentColor : nil)
            Text(workspace.name)
                .fontWeight(selected ? .semibold : .regular)
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
            if activity.waiting > 0 {
                Text("\(activity.waiting)")
                    .monospacedDigit()
                    .foregroundStyle(Color.warningInk)
                    .accessibilityLabel(activity.waitingText ?? "")
            }
        }
        .frame(minHeight: 44)
    }

    private func twoLines(_ activity: WorkspaceActivity) -> some View {
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
                .foregroundStyle(Color.captionInk)
                .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            RowChevron()
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
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        let badge = plugin.badge(context: context)
        HStack(spacing: 12) {
            RowIcon(systemImage: plugin.id == InboxPluginInfo.id ? "tray" : plugin.systemImage)
            Text(plugin.title)
                .frame(maxWidth: .infinity, alignment: .leading)
            if badge > 0 {
                Text(plugin.id == InboxPluginInfo.id && navigation.layout == .compact ? "\(badge) unread" : "\(badge)")
                    .monospacedDigit()
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
                    .fixedSize()
            }
            RowChevron()
        }
        .listRowDensity()
        .opacity(context.connection.state == .online ? 1 : 0.5)
    }
}

struct RowIcon: View {
    let systemImage: String
    var tint: Color?

    var body: some View {
        Image(systemName: systemImage)
            .font(.system(size: 14, weight: .medium))
            .foregroundStyle(tint ?? Color.captionInk)
            .frame(width: 28, height: 28)
            .background(Color(.tertiarySystemFill), in: .circle)
    }
}

struct RowChevron: View {
    var always = false
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        if always || navigation.layout == .compact {
            Image(systemName: "chevron.right")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.tertiary)
                .accessibilityHidden(true)
        }
    }
}

struct NeedsYouSummaryRow: View {
    let count: Int

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "exclamationmark")
                .font(.system(size: 14, weight: .bold))
                .foregroundStyle(Color.onWarning)
                .frame(width: 28, height: 28)
                .background(Color.warning, in: .circle)
            Text("Needs you")
                .frame(maxWidth: .infinity, alignment: .leading)
            Text("\(count)")
                .monospacedDigit()
                .foregroundStyle(Color.warningInk)
        }
        .frame(minHeight: 44)
        .listRowDensity()
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("macs.needsYou")
    }
}

struct NeedsYouListView: View {
    @Environment(CompanionModel.self) private var model

    var body: some View {
        let needs = model.needsYou()
        List {
            Section {
                ForEach(needs) { item in
                    NeedsYouRow(item: item)
                }
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(model.offlineMacNames(needs), id: \.self) { name in
                        Text("\(name) is offline — answer when it's back.")
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle("Needs you")
        .overlay {
            if needs.isEmpty {
                ContentUnavailableView("Nothing needs you", systemImage: "checkmark.circle", description: Text("Requests from your Macs show up here."))
            }
        }
    }

}

extension CompanionModel {
    func offlineMacNames(_ needs: [NeedsYouItem]) -> [String] {
        var seen: Set<String> = []
        return needs.filter { $0.offline && seen.insert($0.computer).inserted }.compactMap { connection($0.computer)?.computer.name }
    }
}

extension View {
    func selectedRow(_ selected: Bool) -> some View {
        listRowBackground(selected ? Color.selectedRow : nil)
            .foregroundStyle(selected ? AnyShapeStyle(Color(.label)) : AnyShapeStyle(.foreground))
    }

    func listRowDensity(_ vertical: CGFloat = 11) -> some View {
        listRowInsets(.vertical, vertical)
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
                .foregroundStyle(Color.captionInk)
            }
        case .connecting, .online:
            Label {
                Text("Connecting…").foregroundStyle(Color.captionInk)
            } icon: {
                ProgressView()
            }
        }
    }
}
