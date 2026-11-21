import SwiftUI

import CompanionKit

struct WorkspaceCapsule: View {
    let context: PluginContext

    var body: some View {
        let model = context.model
        let navigation = context.navigation
        let current = navigation.workspaceKey
        Menu {
            ForEach(model.computers) { computer in
                if let connection = model.connection(computer.id) {
                    Section(connection.computer.name + " · " + connection.shortPresence) {
                        ForEach(connection.workspaces) { workspace in
                            let key = WorkspaceKey(computer: computer.id, workspace: workspace.id)
                            let waiting = model.waitingCount(computer: computer.id, workspace: workspace.id)
                            Button {
                                navigation.switchWorkspace(computer: computer.id, workspace: workspace.id)
                            } label: {
                                Label(workspace.name, systemImage: key == current ? "checkmark" : "folder")
                                if waiting > 0 {
                                    Text("\(waiting) waiting")
                                }
                            }
                        }
                        Button("Mac Details", systemImage: "info.circle") {
                            navigation.push(.macDetails(computer: computer.id))
                        }
                    }
                }
            }
            Section {
                Button("Pair a Mac", systemImage: "qrcode.viewfinder") { navigation.showingAddComputer = true }
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
                guard let store = model.agents(computer: key.computer, workspace: key.workspace), !store.fetched else { continue }
                await store.refresh()
            }
        }
    }

    private var online: Bool {
        context.connection.state == .online
    }

    private var stores: [WorkspaceKey] {
        context.model.computers.flatMap { computer -> [WorkspaceKey] in
            guard let connection = context.model.connection(computer.id), connection.state == .online else { return [] }
            return connection.workspaces.map { WorkspaceKey(computer: computer.id, workspace: $0.id) }
        }
    }
}
