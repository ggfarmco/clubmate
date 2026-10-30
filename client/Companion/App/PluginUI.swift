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
    static let all: [any CompanionPluginUI] = [AgentsPlugin(), InboxPlugin()]
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
