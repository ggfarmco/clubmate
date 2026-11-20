import SwiftUI

import CompanionKit

struct PluginContext {
    let model: CompanionModel
    let connection: ComputerConnection
    let navigation: AppNavigation
    var workspace: String? = nil

    @MainActor var shownWorkspace: Workspace? {
        workspace.flatMap { id in connection.workspaces.first { $0.id == id } } ?? navigation.workspace(for: connection)
    }
}

protocol CompanionPluginUI: CompanionPlugin {
    var tab: CompanionTab? { get }
    @MainActor func makeRoot(context: PluginContext) -> AnyView
    @MainActor func makeDestination(_ destination: Destination, context: PluginContext) -> AnyView?
    @MainActor func makeEmptyDetail(context: PluginContext) -> AnyView
    @MainActor func badge(context: PluginContext) -> Int
    var needsUI: Bool { get }
}

extension CompanionPluginUI {
    var tab: CompanionTab? { nil }
    var needsUI: Bool { false }
}

@MainActor
enum CompanionPlugins {
    static let all: [any CompanionPluginUI] = [AgentsPlugin(), BacklogPlugin(), WikiPlugin(), InboxPlugin(), MorePlugin()]
    static let registry = PluginRegistry(all)

    static func supported(by connection: ComputerConnection) -> [any CompanionPluginUI] {
        guard connection.state != .unauthorized else { return [] }
        let ids = Set(registry.supported(by: connection.hello).map(\.id))
        return all.filter { ids.contains($0.id) && (!$0.needsUI || connection.supportsUI) }
    }

    static func liveTabs(_ connection: ComputerConnection?, ui: WorkspaceUI?) -> [CompanionTab]? {
        guard let connection, connection.state == .unauthorized || connection.hello != nil && (!connection.supportsUI || ui?.sectionsLoaded == true) else { return nil }
        return tabs(connection, sections: ui?.sections ?? [])
    }

    static func tabs(_ connection: ComputerConnection?, sections: [SectionInfo]) -> [CompanionTab] {
        let supported = Set(connection.map(supported(by:))?.compactMap(\.tab) ?? [])
        let tabs = CompanionTab.shown(hasWiki: sections.contains { $0.id == WikiPluginInfo.id }).filter(supported.contains)
        return tabs.isEmpty ? [.agents] : tabs
    }

    static func plugin(_ id: String) -> (any CompanionPluginUI)? {
        all.first { $0.id == id }
    }

    static func plugin(for tab: CompanionTab) -> (any CompanionPluginUI)? {
        all.first { $0.tab == tab }
    }

    static func destination(_ destination: Destination, tab: CompanionTab, context: PluginContext) -> AnyView? {
        if case .doc(let path, let anchor) = destination, let workspace = context.shownWorkspace {
            return AnyView(DocPageView(connection: context.connection, workspace: workspace.id, path: path, anchor: anchor).readingMode().id(destination))
        }
        let order = all.filter { $0.tab == tab } + all.filter { $0.tab != tab }
        guard let page = order.lazy.compactMap({ $0.makeDestination(destination, context: context) }).first else { return nil }
        return destination.hasOwnMic ? page : AnyView(page.modifier(PageVoiceSlot(context: context)))
    }
}

struct ListHeader: View {
    let context: PluginContext

    var body: some View {
        if context.showsUsage {
            Section {
            } header: {
                UsageHeaderLine(computer: context.connection.computer.id)
                    .frame(maxWidth: .infinity, alignment: .trailing)
                    .textCase(nil)
                    .padding(.bottom, 8)
            }
            .listSectionSpacing(0)
        }
    }
}

struct CapsuleSlot: ToolbarContent {
    let context: PluginContext

    var body: some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            WorkspaceCapsule(context: context)
        }
        .sharedBackgroundVisibility(.hidden)
    }
}

struct InboxSlot: ToolbarContent {
    let context: PluginContext

    var body: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            InboxTray(context: context)
        }
    }
}

struct VoiceSlot: ToolbarContent {
    let context: PluginContext

    var body: some ToolbarContent {
        if context.navigation.voiceTarget(context.model) != nil {
            ToolbarItem(placement: .topBarTrailing) {
                VoiceBarButton()
            }
        }
    }
}

struct PageVoiceKey: PreferenceKey {
    static let defaultValue = false

    static func reduce(value: inout Bool, nextValue: () -> Bool) {
        value = value || nextValue()
    }
}

struct PageVoiceItem: ToolbarContent {
    @Environment(AppNavigation.self) private var navigation: AppNavigation?
    @Environment(CompanionModel.self) private var model: CompanionModel?

    var body: some ToolbarContent {
        if let navigation, let model, navigation.voiceTarget(model) != nil {
            ToolbarItem(placement: .topBarTrailing) {
                VoiceBarButton()
            }
        }
    }
}

private struct PageVoiceSlot: ViewModifier {
    let context: PluginContext
    @State private var owned = false

    func body(content: Content) -> some View {
        content
            .onPreferenceChange(PageVoiceKey.self) { owned = $0 }
            .toolbar {
                if !owned {
                    VoiceSlot(context: context)
                }
            }
    }
}

struct TabRootChrome<Create: ToolbarContent>: ViewModifier {
    let context: PluginContext
    let title: String
    let create: Create

    func body(content: Content) -> some View {
        let bar = content
            .scrollEdgeEffectStyle(.soft, for: .top)
            .navigationTitle(title)
            .toolbar {
                CapsuleSlot(context: context)
                InboxSlot(context: context)
                VoiceSlot(context: context)
                create
            }
        if context.navigation.layout == .compact {
            bar.toolbarTitleDisplayMode(.large)
        } else {
            bar.toolbarTitleDisplayMode(.inline).toolbar(removing: .title)
        }
    }
}

extension View {
    func tabRootChrome(_ context: PluginContext, title: String) -> some View {
        modifier(TabRootChrome(context: context, title: title, create: ToolbarItemGroup(placement: .topBarTrailing) {}))
    }

    func tabRootChrome<Create: ToolbarContent>(_ context: PluginContext, title: String, @ToolbarContentBuilder create: () -> Create) -> some View {
        modifier(TabRootChrome(context: context, title: title, create: create()))
    }
}

extension PluginContext {
    @MainActor var showsUsage: Bool {
        model.claudeAccounts.line(for: connection.computer.id, now: .now) != nil
    }
}
