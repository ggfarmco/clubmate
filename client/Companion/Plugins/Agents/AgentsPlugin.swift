import SwiftUI

import CompanionKit

struct AgentsPlugin: CompanionPluginUI {
    let id = AgentsPluginInfo.id
    let title = "Agents"
    let systemImage = "sparkles"
    let requiredCommands = AgentsPluginInfo.requiredCommands

    @MainActor
    func makeList(context: PluginContext) -> AnyView {
        AnyView(AgentsListView(context: context))
    }

    @MainActor
    func makeDetail(context: PluginContext, selection: String?) -> AnyView {
        if context.connection.state == .unauthorized {
            return AnyView(ConnectionUnavailableView(connection: context.connection))
        }
        guard let selection, let store = Self.store(context) else {
            return AnyView(ContentUnavailableView("No Session Selected", systemImage: "sparkles", description: Text("Pick a session to read its transcript and reply.")))
        }
        if selection == AgentsPluginInfo.newSession {
            return AnyView(NewSessionView(context: context, store: store))
        }
        return AnyView(SessionDetailView(context: context, store: store, sessionID: selection).id(selection))
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        Self.store(context)?.needsInputCount ?? 0
    }

    @MainActor
    static func store(_ context: PluginContext) -> AgentsStore? {
        guard let workspace = context.navigation.workspace(for: context.connection) else { return nil }
        return context.model.agents(computer: context.connection.computer.id, workspace: workspace.id)
    }

    @MainActor
    static func startNew(_ context: PluginContext) {
        context.navigation.detail = AgentsPluginInfo.newSession
        context.navigation.preferredColumn = .detail
    }
}

struct AgentsListView: View {
    let context: PluginContext

