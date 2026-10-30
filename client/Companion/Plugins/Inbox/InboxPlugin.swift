import SwiftUI

import CompanionKit

struct InboxPlugin: CompanionPluginUI {
    let id = InboxPluginInfo.id
    let title = "Inbox"
    let systemImage = "tray"
    let requiredCommands: [String] = []

    @MainActor
    func makeList(context: PluginContext) -> AnyView {
        AnyView(InboxListView(context: context))
    }

    @MainActor
    func makeDetail(context: PluginContext, selection: String?) -> AnyView {
        guard let selection, let item = context.model.inbox.items.first(where: { $0.id == selection && $0.computerID == context.connection.computer.id }) else {
            return AnyView(ContentUnavailableView("No Notification Selected", systemImage: "tray", description: Text("Notifications that point at a session open it in Agents.")))
        }
        return AnyView(InboxDetailView(item: item, workspace: context.connection.workspaces.first { $0.id == item.workspace }?.name))
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        let inbox = context.model.inbox
        return inbox.items.filter { $0.computerID == context.connection.computer.id && inbox.unread.contains($0.id) }.count
    }
}

struct InboxListView: View {
    let context: PluginContext

    var body: some View {
        let items = context.model.inbox.items.filter { $0.computerID == context.connection.computer.id }
        List {
            ForEach(items) { item in
                Button {
                    open(item)
                } label: {
                    InboxRow(item: item, unread: context.model.inbox.unread.contains(item.id), workspace: context.connection.workspaces.first { $0.id == item.workspace }?.name)
                }
                .buttonStyle(.plain)
                .listRowBackground(context.navigation.detail == item.id ? Color.accentColor.opacity(0.15) : nil)
            }
        }
        .overlay {
            if items.isEmpty {
                ContentUnavailableView("No Notifications", systemImage: "tray", description: Text("Agent notifications from \(context.connection.computer.name) appear here while the app is open."))
            }
        }
        .navigationTitle("Inbox")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
                    Button("Mark All as Read", systemImage: "envelope.open") { context.model.inbox.markAllRead() }
                    Button("Clear", systemImage: "trash", role: .destructive) {
                        context.model.inbox.remove(computer: context.connection.computer.id)
                        context.navigation.detail = nil
                    }
                }
                .disabled(items.isEmpty)
            }
        }
    }

    private func open(_ item: InboxItem) {
        context.model.inbox.markRead(item.id)
        if let session = item.session {
            context.navigation.workspaces[item.computerID] = item.workspace
            context.navigation.open(computer: item.computerID, plugin: AgentsPluginInfo.id, detail: session)
        } else {
            context.navigation.detail = item.id
            context.navigation.preferredColumn = .detail
        }
    }
}

struct InboxRow: View {
    let item: InboxItem
    let unread: Bool
    let workspace: String?

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: item.kind.symbol)
                .font(.title3)
                .foregroundStyle(color)
                .frame(width: 28)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(item.title.isEmpty ? item.kind.label : item.title)
                        .font(.body.weight(unread ? .semibold : .regular))
                        .lineLimit(1)
                    Spacer()
                    Text(item.date.formatted(.relative(presentation: .named)))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Text(item.message)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
                Text([workspace, item.session == nil ? nil : "Opens the session"].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
            if unread {
                Circle().fill(.tint).frame(width: 8, height: 8).padding(.top, 6)
            }
        }
        .contentShape(.rect)
        .padding(.vertical, 2)
    }

    private var color: Color {
        switch item.kind {
        case .needsInput: .blue
        case .permission: .orange
        case .done: .green
        case .stuck: .red
        case .info: .secondary
        }
    }
}

struct InboxDetailView: View {
    let item: InboxItem
    let workspace: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Label(item.kind.label, systemImage: item.kind.symbol)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.secondary)
                Text(item.title.isEmpty ? item.kind.label : item.title)
                    .font(.title2.bold())
                Text(item.message)
                    .textSelection(.enabled)
                Text([workspace, item.date.formatted(date: .abbreviated, time: .shortened)].compactMap { $0 }.joined(separator: " · "))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .padding()
            .frame(maxWidth: 720, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .navigationTitle("Notification")
        .navigationBarTitleDisplayMode(.inline)
    }
}
