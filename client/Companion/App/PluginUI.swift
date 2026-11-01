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
    var inWorkspace: Bool { get }
    var needsUI: Bool { get }
}

extension CompanionPluginUI {
    var inWorkspace: Bool { false }
    var needsUI: Bool { false }
}

@MainActor
enum CompanionPlugins {
    static let all: [any CompanionPluginUI] = [AgentsPlugin(), BacklogPlugin(), DocsPlugin(), MorePlugin(), InboxPlugin()]
    static let registry = PluginRegistry(all)

    static func supported(by connection: ComputerConnection) -> [any CompanionPluginUI] {
        guard connection.state != .unauthorized else { return [] }
        let ids = Set(registry.supported(by: connection.hello).map(\.id))
        return all.filter { ids.contains($0.id) && (!$0.needsUI || connection.supportsUI) }
    }

    static func inWorkspace(_ connection: ComputerConnection) -> [any CompanionPluginUI] {
        supported(by: connection).filter(\.inWorkspace)
    }

    static func plugin(_ id: String) -> (any CompanionPluginUI)? {
        all.first { $0.id == id }
    }
}

struct WorkspaceTabs: View {
    let context: PluginContext

    var body: some View {
        let tabs = CompanionPlugins.inWorkspace(context.connection)
        if tabs.count > 1 {
            Picker("Section", selection: context.section) {
                ForEach(tabs, id: \.id) { plugin in
                    Text(plugin.title).tag(plugin.id)
                }
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            .padding(.bottom, 8)
        }
    }
}

struct WorkspaceChrome: ViewModifier {
    let context: PluginContext
    let title: String

    func body(content: Content) -> some View {
        if context.navigation.layout == .compact {
            content
                .safeAreaBar(edge: .top) { WorkspaceTabs(context: context) }
                .navigationTitle(title)
                .navigationSubtitle("\(context.connection.computer.name) · \(context.connection.presence)")
                .toolbar { MacSubtitle(text: context.model.tag(context.connection.computer.id)?.lead(context.connection.presence)) }
        } else {
            content
                .navigationTitle(title)
                .toolbarTitleDisplayMode(.inlineLarge)
        }
    }
}

extension View {
    func workspaceChrome(_ context: PluginContext, title: String) -> some View {
        modifier(WorkspaceChrome(context: context, title: title))
    }
}

extension PluginContext {
    @MainActor var section: Binding<String> {
        Binding(get: { navigation.sidebar?.plugin ?? "" }, set: { id in
            navigation.sidebar = SidebarItem(computer: connection.computer.id, plugin: id)
            navigation.detail = nil
        })
    }
}

struct WorkspaceMenu: ToolbarContent {
    let context: PluginContext

    var body: some ToolbarContent {
        let sections = context.navigation.layout == .compact ? [] : CompanionPlugins.inWorkspace(context.connection)
        if context.connection.workspaces.count > 1 || sections.count > 1 {
            ToolbarItem(placement: .primaryAction) {
                Menu("Workspace", systemImage: "folder") {
                    if sections.count > 1 {
                        Picker("Section", selection: context.section) {
                            ForEach(sections, id: \.id) { plugin in
                                Label(plugin.title, systemImage: plugin.systemImage).tag(plugin.id)
                            }
                        }
                    }
                    if context.connection.workspaces.count > 1 {
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
}
