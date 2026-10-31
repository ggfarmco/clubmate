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
}

struct AgentsListView: View {
    let context: PluginContext
    @State private var creating = false

    var body: some View {
        let workspace = context.navigation.workspace(for: context.connection)
        Group {
            if let workspace, let store = AgentsPlugin.store(context) {
                SessionListView(context: context, store: store, creating: $creating)
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
                Button("New Session", systemImage: "square.and.pencil") { creating = true }
                    .disabled(workspace == nil || context.connection.state != .online)
            }
        }
    }
}

struct SessionListView: View {
    let context: PluginContext
    let store: AgentsStore
    @Binding var creating: Bool

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
                Section {
                    ForEach(group.sessions) { session in
                        SessionRow(session: session)
                            .tag(session.id)
                    }
                } header: {
                    Text(group.group.title)
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
                    Button("New Session", systemImage: "square.and.pencil") { creating = true }
                        .buttonStyle(.glassProminent)
                        .disabled(context.connection.state != .online)
                }
            }
        }
        .refreshable { await store.refresh() }
        .sheet(isPresented: $creating) {
            NewSessionView(store: store) { id in
                context.navigation.detail = id
                context.navigation.preferredColumn = .detail
            }
        }
    }
}

struct SessionRow: View {
    let session: AgentSession

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            StatusIcon(session: session)
            VStack(alignment: .leading, spacing: 3) {
                Text(session.displayTitle)
                    .font(.body.weight(.medium))
                    .lineLimit(2)
                HStack(spacing: 4) {
                    if session.parentSession != nil {
                        Image(systemName: "arrow.turn.down.right")
                    }
                    Text(session.worktreeName)
                    Text("·")
                    Text(session.updatedAt.formatted(.relative(presentation: .named)))
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
            if !session.pending.isEmpty {
                Tag(text: session.pending.count == 1 ? "Permission" : "\(session.pending.count) Permissions", color: .orange)
            } else if session.asks {
                Tag(text: "Question", color: .blue)
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
            .symbolEffect(.rotate, isActive: session.status == .running)
            .frame(width: 22)
            .accessibilityLabel(session.status.label)
    }

    private var symbol: String {
        if session.status == .waiting || !session.pending.isEmpty { return "hand.raised.circle.fill" }
        if session.asks { return "questionmark.bubble.fill" }
        switch session.status {
        case .running: return "circle.dotted"
        case .done: return "checkmark.circle.fill"
        case .error: return "xmark.octagon.fill"
        case .closed: return "archivebox"
        default: return "circle"
        }
    }

    private var color: Color {
        if session.status == .waiting || !session.pending.isEmpty { return .orange }
        if session.asks { return .blue }
        switch session.status {
        case .running: return .accentColor
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
    let store: AgentsStore
    let created: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var prompt = ""
    @State private var title = ""
    @State private var starting = false
    @State private var error: String?
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("What should the agent do?", text: $prompt, axis: .vertical)
                        .lineLimit(4...12)
                        .focused($focused)
                } header: {
                    Text("First Prompt")
                } footer: {
                    Text("Leave empty to start the session idle.")
                }
                Section("Title") {
                    TextField("Optional", text: $title)
                }
                if let error {
                    Section {
                        Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("New Session")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", systemImage: "xmark") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if starting {
                        ProgressView()
                    } else {
                        Button("Start", systemImage: "arrow.up") { start() }
                            .buttonStyle(.glassProminent)
                    }
                }
            }
            .onAppear { focused = true }
        }
        .presentationDetents([.medium, .large])
        .sensoryFeedback(.success, trigger: starting) { old, new in old && !new && error == nil }
    }

    private func start() {
        starting = true
        error = nil
        Task {
            do {
                let id = try await store.create(prompt: prompt.trimmingCharacters(in: .whitespacesAndNewlines), title: title.trimmingCharacters(in: .whitespacesAndNewlines))
                starting = false
                dismiss()
                if let id { created(id) }
            } catch {
                self.error = (error as? RemoteError)?.message ?? error.localizedDescription
                starting = false
            }
        }
    }
}
