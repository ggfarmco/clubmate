import SwiftUI

import CompanionKit

struct DocsPlugin: CompanionPluginUI {
    let id = DocsPluginInfo.id
    let title = "Docs"
    let systemImage = "doc.richtext"
    let requiredCommands = DocsPluginInfo.requiredCommands
    let inWorkspace = true

    @MainActor
    func makeList(context: PluginContext) -> AnyView {
        AnyView(DocsListView(context: context))
    }

    @MainActor
    func makeDetail(context: PluginContext, selection: String?) -> AnyView {
        if context.connection.state == .unauthorized {
            return AnyView(ConnectionUnavailableView(connection: context.connection))
        }
        return AnyView(ContentUnavailableView("No Doc Open", systemImage: "doc.richtext", description: Text("Pick a doc to read it.")))
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        0
    }

    func listHasMic(connection: ComputerConnection) -> Bool {
        true
    }

    @MainActor
    static func store(_ context: PluginContext) -> DocsListStore? {
        guard let workspace = context.navigation.workspace(for: context.connection) else { return nil }
        return context.model.docs(computer: context.connection.computer.id, workspace: workspace.id)
    }
}

struct DocsListView: View {
    let context: PluginContext

    var body: some View {
        let workspace = context.navigation.workspace(for: context.connection)
        Group {
            if let workspace, let store = DocsPlugin.store(context) {
                DocsListContent(context: context, store: store)
                    .task(id: workspace.id) { await store.refresh() }
            } else if context.connection.state == .online {
                ContentUnavailableView("No Workspaces", systemImage: "folder", description: Text("Open a workspace in Clubmate on \(context.connection.computer.name)."))
            } else {
                ConnectionUnavailableView(connection: context.connection)
            }
        }
        .workspaceChrome(context, title: workspace?.name ?? "Docs")
        .toolbar {
            WorkspaceMenu(context: context)
        }
    }
}

private struct DocsListContent: View {
    let context: PluginContext
    let store: DocsListStore
    @State private var query = ""

    var body: some View {
        let docs = store.matching(query)
        let agent = docs.filter(\.agent)
        let other = docs.filter { !$0.agent }
        List {
            WorkspaceTabs(context: context)
            if context.connection.state != .online {
                Section {
                    ConnectionBanner(connection: context.connection)
                }
            }
            if !agent.isEmpty {
                Section {
                    rows(agent)
                } header: {
                    header("Written by agents", agent.count)
                }
            }
            if !other.isEmpty {
                Section {
                    rows(other)
                } header: {
                    header(agent.isEmpty ? "Docs" : "All docs", other.count)
                } footer: {
                    if store.truncated {
                        Text("Only the first \(store.docs.count) docs are listed.")
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .searchBar(text: $query, prompt: "Search docs")
        .overlay {
            if !store.loaded {
                ProgressView()
            } else if store.docs.isEmpty {
                ContentUnavailableView("No Docs", systemImage: "doc.richtext", description: Text(store.error ?? "Markdown and HTML files in this workspace show here, the ones agents wrote first."))
            } else if docs.isEmpty {
                ContentUnavailableView.search(text: query)
            }
        }
        .refreshable { await store.refresh() }
    }

    private func header(_ title: String, _ count: Int) -> some View {
        HStack {
            Text(title)
            Spacer()
            Text("\(count)")
        }
    }

    private func rows(_ docs: [DocEntry]) -> some View {
        ForEach(docs) { doc in
            Button {
                context.navigation.openDoc(computer: context.connection.computer.id, workspace: store.workspace, path: doc.path)
            } label: {
                DocRow(doc: doc)
            }
            .tint(.primary)
            .disabled(context.connection.state != .online)
        }
    }
}

struct DocRow: View {
    let doc: DocEntry

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: doc.symbol)
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(.secondary)
                .frame(width: 28, height: 28)
                .background(Color(.tertiarySystemFill), in: .circle)
            VStack(alignment: .leading, spacing: 2) {
                Text(doc.name)
                    .foregroundStyle(.primary)
                    .lineLimit(2)
                HStack(spacing: 0) {
                    if let folder = doc.folder {
                        Text(folder)
                            .truncationMode(.middle)
                    }
                    if let age {
                        Text((doc.folder == nil ? "" : " · ") + age)
                            .layoutPriority(1)
                    }
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .frame(minHeight: 44)
        .contentShape(.rect)
        .listRowDensity()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(doc.name)
        .accessibilityValue([doc.folder, age].compactMap { $0 }.joined(separator: ", "))
    }

    private var age: String? {
        doc.modified?.formatted(.relative(presentation: .named))
    }
}

extension DocEntry {
    var symbol: String {
        isArtifact ? "chart.bar.doc.horizontal" : "doc.richtext"
    }
}
