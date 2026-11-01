import SwiftUI

import CompanionKit

struct MorePlugin: CompanionPluginUI {
    let id = MorePluginInfo.id
    let title = "More"
    let systemImage = "ellipsis.circle"
    let requiredCommands: [String] = []
    let inWorkspace = true
    let needsUI = true

    @MainActor
    func makeList(context: PluginContext) -> AnyView {
        AnyView(MoreListView(context: context))
    }

    @MainActor
    func makeDetail(context: PluginContext, selection: String?) -> AnyView {
        if context.connection.state == .unauthorized {
            return AnyView(ConnectionUnavailableView(connection: context.connection))
        }
        guard let selection, let workspace = context.navigation.workspace(for: context.connection) else {
            return AnyView(ContentUnavailableView("No Section Selected", systemImage: "ellipsis.circle", description: Text("Pick Cron, Processes or another section to open it.")))
        }
        return AnyView(SectionScreen(connection: context.connection, workspace: workspace.id, section: selection).id("\(workspace.id)/\(selection)"))
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        0
    }
}

struct MoreListView: View {
    let context: PluginContext

    var body: some View {
        let workspace = context.navigation.workspace(for: context.connection)
        Group {
            if let workspace {
                MoreSectionsList(context: context, ui: context.connection.workspaceUI(workspace.id))
                    .id(workspace.id)
            } else if context.connection.state == .online {
                ContentUnavailableView("No Workspaces", systemImage: "folder", description: Text("Open a workspace in Clubmate on \(context.connection.computer.name)."))
            } else {
                ConnectionUnavailableView(connection: context.connection)
            }
        }
        .workspaceChrome(context, title: workspace?.name ?? "More")
        .toolbar {
            WorkspaceMenu(context: context)
        }
    }
}

private struct MoreSectionsList: View {
    let context: PluginContext
    @State private var ui: WorkspaceUI
    @State private var owner = UUID()

    init(context: PluginContext, ui: WorkspaceUI) {
        self.context = context
        _ui = State(initialValue: ui)
    }

    var body: some View {
        let sections = PhoneSections.more(ui.sections)
        List(selection: Binding(get: { context.navigation.detail }, set: { id in
            context.navigation.detail = id
            if id != nil { context.navigation.preferredColumn = .detail }
        })) {
            if context.connection.state != .online {
                Section {
                    ConnectionBanner(connection: context.connection)
                }
            }
            Section {
                ForEach(sections, id: \.id) { section in
                    HStack(spacing: 12) {
                        RowIcon(systemImage: section.icon)
                        Text(section.title)
                            .lineLimit(1)
                    }
                    .frame(minHeight: 44)
                    .tag(section.id)
                }
            }
        }
        .listStyle(.insetGrouped)
        .overlay {
            if ui.sections.isEmpty, let failure = ui.failure {
                ContentUnavailableView("Can't Load Sections", systemImage: "exclamationmark.triangle", description: Text(failure))
            } else if ui.sections.isEmpty {
                ProgressView()
            } else if sections.isEmpty {
                ContentUnavailableView("Nothing Else Here", systemImage: "ellipsis.circle", description: Text("Plugins with their own screens on the Mac show here."))
            }
        }
        .onAppear { ui.show([], owner: owner) }
        .onDisappear { ui.hide(owner: owner) }
    }
}
