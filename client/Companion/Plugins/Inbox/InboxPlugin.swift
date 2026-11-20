import SwiftUI

import CompanionKit

struct InboxPlugin: CompanionPluginUI {
    let id = InboxPluginInfo.id
    let title = "Inbox"
    let systemImage = "tray"
    let requiredCommands: [String] = []

    @MainActor
    func makeRoot(context: PluginContext) -> AnyView {
        AnyView(InboxListView(context: context))
    }

    @MainActor
    func makeEmptyDetail(context: PluginContext) -> AnyView {
        AnyView(ContentUnavailableView("No Notification Selected", systemImage: "tray", description: Text("Notifications that point at a session open it in Agents.")))
    }

    @MainActor
    func makeDestination(_ destination: Destination, context: PluginContext) -> AnyView? {
        switch destination {
        case .inbox:
            return makeRoot(context: context)
        case .section(InboxPluginInfo.id, let selection?):
            guard let item = context.model.inbox.items.first(where: { $0.id == selection }) else { return makeEmptyDetail(context: context) }
            return AnyView(InboxDetailView(item: item, workspace: context.connection.workspaces.first { $0.id == item.workspace }?.name))
        default:
            return nil
        }
    }

    static func isEntry(_ destination: Destination) -> Bool {
        if case .section(InboxPluginInfo.id, _) = destination { return true }
        return false
    }

    static func entry(_ item: InboxItem) -> Destination {
        .section(id: InboxPluginInfo.id, item: item.id)
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        let sections = Self.sections(context)
        return sections.waiting.count(where: context.model.inbox.isUnread)
    }

    @MainActor
    static func store(_ context: PluginContext, _ item: InboxItem) -> AgentsStore? {
        context.model.agents(computer: item.computerID, workspace: item.workspace)
    }

    @MainActor
    static func sections(_ context: PluginContext) -> InboxSections {
        context.model.inbox.sections(computer: context.connection.computer.id) { item in
            guard let session = item.session, let store = store(context, item), store.fetched else { return nil }
            return store.session(session)?.needsYou == true
        }
    }
}

struct InboxListView: View {
    let context: PluginContext

    var body: some View {
        let model = context.model
        let needs = model.needsYou()
        let waiting = Set(needs.map(\.id))
        let earlier = model.inbox.items.filter { item in item.session.map { !waiting.contains(NeedsYouItem.id(computer: item.computerID, session: $0)) } ?? true }
        let stores = Set(earlier.map { WorkspaceKey(computer: $0.computerID, workspace: $0.workspace) })
        List {
            if !needs.isEmpty {
                Section {
                    ForEach(needs) { item in
                        NeedsYouRow(item: item, dot: unread(item).isEmpty == false) { open(item) }
                    }
                } header: {
                    HStack {
                        Text("Waiting for you")
                        Spacer()
                        Text("\(needs.count)")
                    }
                } footer: {
                    ForEach(model.offlineMacNames(needs), id: \.self) { name in
                        Text("\(name) is offline — answer when it's back.")
                    }
                }
            }
            if !earlier.isEmpty {
                Section("Earlier") {
                    ForEach(earlier) { item in
                        Button {
                            open(item)
                        } label: {
                            InboxRow(item: item, waiting: false, dot: model.inbox.isUnread(item), command: nil, place: place(item))
                        }
                        .buttonStyle(.plain)
                        .swipeActions(edge: .leading) {
                            if model.inbox.isUnread(item) {
                                Button("Mark as Read", systemImage: "envelope.open") { model.inbox.markRead(item) }
                                    .tint(.accentColor)
                            }
                        }
                    }
                }
            }
        }
        .columnList()
        .overlay {
            if needs.isEmpty && earlier.isEmpty {
                ContentUnavailableView("Nothing Needs You", systemImage: "tray", description: Text("Questions, permission requests and finished runs from your Macs land here while the app is open."))
            }
        }
        .task(id: stores) {
            for key in stores {
                guard let store = model.agents(computer: key.computer, workspace: key.workspace), !store.fetched else { continue }
                await store.refresh()
            }
        }
        .navigationTitle("Inbox")
        .navigationBarTitleDisplayMode(.inline)
        .navigationSubtitle(model.computers.count > 1 ? "All Macs" : "")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
                    Button("Mark All as Read", systemImage: "envelope.open") { model.inbox.markAllRead() }
                    Button("Clear", systemImage: "trash", role: .destructive) {
                        model.inbox.clear()
                        context.navigation.setPath(context.navigation.path.filter { !InboxPlugin.isEntry($0.destination) }, tab: context.navigation.tab)
                    }
                }
                .disabled(model.inbox.items.isEmpty)
            }
        }
    }

    private func unread(_ item: NeedsYouItem) -> [InboxItem] {
        context.model.inbox.items.filter { $0.computerID == item.computer && $0.session == item.session.id && context.model.inbox.isUnread($0) }
    }

    private func place(_ item: InboxItem) -> String {
        let store = InboxPlugin.store(context, item)
        let workspace = context.model.connection(item.computerID)?.workspaces.first { $0.id == item.workspace }?.name
        return [workspace, item.session.flatMap { store?.session($0)?.displayTitle }].compactMap { $0 }.joined(separator: " · ")
    }

    private func open(_ item: NeedsYouItem) {
        for note in unread(item) { context.model.inbox.markRead(note) }
        context.navigation.pushFromInbox(.session(item.session.id), in: WorkspaceKey(computer: item.computer, workspace: item.workspace.id))
    }

    private func open(_ item: InboxItem) {
        context.model.inbox.markRead(item)
        let key = WorkspaceKey(computer: item.computerID, workspace: item.workspace)
        context.navigation.pushFromInbox(item.session.map(Destination.session) ?? InboxPlugin.entry(item), in: key)
    }
}

struct UnreadDot: View {
    let shown: Bool

    var body: some View {
        Circle()
            .fill(Color.accentColor)
            .frame(width: 9, height: 9)
            .opacity(shown ? 1 : 0)
            .accessibilityHidden(!shown)
            .accessibilityLabel("Unread")
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
                        .foregroundStyle(muted ? Color.captionInk : .primary)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    TimelineView(.periodic(from: .now, by: 30)) { timeline in
                        Text(item.age(now: timeline.date))
                            .font(.footnote)
                            .foregroundStyle(Color.captionInk)
                    }
                }
                summary
                    .font(.subheadline)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(2)
                if !place.isEmpty {
                    Text(place)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
            }
            UnreadDot(shown: dot)
                .padding(.top, 7)
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
        return kind == .stuck ? .dangerInk : Color.captionInk
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
                    .foregroundStyle(Color.captionInk)
                Text(item.displayTitle)
                    .font(.title2.bold())
                Text(item.message)
                    .textSelection(.enabled)
                Text([workspace, item.date.formatted(date: .abbreviated, time: .shortened)].compactMap { $0 }.joined(separator: " · "))
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
            }
            .padding()
            .frame(maxWidth: 720, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .navigationTitle("Notification")
        .navigationBarTitleDisplayMode(.inline)
    }
}
