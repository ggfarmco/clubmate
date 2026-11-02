import SwiftUI

import CompanionKit

struct AgentsPlugin: CompanionPluginUI {
    let id = AgentsPluginInfo.id
    let title = "Agents"
    let systemImage = "sparkles"
    let requiredCommands = AgentsPluginInfo.requiredCommands
    let inWorkspace = true

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
        .workspaceChrome(context, title: workspace?.name ?? "Agents")
        .toolbar {
            WorkspaceMenu(context: context)
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
    @State private var unfolded: Set<SessionGroup> = []

    var body: some View {
        List(selection: Binding(get: { context.navigation.detail }, set: { id in
            context.navigation.detail = id
            if id != nil { context.navigation.preferredColumn = .detail }
        })) {
            WorkspaceTabs(context: context)
            if context.connection.state != .online {
                Section {
                    ConnectionBanner(connection: context.connection)
                }
            }
            if store.searching {
                ForEach(store.searchResults ?? []) { result in
                    SearchResultRow(result: result, session: store.session(result.session))
                        .listRowDensity()
                        .listRowSeparator(wide ? .hidden : .automatic)
                        .tag(result.session)
                        .modifier(SelectedSessionRow(active: wide, selected: context.navigation.detail == result.session))
                }
            } else {
                sections
            }
        }
        .listStyle(insetGrouped: context.navigation.layout == .compact)
        .searchable(enabled: context.connection.supports(AgentsStore.searchCommand), text: Bindable(store).searchQuery, prompt: "Search sessions")
        .overlay {
            if !store.loaded {
                ProgressView()
            } else if store.searching {
                if let error = store.searchError {
                    ContentUnavailableView("Search Failed", systemImage: "magnifyingglass", description: Text(error))
                } else if store.searchResults == nil {
                    ProgressView()
                } else if store.searchResults?.isEmpty == true {
                    ContentUnavailableView.search(text: store.searchQuery)
                }
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

    @ViewBuilder
    private var sections: some View {
        ForEach(store.groups, id: \.group) { group in
            let items = group.sessions.flatMap { $0.rows(expanded: store.expanded) }
            let count = group.sessions.reduce(0) { $0 + 1 + $1.descendants }
            if group.group.folds {
                let open = unfolded.contains(group.group)
                Section(isExpanded: Binding(get: { open }, set: { _ in })) {
                    rows(items)
                } header: {
                    Button {
                        Motion.disclose {
                            if unfolded.remove(group.group) == nil { unfolded.insert(group.group) }
                        }
                    } label: {
                        HStack {
                            Text("\(group.group.title) · \(count)")
                            Spacer()
                            Image(systemName: "chevron.right")
                                .rotationEffect(.degrees(open ? 90 : 0))
                        }
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                }
            } else {
                Section {
                    rows(items)
                } header: {
                    HStack {
                        Text(group.group.title)
                        Spacer()
                        Text("\(count)")
                    }
                }
            }
        }
    }

    private var wide: Bool {
        context.navigation.layout != .compact
    }

    private func rows(_ items: [SessionRowItem]) -> some View {
        ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
            let nestedBelow = index + 1 < items.count && items[index + 1].depth > 0
            HStack(spacing: 0) {
                SessionRow(session: item.node.session, depth: item.depth, waitingChildren: item.node.waitingDescendants, chevron: item.toggleCount == nil)
                if let count = item.toggleCount {
                    SubagentToggle(count: count, expanded: item.expanded) {
                        Motion.disclose {
                            if item.expanded { store.expanded.remove(item.id) } else { store.expanded.insert(item.id) }
                        }
                    }
                }
            }
            .listRowDensity()
            .listRowSeparator(item.depth > 0 || wide ? .hidden : .automatic, edges: .top)
            .listRowSeparator(nestedBelow || wide ? .hidden : .automatic, edges: .bottom)
            .tag(item.id)
            .modifier(SelectedSessionRow(active: wide, selected: context.navigation.detail == item.id))
        }
    }
}

struct SelectedSessionRow: ViewModifier {
    let active: Bool
    let selected: Bool

    func body(content: Content) -> some View {
        if active {
            content
                .listRowBackground(RoundedRectangle(cornerRadius: 16).fill(selected ? Color.accentColor.opacity(0.2) : .clear).padding(.horizontal, 8))
                .foregroundStyle(selected ? AnyShapeStyle(Color(.label)) : AnyShapeStyle(.foreground))
        } else {
            content
        }
    }
}

extension View {
    @ViewBuilder
    func searchable(enabled: Bool, text: Binding<String>, prompt: String) -> some View {
        if enabled {
            searchable(text: text, prompt: prompt)
        } else {
            self
        }
    }

    @ViewBuilder
    func listStyle(insetGrouped: Bool) -> some View {
        if insetGrouped {
            listStyle(.insetGrouped)
        } else {
            listStyle(.plain)
        }
    }
}

struct SubagentToggle: View {
    let count: Int
    let expanded: Bool
    let toggle: () -> Void

    var body: some View {
        Button(action: toggle) {
            HStack(spacing: 4) {
                Text("\(count)")
                    .monospacedDigit()
                Image(systemName: "chevron.right")
                    .rotationEffect(.degrees(expanded ? 90 : 0))
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.borderless)
        .tint(.secondary)
        .accessibilityLabel(expanded ? "Hide \(count) subagents" : "Show \(count) subagents")
    }
}

struct SessionRow: View {
    let session: AgentSession
    var depth = 0
    var waitingChildren = 0
    var chevron = false

    var body: some View {
        TimelineView(.periodic(from: .now, by: 30)) { timeline in
            HStack(alignment: .top, spacing: 12) {
                SessionGlyphView(glyph: session.glyph(waitingChildren: waitingChildren), size: depth > 0 ? 24 : 28)
                VStack(alignment: .leading, spacing: 2) {
                    if depth > 0 {
                        Text(session.subagentLabel)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    Text(session.displayTitle)
                        .font(.body.weight(session.glyph.needsYou ? .semibold : .regular))
                        .foregroundStyle(session.isClosed ? .secondary : .primary)
                        .lineLimit(2)
                    StateLineText(line: session.stateLine(now: timeline.date, waitingChildren: waitingChildren))
                        .font(.subheadline)
                        .lineLimit(1)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if chevron {
                    RowChevron()
                        .frame(height: 22)
                }
            }
            .padding(.leading, CGFloat(min(depth, 2)) * 24)
            .frame(minHeight: 44)
            .contentShape(.rect)
            .accessibilityElement(children: .combine)
        }
    }
}

struct SearchResultRow: View {
    let result: AgentSearchResult
    let session: AgentSession?

    var body: some View {
        TimelineView(.periodic(from: .now, by: 30)) { timeline in
            HStack(alignment: .top, spacing: 12) {
                SessionGlyphView(glyph: session?.glyph ?? result.glyph)
                VStack(alignment: .leading, spacing: 2) {
                    Text(result.displayTitle)
                        .font(.body.weight(session?.glyph.needsYou == true ? .semibold : .regular))
                        .foregroundStyle(result.status == .closed ? .secondary : .primary)
                        .lineLimit(2)
                    StateLineText(line: session?.stateLine(now: timeline.date) ?? result.stateLine(now: timeline.date))
                        .font(.subheadline)
                        .lineLimit(1)
                    ForEach(Array(result.snippets.enumerated()), id: \.offset) { _, hit in
                        HStack(alignment: .firstTextBaseline, spacing: 6) {
                            Image(systemName: hit.kind == .prompt ? "person" : "bubble.left")
                                .accessibilityLabel(hit.kind == .prompt ? "Prompt" : "Reply")
                            Text(hit.snippet)
                                .lineLimit(2)
                        }
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(.top, 4)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                RowChevron()
                    .frame(height: 22)
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
            .accessibilityElement(children: .combine)
        }
    }
}

struct StateLineText: View {
    let line: SessionStateLine

    var body: some View {
        let lead = Text(line.lead.map { $0 + " " } ?? "").foregroundStyle(Color.warningInk)
        let text = Text(line.text).monospaced(line.mono).foregroundStyle(color)
        HStack(spacing: 0) {
            Text("\(lead)\(text)")
                .lineLimit(1)
            if let age = line.age {
                Text(" · " + age)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .fixedSize()
            }
        }
    }

    private var color: Color {
        switch line.tone {
        case .primary: .primary
        case .secondary: .secondary
        case .needsYou: Color.warningInk
        case .danger: Color.dangerInk
        }
    }
}

struct SessionGlyphView: View {
    let glyph: SessionGlyph
    var size: CGFloat = 28
    var muted = false

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: size * 0.5, weight: .semibold))
            .foregroundStyle(muted ? .secondary : foreground)
            .frame(width: size, height: size)
            .background(muted ? Color(.tertiarySystemFill) : background, in: .circle)
            .accessibilityHidden(true)
    }

    private var symbol: String {
        switch glyph {
        case .permission: "hand.raised.fill"
        case .question: "questionmark.bubble.fill"
        case .childWaiting: "exclamationmark"
        case .asksParent: "arrow.up"
        case .running: "play.fill"
        case .stopped: "xmark"
        case .review: "eye"
        case .done: "checkmark"
        case .closed: "archivebox"
        }
    }

    private var foreground: Color {
        switch glyph {
        case .permission, .question: Color.onWarning
        case .childWaiting: Color.warningInk
        case .asksParent, .running, .review: .accentColor
        case .stopped: Color.dangerInk
        case .done, .closed: .secondary
        }
    }

    private var background: Color {
        switch glyph {
        case .permission, .question: Color.warning
        case .childWaiting: Color.warning.opacity(0.18)
        case .asksParent, .running, .review: Color.accentColor.opacity(0.18)
        case .stopped: Color.dangerInk.opacity(0.18)
        case .done, .closed: Color(.tertiarySystemFill)
        }
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

struct ConnectionBanner: View {
    let connection: ComputerConnection

    var body: some View {
        Button {
            connection.reconnectNow()
        } label: {
            Label {
                VStack(alignment: .leading, spacing: 2) {
                    Text(connection.state.label)
                    if let detail = connection.offlineDetail {
                        Text(detail).font(.footnote).foregroundStyle(.secondary)
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
    @FocusState private var composerFocused: Bool

    var body: some View {
        ContentUnavailableView("New Session", systemImage: "sparkles", description: Text("Describe the task and attach photos, videos or files. The session starts when you send."))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .contentShape(.rect)
            .onTapGesture { composerFocused = false }
            .safeAreaInset(edge: .bottom) {
                ComposerView(
                    draft: $draft,
                    dictation: AgentsPluginInfo.newSession,
                    attachments: $attachments,
                    focused: $composerFocused,
                    session: nil,
                    enabled: context.connection.state == .online,
                    sending: sending,
                    send: send
                )
                .voiceDock()
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
