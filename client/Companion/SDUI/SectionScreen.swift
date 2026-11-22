import SwiftUI

import CompanionKit

struct SectionScreen: View {
    let section: String
    let item: String?
    let tabRoot: Bool
    let connection: ComputerConnection
    @Environment(AppNavigation.self) private var navigation
    @Environment(\.ownerTab) private var ownerTab
    @Environment(\.tabShown) private var tabShown
    @State private var ui: WorkspaceUI
    @State private var owner = UUID()
    @State private var visible = false

    init(connection: ComputerConnection, workspace: String, section: String, item: String? = nil, tabRoot: Bool = false) {
        self.section = section
        self.item = item
        self.tabRoot = tabRoot
        self.connection = connection
        _ui = State(initialValue: connection.workspaceUI(workspace))
    }

    private var sidebar: String { "\(section).sidebar" }
    private var main: String { "\(section).main" }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color(.systemGroupedBackground))
            .navigationTitle(unless: tabRoot, title)
            .onAppear { visible = true }
            .onDisappear { visible = false }
            .onChange(of: ui.sections) { _, sections in
                let tab = ownerTab ?? navigation.tab
                let path = navigation.path(tab)
                if path.last?.destination == .section(id: section, item: nil), !sections.contains(where: { $0.id == section }) { navigation.setPath(Array(path.dropLast()), tab: tab) }
            }
            .task(id: ui.view(main) != nil && ui.view(sidebar) != nil && tabShown) { await openItem() }
            .modifier(TabShownWatch(ui: ui, slots: [sidebar, main], owner: owner))
            .task(id: "\(visible && tabShown)|\(PhoneSections.selected(in: ui.view(sidebar)) ?? "")") { await follow() }
    }

    @ViewBuilder
    private var content: some View {
        if item != nil {
            SectionSlot(ui: ui, slot: main, connection: connection)
        } else if tabRoot {
            SectionSlot(ui: ui, slot: sidebar, connection: connection, open: opened, list: true)
                .overlay {
                    if ui.view(sidebar) == nil, let failure = ui.failure {
                        ContentUnavailableView("Can't Load \(title)", systemImage: "exclamationmark.triangle", description: Text(failure))
                    }
                }
        } else if let root = PhoneSections.root(section, sidebar: ui.view(sidebar), main: ui.view(main)) {
            SectionSlot(ui: ui, slot: root, connection: connection, open: root == sidebar ? opened : { _ in })
        } else if let failure = ui.failure {
            ContentUnavailableView("Can't Load \(title)", systemImage: "exclamationmark.triangle", description: Text(failure))
        } else {
            ProgressView()
        }
    }

    private func opened(_ id: String) {
        guard section == WikiPluginInfo.id else { return navigation.push(.section(id: section, item: id)) }
        let page = Destination.wikiPage(id: id, path: "", title: ui.view(sidebar).flatMap { Self.label(of: id, in: $0.tree) } ?? id)
        if navigation.layout == .compact {
            navigation.push(page)
        } else {
            navigation.setPath([StackEntry(page)], tab: .wiki)
        }
    }

    private func openItem() async {
        let tab = ownerTab ?? navigation.tab
        let path = navigation.path(tab)
        guard tabShown, item == nil, let item = navigation.sectionItem, path.last?.destination == .section(id: section, item: nil), PhoneSections.root(section, sidebar: ui.view(sidebar), main: ui.view(main)) == sidebar,
              ui.view(sidebar).flatMap({ PhoneSections.list(in: $0.tree) }) != nil, ui.view(main) != nil else { return }
        navigation.sectionItem = nil
        navigation.setPath(path + [StackEntry(.section(id: section, item: item))], tab: tab)
    }

    private func follow() async {
        guard visible, tabShown, let item else { return }
        await ui.select(item, in: sidebar)
    }

    private static func label(of id: String, in node: SDUINode) -> String? {
        func find(_ items: [SDUITreeItem]) -> String? {
            items.lazy.compactMap { $0.target == id ? $0.item.label : find($0.children) }.first
        }
        return PhoneSections.list(in: node).flatMap { find($0.treeItems()) }
    }

    private var title: String {
        ui.sections.first { $0.id == section }?.title ?? section
    }
}

struct SectionSlot: View {
    let ui: WorkspaceUI
    let slot: String
    var tree: SDUINode?
    let connection: ComputerConnection
    var open: (String) -> Void = { _ in }
    var list = false
    @Environment(AppNavigation.self) private var navigation
    @Environment(CompanionModel.self) private var model
    @State private var error: String?
    @State private var liveFocus = SDUILiveFocus()

    var body: some View {
        let shown = tree ?? ui.view(slot)?.tree
        Group {
            if list {
                SidebarList(node: shown ?? SDUINode(type: "stack"), context: context, connection: connection)
                    .overlay {
                        if shown == nil, ui.failure == nil { ProgressView() }
                    }
            } else if let shown {
                SDUINodeView(node: shown, context: context)
            } else {
                ProgressView()
            }
        }
        .environment(\.imageStore, connection.supports(AgentsStore.imageCommand) ? model.agents(computer: connection.computer.id, workspace: ui.workspace) : nil)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .environment(liveFocus)
        .alert("Couldn't Do That", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
            Button("OK") { error = nil }
        } message: {
            Text(error ?? "")
        }
    }

    private var context: SDUIContext {
        SDUIContext(slot: slot, send: { nodeId, action, value in
            do {
                let result = try await ui.send(slot: slot, nodeId: nodeId, action: action, value: value)
                if let session = result["open"]?["session"]?.stringValue {
                    navigation.push(.session(session), from: navigation.origin(), in: WorkspaceKey(computer: connection.computer.id, workspace: ui.workspace))
                }
            } catch {
                self.error = (error as? RemoteError)?.message ?? error.localizedDescription
            }
        }, open: open, openLink: linkOpener)
    }

    private var linkOpener: ((String) -> Void)? {
        guard connection.supports(DocsCommand.read) || connection.supports(WikiCommand.locate) else { return nil }
        return { [navigation] link in
            let (path, anchor) = DocsStore.split(link)
            navigation.openLink(path, anchor: anchor, from: navigation.origin())
        }
    }
}

extension View {
    @ViewBuilder
    func navigationTitle(unless root: Bool, _ title: String) -> some View {
        if root {
            self
        } else {
            navigationTitle(title)
        }
    }
}
