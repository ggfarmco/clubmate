import SwiftUI

import CompanionKit

struct SectionScreen: View {
    let section: String
    let connection: ComputerConnection
    @Environment(AppNavigation.self) private var navigation
    @Environment(CompanionModel.self) private var model
    @State private var ui: WorkspaceUI
    @State private var owner = UUID()
    @State private var path: [String] = []
    @State private var error: String?

    init(connection: ComputerConnection, workspace: String, section: String) {
        self.section = section
        self.connection = connection
        _ui = State(initialValue: connection.workspaceUI(workspace))
    }

    var body: some View {
        let sidebar = "\(section).sidebar"
        let main = "\(section).main"
        NavigationStack(path: $path) {
            Group {
                if let root = PhoneSections.root(section, sidebar: ui.view(sidebar), main: ui.view(main)) {
                    slot(root, open: root == sidebar ? { _ in path = [main] } : { _ in })
                } else if let failure = ui.failure {
                    ContentUnavailableView("Can't Load \(title)", systemImage: "exclamationmark.triangle", description: Text(failure))
                } else {
                    ProgressView()
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color(.systemGroupedBackground))
            .navigationTitle(title)
            .navigationDestination(for: String.self) { slot in
                self.slot(slot, open: { _ in })
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Color(.systemGroupedBackground))
                    .navigationTitle(title)
                    .navigationBarTitleDisplayMode(.inline)
            }
        }
        .alert("Couldn't Do That", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
            Button("OK") { error = nil }
        } message: {
            Text(error ?? "")
        }
        .onChange(of: ui.sections) { _, sections in
            if navigation.detail == section, !sections.contains(where: { $0.id == section }) { navigation.detail = nil }
        }
        .task(id: ui.view(main) != nil && ui.view(sidebar) != nil) { await openItem(sidebar: sidebar, main: main) }
        .onAppear { ui.show([sidebar, main], owner: owner) }
        .onDisappear { ui.hide(owner: owner) }
    }

    private func openItem(sidebar: String, main: String) async {
        guard let item = navigation.sectionItem, navigation.detail == section, PhoneSections.root(section, sidebar: ui.view(sidebar), main: ui.view(main)) == sidebar,
              let list = ui.view(sidebar).flatMap({ Self.list(in: $0.tree) }), ui.view(main) != nil else { return }
        navigation.sectionItem = nil
        _ = try? await ui.send(slot: sidebar, nodeId: list, action: "select", value: .string(item))
        path = [main]
    }

    private static func list(in node: SDUINode) -> String? {
        if node.type == "list" || node.type == "tree", let id = node.id { return id }
        return node.children.lazy.compactMap(list(in:)).first
    }

    private var docOpener: ((String) -> Void)? {
        guard connection.supports(DocsCommand.read) else { return nil }
        return { [navigation, connection, ui] path in navigation.openDoc(computer: connection.computer.id, workspace: ui.workspace, path: path) }
    }

    private var title: String {
        ui.sections.first { $0.id == section }?.title ?? section
    }

    @ViewBuilder
    private func slot(_ slot: String, open: @escaping (String) -> Void) -> some View {
        if let view = ui.view(slot) {
            SDUINodeView(node: view.tree, context: SDUIContext(slot: slot, send: { nodeId, action, value in
                do {
                    _ = try await ui.send(slot: slot, nodeId: nodeId, action: action, value: value)
                } catch {
                    self.error = (error as? RemoteError)?.message ?? error.localizedDescription
                }
            }, open: open, openDoc: docOpener))
            .environment(\.imageStore, connection.supports(AgentsStore.imageCommand) ? model.agents(computer: connection.computer.id, workspace: ui.workspace) : nil)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        } else {
            ProgressView()
        }
    }
}
