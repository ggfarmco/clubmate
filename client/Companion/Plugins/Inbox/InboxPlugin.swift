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
        guard let selection, let item = context.model.inbox.items.first(where: { $0.id == selection }) else {
            return AnyView(ContentUnavailableView("No Notification Selected", systemImage: "tray", description: Text("Notifications that point at a session open it in Agents.")))
        }
        return AnyView(InboxDetailView(item: item, workspace: context.connection.workspaces.first { $0.id == item.workspace }?.name))
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        let sections = Self.sections(context)
        return sections.waiting.count { context.model.inbox.showsDot($0, in: sections) }
    }

    @MainActor
    static func store(_ context: PluginContext, _ item: InboxItem) -> AgentsStore? {
        context.model.agents(computer: item.computerID, workspace: item.workspace)
    }

    @MainActor
    static func sections(_ context: PluginContext) -> InboxSections {
        context.model.inbox.sections(computer: context.connection.computer.id) { item in
            guard let session = item.session, let store = store(context, item), store.loaded else { return nil }
            return store.session(session)?.group == .needsInput
        }
    }
}

struct InboxListView: View {
    let context: PluginContext

    var body: some View {
        let sections = InboxPlugin.sections(context)
        let workspaces = Set((sections.waiting + sections.earlier).map(\.workspace)).sorted()
        List {
            if !sections.waiting.isEmpty {
                Section {
                    rows(sections.waiting, sections: sections, waiting: true)
                } header: {
                    HStack {
                        Text("Waiting for you")
                        Spacer()
                        Text("\(sections.waiting.count)")
                    }
                }
            }
            if !sections.earlier.isEmpty {
                Section("Earlier") {
                    rows(sections.earlier, sections: sections, waiting: false)
                }
            }
        }
        .listStyle(.insetGrouped)
        .overlay {
            if sections.waiting.isEmpty && sections.earlier.isEmpty {
                ContentUnavailableView("Nothing Needs You", systemImage: "tray", description: Text("Questions, permission requests and finished runs from agents on \(context.connection.computer.name) land here while the app is open."))
            }
        }
        .task(id: workspaces) {
            for workspace in workspaces {
                guard let store = context.model.agents(computer: context.connection.computer.id, workspace: workspace), !store.loaded else { continue }
                await store.refresh()
            }
        }
        .navigationTitle("Inbox")
        .navigationSubtitle(context.connection.computer.name)
        .toolbar {
            MacSubtitle(text: context.model.tag(context.connection.computer.id)?.lead(""))
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
                    Button("Mark All as Read", systemImage: "envelope.open") { context.model.inbox.markAllRead() }
                    Button("Clear", systemImage: "trash", role: .destructive) {
                        context.model.inbox.remove(computer: context.connection.computer.id)
                        context.navigation.detail = nil
                    }
                }
                .disabled(sections.waiting.isEmpty && sections.earlier.isEmpty)
            }
        }
    }

    private func rows(_ items: [InboxItem], sections: InboxSections, waiting: Bool) -> some View {
        ForEach(items) { item in
            let store = InboxPlugin.store(context, item)
            Button {
                open(item)
            } label: {
                InboxRow(
                    item: item,
                    waiting: waiting,
                    dot: context.model.inbox.showsDot(item, in: sections),
                    command: item.kind == .permission ? item.session.flatMap { store?.transcripts[$0] }.flatMap { InboxItem.pendingCommand($0.entries) } : nil,
                    place: [context.connection.workspaces.first { $0.id == item.workspace }?.name, item.session.flatMap { store?.session($0)?.displayTitle }].compactMap { $0 }.joined(separator: " · ")
                )
            }
            .buttonStyle(.plain)
            .listRowBackground(context.navigation.detail == item.id ? Color.accentColor.opacity(0.15) : nil)
            .swipeActions(edge: .leading) {
                if context.model.inbox.isUnread(item) {
                    Button("Mark as Read", systemImage: "envelope.open") { context.model.inbox.markRead(item) }
                        .tint(.accentColor)
                }
            }
            .task(id: item.id) {
                guard waiting, item.kind == .permission, let session = item.session, let store, store.transcripts[session] == nil else { return }
                await store.loadTranscript(session)
            }
        }
    }

    private func open(_ item: InboxItem) {
        context.model.inbox.markRead(item)
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
    let waiting: Bool
    let dot: Bool
    let command: String?
    let place: String

    private var muted: Bool {
        item.kind == .done || item.kind == .info
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            InboxGlyph(kind: item.kind, waiting: waiting)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(item.heading)
                        .font(.body.weight(waiting ? .semibold : .regular))
                        .foregroundStyle(muted ? .secondary : .primary)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    TimelineView(.periodic(from: .now, by: 30)) { timeline in
                        Text(item.age(now: timeline.date))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                summary
                    .font(.subheadline)
                    .foregroundStyle(muted ? .tertiary : .secondary)
                    .lineLimit(2)
                if !place.isEmpty {
                    Text(place)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Circle()
                .fill(Color.accentColor)
                .frame(width: 9, height: 9)
                .padding(.top, 7)
                .opacity(dot ? 1 : 0)
                .accessibilityHidden(!dot)
                .accessibilityLabel("Unread")
        }
        .frame(minHeight: 44)
        .contentShape(.rect)
        .listRowDensity()
    }

    private var summary: Text {
        if let command {
            return Text("Run \(Text(command).font(.system(.subheadline, design: .monospaced)))")
        }
        return Text(item.message.isEmpty ? item.displayTitle : item.message)
    }
}

struct InboxGlyph: View {
    let kind: NotificationKind
    let waiting: Bool

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: 14, weight: .semibold))
            .foregroundStyle(ink)
            .frame(width: 28, height: 28)
            .background(fill, in: .circle)
            .accessibilityHidden(true)
    }

    private var symbol: String {
        switch kind {
        case .needsInput: "questionmark.bubble"
        case .permission: "hand.raised"
        case .question: "questionmark.circle"
        case .stuck: "exclamationmark.circle"
        case .done: "checkmark"
        case .info: "info"
        }
    }

    private var ink: Color {
        if waiting { return .onWarning }
        return kind == .stuck ? .dangerInk : .secondary
    }

    private var fill: Color {
        if waiting { return .warning }
        return kind == .stuck ? Color.danger.opacity(0.18) : .surface2
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
                Text(item.displayTitle)
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
