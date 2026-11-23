import SwiftUI

import CompanionKit

struct WorkspaceCapsule: View {
    let context: PluginContext

    var body: some View {
        let model = context.model
        let navigation = context.navigation
        let now = Date()
        Menu {
            ControlGroup {
                Button("Mac Details", systemImage: "info.circle") {
                    navigation.push(.macDetails(computer: context.connection.computer.id))
                }
                Button("Pair a Mac", systemImage: "qrcode.viewfinder") { navigation.showingAddComputer = true }
            }
            if otherMacs.count > 0 {
                Section("Other Macs") {
                    ForEach(otherMacs, id: \.computer.id) { connection in
                        Menu {
                            workspaceRows(connection, now: now)
                            Button("Mac Details", systemImage: "info.circle") {
                                navigation.push(.macDetails(computer: connection.computer.id))
                            }
                        } label: {
                            Label(connection.computer.name, systemImage: connection.computer.symbol)
                            if let subtitle = macSubtitle(connection, now: now) {
                                Text(subtitle)
                            }
                        }
                    }
                }
            }
            Section(context.connection.computer.name + " · " + context.connection.shortPresence) {
                workspaceRows(context.connection, now: now)
            }
        } label: {
            HStack(spacing: 6) {
                Image(systemName: online ? "circle.fill" : "circle")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(model.tag(context.connection.computer.id)?.color ?? Color.accentColor)
                Text(navigation.workspace(for: context.connection)?.name ?? context.connection.computer.name)
                    .lineLimit(1)
                Image(systemName: "chevron.down")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.captionInk)
            }
        }
        .buttonStyle(.glass)
        .accessibilityIdentifier("workspace.capsule")
        .accessibilityValue(online ? "" : context.connection.shortPresence)
        .task(id: stores) {
            for key in stores {
                guard let store = context.model.agents(computer: key.computer, workspace: key.workspace, settled: false), !store.fetched else { continue }
                await store.refresh()
            }
        }
    }

    private var online: Bool {
        context.connection.state == .online
    }

    private var otherMacs: [ComputerConnection] {
        context.model.computers.compactMap { context.model.connection($0.id) }.filter { $0.computer.id != context.connection.computer.id }
    }

    private func summary(_ workspace: Workspace, in connection: ComputerConnection) -> WorkspaceSummary {
        WorkspaceSummary.summary(of: workspace, sendsActivity: WorkspaceSummary.sendsActivity(connection.workspaces)) {
            context.model.waitingCount(computer: connection.computer.id, workspace: $0.id)
        }
    }

    private func macSubtitle(_ connection: ComputerConnection, now: Date) -> String? {
        let total = WorkspaceSummary.of(connection.workspaces) {
            context.model.waitingCount(computer: connection.computer.id, workspace: $0.id)
        }
        return WorkspaceSummary.macSubtitle(total, offline: connection.isOffline, lastSeen: connection.lastSeen, now: now)
    }

    @ViewBuilder
    private func workspaceRows(_ connection: ComputerConnection, now: Date) -> some View {
        ForEach(Workspace.homeFirst(connection.workspaces)) { workspace in
            let key = WorkspaceKey(computer: connection.computer.id, workspace: workspace.id)
            let state = summary(workspace, in: connection)
            Toggle(isOn: Binding {
                context.navigation.workspaceKey == key
            } set: { _ in
                context.navigation.switchWorkspace(computer: key.computer, workspace: key.workspace)
            }) {
                Label(WorkspaceSummary.rowName(workspace.name), systemImage: state.glyph(home: workspace.home == true))
                if let subtitle = state.subtitle(now: now, asOf: connection.lastSeen, offline: connection.isOffline) {
                    Text(subtitle)
                }
            }
        }
    }

    private var stores: [WorkspaceKey] {
        context.model.computers.flatMap { computer -> [WorkspaceKey] in
            guard let connection = context.model.connection(computer.id), connection.state == .online else { return [] }
            return connection.workspaces.map { WorkspaceKey(computer: computer.id, workspace: $0.id) }
        }
    }
}
