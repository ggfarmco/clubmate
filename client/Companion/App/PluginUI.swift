import SwiftUI

import CompanionKit

struct PluginContext {
    let model: CompanionModel
    let connection: ComputerConnection
    let navigation: AppNavigation
}

protocol CompanionPluginUI: CompanionPlugin {
    @MainActor func makeList(context: PluginContext) -> AnyView
    @MainActor func makeDetail(context: PluginContext, selection: String?) -> AnyView
    @MainActor func badge(context: PluginContext) -> Int
}

@MainActor
enum CompanionPlugins {
    static let all: [any CompanionPluginUI] = [AgentsPlugin(), BacklogPlugin(), InboxPlugin()]
    static let registry = PluginRegistry(all)

    static func supported(by connection: ComputerConnection) -> [any CompanionPluginUI] {
        guard connection.state != .unauthorized else { return [] }
        let ids = Set(registry.supported(by: connection.hello).map(\.id))
        return all.filter { ids.contains($0.id) }
    }

    static func plugin(_ id: String) -> (any CompanionPluginUI)? {
        all.first { $0.id == id }
    }
}

struct WorkspaceMenu: ToolbarContent {
    let context: PluginContext

    var body: some ToolbarContent {
        if context.connection.workspaces.count > 1 {
            ToolbarItem(placement: .primaryAction) {
                Menu("Workspace", systemImage: "folder") {
                    Picker("Workspace", selection: Binding(get: { context.navigation.workspace(for: context.connection)?.id ?? "" }, set: { id in
                        context.navigation.workspaces[context.connection.computer.id] = id
                        context.navigation.detail = nil
                    })) {
                        ForEach(context.connection.workspaces) { item in
                            Label(item.name, systemImage: "folder").tag(item.id)
                        }
                    }
                }
            }
        }
    }
}
