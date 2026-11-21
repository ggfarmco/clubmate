import SwiftUI

import ClubmateDocument
import CompanionKit

struct WikiPlugin: CompanionPluginUI {
    let id = WikiPluginInfo.id
    let title = "Wiki"
    let systemImage = "book"
    let requiredCommands: [String] = []
    let tab: CompanionTab? = .wiki
    let needsUI = true

    @MainActor
    func makeRoot(context: PluginContext) -> AnyView {
        AnyView(WikiRoot(context: context))
    }

    @MainActor
    func makeEmptyDetail(context: PluginContext) -> AnyView {
        if context.connection.state == .unauthorized {
            return AnyView(ConnectionUnavailableView(connection: context.connection))
        }
        return AnyView(WikiHomeDetail(context: context))
    }

    @MainActor
    func makeDestination(_ destination: Destination, context: PluginContext) -> AnyView? {
        guard case .wikiPage(let id, let path, let title) = destination else { return nil }
        guard context.connection.state != .unauthorized, let workspace = context.navigation.workspace(for: context.connection) else { return makeEmptyDetail(context: context) }
        return AnyView(WikiPageView(connection: context.connection, workspace: workspace.id, page: id, path: path, title: title).readingMode().id("\(workspace.id)/\(id)"))
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        0
    }
}

struct WikiHomeDetail: View {
    let context: PluginContext
    @State private var empty = false

    var body: some View {
        Group {
            if empty {
                ContentUnavailableView("No Wiki Pages Yet", systemImage: "book", description: Text("Agents write here as they work. Pages show in the tree."))
            } else {
                ProgressView()
            }
        }
        .task(id: context.navigation.workspaceKey) { await open() }
    }

    private func open() async {
        let navigation = context.navigation
        guard let key = navigation.workspaceKey else { return }
        if let last = navigation.lastWiki.removeValue(forKey: key) {
            return navigation.setPath([last], tab: .wiki)
        }
        guard context.connection.supports(WikiCommand.locate),
              let home = try? await context.connection.command(WikiCommand.locate, workspace: key.workspace, args: .object(["home": .bool(true)]), as: WikiLocation.self) else { return empty = true }
        guard navigation.workspaceKey == key, navigation.path(.wiki).isEmpty else { return }
        guard let id = home.id, let path = home.path else { return empty = true }
        navigation.setPath([StackEntry(.wikiPage(id: id, path: path, title: home.title ?? URL(filePath: path).deletingPathExtension().lastPathComponent))], tab: .wiki)
    }
}

struct WikiRoot: View {
    let context: PluginContext

    var body: some View {
        let workspace = context.navigation.workspace(for: context.connection)
        Group {
            if let workspace {
                SectionScreen(connection: context.connection, workspace: workspace.id, section: WikiPluginInfo.id, tabRoot: true)
                    .id(workspace.id)
            } else {
                ConnectionUnavailableView(connection: context.connection)
            }
        }
        .tabRootChrome(context, title: "Wiki")
        .onAppear { DocumentWebView.prepare() }
    }
}

struct WikiPageView: View {
    let connection: ComputerConnection
    let page: String
    let path: String
    let title: String
    @State private var ui: WorkspaceUI
    @State private var owner = UUID()
    @State private var visible = false
    @State private var shown: PageTree?
    @State private var find = DocumentFind()
    @State private var pushed = false
    private static let sidebar = WikiPluginInfo.id + ".sidebar"
    private static let main = WikiPluginInfo.id + ".main"

    init(connection: ComputerConnection, workspace: String, page: String, path: String, title: String) {
        self.connection = connection
        self.page = page
        self.path = path
        self.title = title
        _ui = State(initialValue: connection.workspaceUI(workspace))
    }

    var body: some View {
        Group {
            if ui.missing.contains(page) {
                ContentUnavailableView("Page Not Found", systemImage: "book", description: Text("It was deleted or moved on the Mac."))
            } else if let shown = shown ?? ui.page(page, path: path, in: Self.main) {
                SectionSlot(ui: ui, slot: Self.main, tree: shown.tree, connection: connection)
                    .environment(\.sduiPage, SDUIPage(title: title, find: find))
                    .environment(\.documentHeld, !pushed)
                    .overlay(alignment: .bottom) {
                        if shown.updating {
                            UpdatingLine()
                                .padding(.horizontal, 12)
                                .padding(.vertical, 6)
                                .glassEffect(.regular, in: .capsule)
                                .padding(.bottom, 8)
                                .transition(.opacity)
                        }
                    }
                    .animation(.easeOut(duration: 0.2), value: shown.updating)
            } else {
                ProgressView()
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(PushSettled { pushed = true })
        .background(Color(.systemBackground))
        .navigationTitle(title)
        .modifier(PadPageTitle(title: title))
        .onAppear {
            visible = true
            ui.show([Self.sidebar, Self.main], owner: owner)
            shown = ui.page(page, path: path, in: Self.main) ?? shown
        }
        .onDisappear {
            visible = false
            ui.hide(owner: owner)
        }
        .onChange(of: ui.page(page, path: path, in: Self.main)) { _, fresh in
            if visible || shown == nil, let fresh { shown = fresh }
        }
        .task(id: "\(visible)|\(PhoneSections.selected(in: ui.view(Self.sidebar)) ?? "")") {
            guard visible else { return }
            await ui.select(page, in: Self.sidebar)
        }
    }
}