    var body: some View {
        let workspace = context.navigation.workspace(for: context.connection)
        Group {
            if let workspace, let store = AgentsPlugin.store(context) {
                SessionListView(context: context, store: store)
                    .task(id: workspace.id) { await store.refresh() }
            } else if context.connection.state == .online {
                ContentUnavailableView("No Workspaces", systemImage: "folder", description: Text("Open a workspace in Clubmate on \(context.connection.computer.name)."))
            } else {
                ConnectionUnavailableView(connection: context.connection)
            }
        }
        .navigationTitle(workspace?.name ?? "Agents")
        .toolbar {
            if context.connection.workspaces.count > 1 {
                ToolbarItem(placement: .primaryAction) {
                    Menu("Workspace", systemImage: "folder") {
                        Picker("Workspace", selection: Binding(get: { workspace?.id ?? "" }, set: { id in
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
            ToolbarItem(placement: .primaryAction) {
                Button("New Session", systemImage: "square.and.pencil") { AgentsPlugin.startNew(context) }
                    .disabled(workspace == nil || context.connection.state != .online)
            }
        }
    }
}

struct SessionListView: View {
    let context: PluginContext
    let store: AgentsStore
    @State private var expanded: Set<String> = []
    @State private var closedExpanded = false

    var body: some View {
        List(selection: Binding(get: { context.navigation.detail }, set: { id in
            context.navigation.detail = id
            if id != nil { context.navigation.preferredColumn = .detail }
        })) {
            if context.connection.state != .online {
                Section {
                    ConnectionBanner(connection: context.connection)
                }
            }
            ForEach(store.groups, id: \.group) { group in
                if group.group == .closed {
                    Section(isExpanded: $closedExpanded) {
                        rows(group.sessions)
                    } header: {
                        Button {
                            withAnimation { closedExpanded.toggle() }
                        } label: {
                            HStack {
                                Text("\(group.group.title) · \(group.sessions.count)")
                                Spacer()
                                Image(systemName: "chevron.right")
                                    .rotationEffect(.degrees(closedExpanded ? 90 : 0))
                            }
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                    }
                } else {
                    Section {
                        rows(group.sessions)
                    } header: {
                        Text(group.group.title)
                    }
                }
            }
        }
        .overlay {
            if !store.loaded {
                ProgressView()
            } else if store.sessions.isEmpty {
                ContentUnavailableView {
                    Label("No Agent Sessions", systemImage: "sparkles")
                } description: {
                    Text(store.error ?? "Start a session and follow it from here.")
                } actions: {
                    Button("New Session", systemImage: "square.and.pencil") { AgentsPlugin.startNew(context) }
                        .buttonStyle(.glassProminent)
                        .disabled(context.connection.state != .online)
                }
            }
        }
        .refreshable { await store.refresh() }
    }

    private func rows(_ nodes: [SessionNode]) -> some View {
        ForEach(nodes) { node in
            SessionTreeRow(node: node, depth: 0, expanded: $expanded)
        }
    }
}

struct SessionTreeRow: View {
    let node: SessionNode
    let depth: Int
    @Binding var expanded: Set<String>

    var body: some View {
        HStack(spacing: 8) {
            SessionRow(session: node.session, nested: depth > 0, descendants: node.descendants)
            if !node.children.isEmpty {
                Button(isExpanded ? "Collapse" : "Expand", systemImage: "chevron.right") {
                    withAnimation { toggle() }
                }
                .labelStyle(.iconOnly)
                .buttonStyle(.borderless)
                .rotationEffect(.degrees(isExpanded ? 90 : 0))
                .foregroundStyle(.secondary)
            }
        }
        .padding(.leading, CGFloat(depth) * 20)
        .tag(node.id)
        if isExpanded {
            ForEach(node.children) { child in
                SessionTreeRow(node: child, depth: depth + 1, expanded: $expanded)
            }
        }
    }

    private var isExpanded: Bool {
        expanded.contains(node.id)
    }

    private func toggle() {
        if expanded.contains(node.id) { expanded.remove(node.id) } else { expanded.insert(node.id) }
    }
}

struct SessionRow: View {
    let session: AgentSession
    var nested = false
    var descendants = 0

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            StatusIcon(session: session)
            VStack(alignment: .leading, spacing: 3) {
                Text(session.displayTitle)
                    .font(.body.weight(.medium))
                    .lineLimit(2)
                HStack(spacing: 4) {
                    if session.parentSession != nil && !nested {
                        Image(systemName: "arrow.turn.down.right")
                    }
                    Text(session.worktreeName)
                    Text("·")
                    Text(session.updatedAt.formatted(.relative(presentation: .named)))
                    if descendants > 0 {
                        Text("·")
                        Text("\(Image(systemName: "arrow.triangle.branch")) \(descendants)")
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                if let error = session.error {
                    Text(error)
                        .font(.footnote)
                        .foregroundStyle(.red)
                        .lineLimit(2)
                }
            }
            Spacer(minLength: 4)
            HStack(spacing: 4) {
                if session.afk {
                    Tag(text: "AFK", color: .purple)
                }
                if !session.pending.isEmpty {
                    Tag(text: session.pending.count == 1 ? "Permission" : "\(session.pending.count) Permissions", color: .orange)
                } else if session.asks {
                    Tag(text: "Question", color: .blue)
                }
            }
        }
        .padding(.vertical, 2)
    }
}

struct Tag: View {
    let text: String
    let color: Color

    var body: some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .foregroundStyle(.white)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(color, in: .capsule)
    }
}

struct StatusIcon: View {
    let session: AgentSession

    var body: some View {
        Image(systemName: symbol)
            .foregroundStyle(color)
            .symbolEffect(.rotate, isActive: session.displayStatus == .running)
            .frame(width: 22)
            .accessibilityLabel(session.statusLabel)
    }

    private var symbol: String {
        if session.status == .waiting || !session.pending.isEmpty { return "hand.raised.circle.fill" }
        if session.asks { return "questionmark.bubble.fill" }
        switch session.displayStatus {
        case .running: return "circle.dotted"
        case .waiting: return "hand.raised.circle"
        case .done: return "checkmark.circle.fill"
        case .error: return "xmark.octagon.fill"
        case .closed: return "archivebox"
        default: return "circle"
        }
    }

    private var color: Color {
        if session.status == .waiting || !session.pending.isEmpty { return .orange }
        if session.asks { return .blue }
        switch session.displayStatus {
        case .running: return .accentColor
        case .waiting: return .orange
        case .done: return .green
        case .error: return .red
        default: return .secondary
        }
    }
}

struct ConnectionBanner: View {
    let connection: ComputerConnection

    var body: some View {
        Button {
            connection.reconnectNow()
        } label: {
            Label {
                VStack(alignment: .leading, spacing: 2) {
                    Text(connection.state.label)
                    if case .offline(let reason?) = connection.state {
                        Text(reason).font(.footnote).foregroundStyle(.secondary)
                    }
                }
            } icon: {
                Image(systemName: connection.state == .unauthorized ? "lock.slash" : "wifi.slash")
            }
        }
        .disabled(connection.state == .connecting)
    }
}

struct ConnectionUnavailableView: View {
    let connection: ComputerConnection
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        switch connection.state {
        case .unauthorized:
            ContentUnavailableView {
                Label("Access Revoked", systemImage: "lock.slash")
            } description: {
                Text("\(connection.computer.name) no longer accepts this phone. Pair it again from Clubmate Settings → Devices.")
            } actions: {
                Button("Pair Again") { navigation.showingAddComputer = true }
                    .buttonStyle(.glassProminent)
            }
        case .offline(let reason):
            ContentUnavailableView {
                Label("\(connection.computer.name) Is Offline", systemImage: "wifi.slash")
            } description: {
                Text(reason ?? "Check that the Mac is awake and Tailscale is connected.")
            } actions: {
                Button("Try Again") { connection.reconnectNow() }
                    .buttonStyle(.glass)
            }
        case .connecting, .online:
            ProgressView("Connecting to \(connection.computer.name)…")
        }
    }
}

struct NewSessionView: View {
    let context: PluginContext
    let store: AgentsStore
    @State private var draft = ""
    @State private var attachments: [ComposerAttachment] = []
    @State private var sending = false
    @State private var failure: String?

    var body: some View {
        ContentUnavailableView("New Session", systemImage: "sparkles", description: Text("Describe the task and attach photos or videos. The session starts when you send."))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .safeAreaInset(edge: .bottom) {
                ComposerView(
                    draft: $draft,
                    attachments: $attachments,
                    session: nil,
                    enabled: context.connection.state == .online,
                    sending: sending,
                    send: send,
                    stop: {}
                )
            }
            .navigationTitle("New Session")
            .navigationBarTitleDisplayMode(.inline)
            .alert("Couldn't Start the Session", isPresented: Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })) {
                Button("OK") { failure = nil }
            } message: {
                Text(failure ?? "")
            }
    }

    private func send() {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        let attached = attachments
        guard !text.isEmpty || !attached.isEmpty else { return }
        sending = true
        Task {
            do {
                if let id = try await store.create(text, attachments: attached) {
                    context.navigation.detail = id
                }
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
            }
            sending = false
        }
    }
}
