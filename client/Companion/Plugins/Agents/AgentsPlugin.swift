import SwiftUI

import CompanionKit

struct AgentsPlugin: CompanionPluginUI {
    let id = AgentsPluginInfo.id
    let title = "Agents"
    let systemImage = "sparkles"
    let requiredCommands = AgentsPluginInfo.requiredCommands
    let tab: CompanionTab? = .agents

    @MainActor
    func makeRoot(context: PluginContext) -> AnyView {
        AnyView(AgentsListView(context: context))
    }

    @MainActor
    func makeEmptyDetail(context: PluginContext) -> AnyView {
        if context.connection.state == .unauthorized {
            return AnyView(ConnectionUnavailableView(connection: context.connection))
        }
        return AnyView(AgentsEmptyDetail(context: context))
    }

    @MainActor
    func makeDestination(_ destination: Destination, context: PluginContext) -> AnyView? {
        let selection: String
        var item: String?
        switch destination {
        case .session(let id), .run(let id):
            selection = id
        case .section(let id, let entry) where id == AgentsPluginInfo.newSession || id == AgentsPluginInfo.openQueue || id.hasPrefix(AgentsPluginInfo.id + "."):
            selection = id
            item = entry
        default:
            return nil
        }
        guard context.connection.state != .unauthorized, let store = Self.store(context) else {
            return makeEmptyDetail(context: context)
        }
        if selection == AgentsPluginInfo.newSession {
            return AnyView(NewSessionView(context: context, store: store))
        }
        if selection == AgentsPluginInfo.openQueue {
            return AnyView(OpenQueueView(context: context, store: store))
        }
        if selection.hasPrefix(AgentsPluginInfo.id + "."), let workspace = context.navigation.workspace(for: context.connection) {
            return AnyView(SectionScreen(connection: context.connection, workspace: workspace.id, section: selection, item: item).id("\(workspace.id)/\(selection)/\(item ?? "")"))
        }
        if GraphRun.isRun(selection) {
            return AnyView(RunDetailView(context: context, store: store, runID: selection).id(selection))
        }
        return AnyView(SessionDetailView(context: context, store: store, sessionID: selection).id(selection))
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        Self.store(context)?.needsInputCount ?? 0
    }

    @MainActor
    static func store(_ context: PluginContext) -> AgentsStore? {
        guard let workspace = context.shownWorkspace else { return nil }
        return context.model.agents(computer: context.connection.computer.id, workspace: workspace.id)
    }

    @MainActor
    static func startNew(_ context: PluginContext) {
        context.navigation.select(AgentsPluginInfo.newSession, tab: .agents)
    }
}

struct AgentsEmptyDetail: View {
    let context: PluginContext

    var body: some View {
        let computer = context.connection.computer.id
        let workspace = context.shownWorkspace
        let waiting = workspace.map { context.model.waitingCount(computer: computer, workspace: $0.id) } ?? 0
        let oldest = workspace.flatMap { workspace in context.model.needsYou().first { $0.computer == computer && $0.workspace.id == workspace.id } }
        ContentUnavailableView {
            Label("No Session Selected", systemImage: "sparkles")
        } description: {
            Text(waiting == 0 ? "Pick a session to read its transcript and reply." : waiting == 1 ? "1 session needs you." : "\(waiting) sessions need you.")
        } actions: {
            if let oldest {
                Button("Open the oldest waiting") { context.navigation.select(oldest.session.id, tab: .agents) }
                    .buttonStyle(.glassProminent)
            } else {
                Button("New session") { AgentsPlugin.startNew(context) }
                    .buttonStyle(.glassProminent)
                    .disabled(workspace == nil || !context.connection.canWrite)
            }
        }
    }
}

struct AgentsListView: View {
    let context: PluginContext
    @Environment(\.tabShown) private var shown

    var body: some View {
        let workspace = context.navigation.workspace(for: context.connection)
        Group {
            if let workspace, let store = AgentsPlugin.store(context) {
                SessionListView(context: context, store: store)
                    .task(id: TabFetch(workspace: workspace.id, shown: shown)) {
                        guard shown else { return }
                        if store.fetched { try? await Task.sleep(for: Motion.navigationSettle) }
                        guard !Task.isCancelled else { return }
                        await store.refreshIfStale()
                    }
            } else if context.connection.state == .online {
                ContentUnavailableView("No Workspaces", systemImage: "folder", description: Text("Open a workspace in Clubmate on \(context.connection.computer.name)."))
            } else {
                ConnectionUnavailableView(connection: context.connection)
            }
        }
        .tabRootChrome(context, title: "Agents") {
            ToolbarItem(placement: .primaryAction) {
                Button("New Session", systemImage: "plus") { AgentsPlugin.startNew(context) }
                    .disabled(workspace == nil || !context.connection.canWrite)
            }
        }
    }
}

struct SessionListView: View {
    let context: PluginContext
    let store: AgentsStore
    @State private var unfolded: Set<SessionGroup> = []

    var body: some View {
        List(selection: context.navigation.listSelection(.agents)) {
            TabListRows {
                ListHeader(context: context)
                OfflineRow(connection: context.connection)
                ListAgeSection { store.ageLine(now: $0) }
                ForEach(store.queue?.notes(computer: context.connection.computer.id) ?? []) { note in
                    Section {
                        QueueNoteRow(note: note) { store.queue?.dismiss(note) }
                            .listRowInsets(EdgeInsets())
                            .columnRow()
                    }
                }
                if !store.pendingCreates.isEmpty {
                    Section("Starting") {
                        ForEach(store.pendingCreates) { item in
                            QueuedLine(item: item, queuedLine: context.connection.queuedLine) { store.queue?.cancel(item) }
                                .selectionDisabled()
                                .columnRow()
                        }
                    }
                }
                if store.searching {
                    found
                } else {
                    sections
                }
            }
        }
        .overlay(alignment: .leading) {
            if context.navigation.menu.state.listStop {
                PaneBar(label: "Session list has keyboard focus", id: "list.focus")
                    .padding(.leading, 4)
            }
        }
        .columnList()
        .tabSearch(.agents, text: Bindable(store).searchQuery, prompt: "Search sessions")
        .overlay {
            if !store.loaded {
                ProgressView()
            } else if store.searching {
                if let error = store.searchError {
                    ContentUnavailableView("Search Failed", systemImage: "magnifyingglass", description: Text(error))
                } else if let results = store.searchResults {
                    if results.isEmpty { ContentUnavailableView.search(text: store.searchQuery) }
                } else {
                    ProgressView()
                }
            } else if store.sessions.isEmpty, !store.olderShown {
                ContentUnavailableView {
                    Label("No Agent Sessions", systemImage: "sparkles")
                } description: {
                    Text(store.error ?? "Start a session and follow it from here.")
                } actions: {
                    Button("New Session", systemImage: "plus") { AgentsPlugin.startNew(context) }
                        .buttonStyle(.glassProminent)
                        .disabled(!context.connection.canWrite)
                }
            }
        }
        .refreshable {
            await store.refresh()
            store.landList()
        }
        .toolbar {
            if let pill = store.hold.pill, !store.searching {
                ToolbarItem(placement: .topBarLeading) {
                    HoldPill(text: pill) { Motion.disclose { store.landList() } }
                }
            }
        }
        .modifier(ListOnScreen(store: store, navigation: context.navigation))
        .onChange(of: store.arrived) { _, arrived in
            guard let arrived else { return }
            store.arrived = nil
            context.navigation.land(arrived)
        }
    }

    private var picked: String? {
        wide ? context.navigation.selected(.agents) : nil
    }

    private var found: some View {
        Section {
            ForEach(store.searchResults ?? []) { result in
                Group {
                    if let session = store.session(result.session) {
                        SessionRow(session: session, chevron: true)
                    } else {
                        FoundSessionRow(result: result)
                    }
                }
                .listRowDensity()
                .tag(result.session)
                .columnRow(selected: picked == result.session)
                .accessibilityIdentifier("row." + result.session)
            }
        }
    }

    @ViewBuilder
    private var sections: some View {
        if store.treeMode {
            tree
        } else {
            stateSections
        }
        endRows
    }

    @ViewBuilder
    private var tree: some View {
        Section {
        } header: {
            FilterChips(counts: store.listCounts, selected: store.listFilter) { filter in
                Motion.disclose {
                    store.listFilter = filter
                    store.landList()
                }
            }
            .textCase(nil)
            .padding(.top, 4)
            .listRowInsets(EdgeInsets())
        }
        let shown = store.shownSections
        ForEach(shown.filter { $0.section.kind == .needsYou }) { listSection($0) }
        queueSections
        ForEach(shown.filter { $0.section.kind != .needsYou }) { listSection($0) }
    }

    @ViewBuilder
    private func listSection(_ shown: CompanionKit.ShownSection) -> some View {
        let section = shown.section
        if section.folds {
            let open = store.unfoldedSections.contains(section.id)
            Section(isExpanded: Binding(get: { open }, set: { _ in })) {
                rows(shown.rows)
                if section.kind == .older {
                    OlderPageRow(loading: store.olderLoading, error: store.olderError, more: store.olderMore, left: store.olderLeft) {
                        Task { await store.loadOlder() }
                    }
                }
            } header: {
                Button {
                    Motion.disclose {
                        if store.unfoldedSections.remove(section.id) == nil { store.unfoldedSections.insert(section.id) }
                    }
                    if section.kind == .older, !open { Task { await store.openOlder() } }
                } label: {
                    HStack {
                        Text("\(section.title(now: .now) ?? "") · \(section.count)")
                        Spacer()
                        Image(systemName: "chevron.right")
                            .rotationEffect(.degrees(open ? 90 : 0))
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
        } else if let title = section.title(now: .now) {
            Section {
                rows(shown.rows)
            } header: {
                HStack(spacing: 10) {
                    Text(title)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Spacer()
                    if section.kind == .open, context.connection.supports(OpenCommand.queue) {
                        Button("Answer all", action: answerAll)
                            .buttonStyle(.borderless)
                            .tint(.accentColor)
                            .fontWeight(.semibold)
                            .contentShape(.rect.inset(by: -12))
                    }
                    Text("\(section.kind == .open ? section.nodes.reduce(0) { $0 + $1.openInTree } : section.nodes.count)")
                }
            }
        } else {
            Section {
                rows(shown.rows)
            }
        }
    }

    private var queueSections: some View {
        ForEach(store.shownQueues) { queue in
            LandQueueSection(queue: queue, store: store) { context.navigation.select($0, tab: .agents) }
        }
    }

    @ViewBuilder
    private var stateSections: some View {
        queueSections
        ForEach(store.groups) { group in
            let items = group.sessions.flatMap { $0.rows(expanded: store.expanded, folded: store.foldedTasks) }.map { ListRow(section: group.id, item: $0, place: nil) }
            let count = group.sessions.count
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
            } else if group.group == .open {
                Section {
                    rows(items)
                } header: {
                    HStack(spacing: 10) {
                        Text(group.group.title)
                        Spacer()
                        if context.connection.supports(OpenCommand.queue) {
                            Button("Answer all", action: answerAll)
                                .buttonStyle(.borderless)
                                .tint(.accentColor)
                                .fontWeight(.semibold)
                                .contentShape(.rect.inset(by: -12))
                        }
                        Text("\(group.sessions.reduce(0) { $0 + $1.openInTree })")
                    }
                }
            } else {
                Section {
                    rows(items)
                } header: {
                    HStack {
                        Text(group.title(now: .now))
                        Spacer()
                        Text("\(count)")
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var endRows: some View {
        if !store.endRows.isEmpty {
            Section {
                ForEach(store.endRows) { row in
                    HStack(spacing: 12) {
                        Image(systemName: row.icon)
                            .foregroundStyle(row.attention ? Color.warningInk : Color.captionInk)
                            .frame(width: 24)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(row.label)
                            if let state = row.state {
                                (Text(state).foregroundStyle(row.attention ? Color.warningInk : Color.captionInk) + Text(row.which.map { " · " + $0 } ?? "").foregroundStyle(Color.captionInk))
                                    .font(.footnote)
                                    .lineLimit(1)
                            }
                        }
                        Spacer()
                        Text(row.detail)
                            .font(.footnote)
                            .foregroundStyle(Color.captionInk)
                    }
                    .listRowDensity()
                    .tag(row.section)
                    .columnRow(selected: picked == row.section)
                }
            }
        }
    }

    private var wide: Bool {
        context.navigation.layout != .compact
    }

    private func answerAll() {
        store.beginOpenPass()
        context.navigation.select(AgentsPluginInfo.openQueue, tab: .agents)
    }

    private func rows(_ rows: [ListRow]) -> some View {
        ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
            let item = row.item
            let nestedBelow = index + 1 < rows.count && rows[index + 1].item.depth > 0
            HStack(spacing: 0) {
                if let repo = item.repo {
                    PendingRepoRow(row: repo)
                } else {
                    SessionRow(session: item.node.session, depth: item.depth, waitingChildren: item.node.waitingDescendants, waitingAsides: item.node.waitingAsides, chevron: item.toggleCount == nil, taskRow: item.taskRow, place: row.place)
                }
                if let count = item.toggleCount, !wide {
                    SubagentToggle(count: count, expanded: item.expanded) {
                        Motion.disclose { store.toggleTree(item) }
                    }
                }
            }
            .listRowDensity()
            .listRowSeparator(item.depth > 0 ? .hidden : .automatic, edges: .top)
            .listRowSeparator(nestedBelow ? .hidden : .automatic, edges: .bottom)
            .tag(item.id)
            .selectionDisabled(item.repo != nil)
            .columnRow(selected: picked == item.id)
            .contextMenu {
                if let count = item.toggleCount, wide {
                    Button(item.expanded ? "Hide \(count) Subagents" : "Show \(count) Subagents", systemImage: item.expanded ? "chevron.up" : "chevron.down") {
                        Motion.disclose { store.toggleTree(item) }
                    }
                }
            }
            .accessibilityIdentifier(row.section.hasPrefix("inbox") ? "inbox." + item.id : "row." + item.id)
            .onAppear { store.visibleRows.insert(row.id) }
            .onDisappear { store.visibleRows.remove(row.id) }
        }
    }
}

private struct ListOnScreen: ViewModifier {
    let store: AgentsStore
    let navigation: AppNavigation
    @Environment(\.scenePhase) private var phase

    func body(content: Content) -> some View {
        content.onChange(of: onScreen, initial: true) { _, shown in
            store.listOnScreen = shown
            if shown { store.landList() }
        }
    }

    private var onScreen: Bool {
        phase != .background && (navigation.layout != .compact || navigation.path(.agents).isEmpty)
    }
}

struct OlderPageRow: View {
    let loading: Bool
    let error: String?
    let more: Bool
    let left: Int
    let load: () -> Void

    var body: some View {
        Group {
            if loading {
                ProgressView()
                    .frame(maxWidth: .infinity, minHeight: 44)
                    .accessibilityIdentifier("older.loading")
            } else if let error {
                HStack(spacing: 12) {
                    Text(error)
                        .font(.footnote)
                        .foregroundStyle(Color.dangerInk)
                        .lineLimit(2)
                    Spacer(minLength: 0)
                    Button("Retry", action: load)
                        .buttonStyle(.borderless)
                        .tint(.accentColor)
                        .fontWeight(.semibold)
                        .accessibilityIdentifier("older.retry")
                }
                .frame(minHeight: 44)
            } else if more {
                Button(action: load) {
                    HStack(spacing: 6) {
                        Image(systemName: "chevron.down")
                            .font(.caption.weight(.semibold))
                        Text(left > AgentsTree.olderPage ? "Show \(AgentsTree.olderPage) more · \(left) left" : left > 0 ? "Show \(left) more" : "Show more")
                    }
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tint)
                    .frame(maxWidth: .infinity, minHeight: 44)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("older.more")
            }
        }
        .listRowDensity()
        .selectionDisabled()
    }
}

struct FilterChips: View {
    let counts: [ListFilter: Int]
    let selected: ListFilter
    let pick: (ListFilter) -> Void

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                ForEach(ListFilter.allCases) { filter in
                    let count = counts[filter] ?? 0
                    let on = filter == selected
                    Button { pick(filter) } label: {
                        HStack(spacing: 6) {
                            Text(filter.title)
                                .fontWeight(on ? .semibold : .regular)
                                .foregroundStyle(.primary)
                            Text("\(count)")
                                .monospacedDigit()
                                .fontWeight(filter == .needs && count > 0 ? .semibold : .regular)
                                .foregroundStyle(filter == .needs && count > 0 ? Color.warningInk : Color(.tertiaryLabel))
                        }
                        .font(.subheadline)
                        .padding(.horizontal, 12)
                        .frame(minHeight: 32)
                        .background(Capsule().fill(on ? Color(.tertiarySystemFill) : Color(.quaternarySystemFill)))
                        .contentShape(.capsule)
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(on ? .isSelected : [])
                    .accessibilityIdentifier("chip." + filter.rawValue)
                }
            }
        }
        .scrollIndicators(.hidden)
        .scrollClipDisabled()
    }
}

struct HoldPill: View {
    let text: String
    let land: () -> Void

    var body: some View {
        Button(action: land) {
            HStack(spacing: 6) {
                Image(systemName: "arrow.down.to.line")
                    .foregroundStyle(Color.accentColor)
                Text(text)
                    .fixedSize()
            }
            .font(.footnote.weight(.semibold))
            .lineLimit(1)
            .padding(.horizontal, 4)
            .frame(minHeight: 36)
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("list.hold")
        .accessibilityHint("Shows the waiting changes")
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
            .foregroundStyle(Color.captionInk)
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.borderless)
        .tint(Color.captionInk)
        .accessibilityLabel(expanded ? "Hide \(count) subagents" : "Show \(count) subagents")
    }
}

struct SessionRow: View {
    let session: AgentSession
    var depth = 0
    var waitingChildren = 0
    var waitingAsides = 0
    var chevron = false
    var taskRow: RepoRow?
    var place: String?

    var body: some View {
        TimelineView(.periodic(from: .now, by: 30)) { timeline in
            HStack(alignment: .top, spacing: 12) {
                SessionGlyphView(glyph: session.glyph(waitingChildren: waitingChildren), size: depth > 0 ? 24 : 28, review: session.gateRound != nil)
                VStack(alignment: .leading, spacing: 2) {
                    if depth > 0, session.repo == nil, session.gateRound == nil {
                        Text(session.subagentLabel)
                            .font(.subheadline)
                            .foregroundStyle(Color.captionInk)
                    }
                    HStack(alignment: .top, spacing: 8) {
                        if depth > 0, let repo = session.repo {
                            RepoName(name: repo, font: .body.weight(session.glyph.needsYou ? .semibold : .regular))
                                .frame(maxWidth: .infinity, alignment: .leading)
                        } else {
                            Text(session.displayTitle)
                                .font(.body.weight(session.glyph.needsYou ? .semibold : .regular))
                                .foregroundStyle(session.isClosed ? Color.captionInk : .primary)
                                .lineLimit(2)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        if session.glyph == .open, let label = session.openLabel {
                            OpenCountChip(text: label)
                                .frame(height: 22)
                        }
                    }
                    if let word = taskRow?.word {
                        Text(word)
                            .font(.subheadline)
                            .foregroundStyle(taskRow?.tone.toneInk(default: Color.captionInk) ?? Color.captionInk)
                            .lineLimit(1)
                    } else {
                        StateLineText(line: placed(session.stateLine(now: timeline.date, waitingChildren: waitingChildren, waitingAsides: waitingAsides)))
                            .font(.subheadline)
                            .lineLimit(1)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if chevron, session.glyph != .open {
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

    private func placed(_ line: SessionStateLine) -> SessionStateLine {
        guard let place else { return line }
        var line = line
        line.age = place
        return line
    }
}

struct FoundSessionRow: View {
    let result: AgentSearchResult

    var body: some View {
        TimelineView(.periodic(from: .now, by: 30)) { timeline in
            HStack(alignment: .top, spacing: 12) {
                SessionGlyphView(glyph: result.glyph)
                VStack(alignment: .leading, spacing: 2) {
                    Text(result.displayTitle)
                        .foregroundStyle(result.status == .closed ? Color.captionInk : .primary)
                        .lineLimit(2)
                    StateLineText(line: result.stateLine(now: timeline.date))
                        .font(.subheadline)
                        .lineLimit(1)
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
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
                    .fixedSize()
            }
        }
    }

    private var color: Color {
        switch line.tone {
        case .primary: .primary
        case .secondary: Color.captionInk
        case .faint: Color(.tertiaryLabel)
        case .needsYou: Color.warningInk
        case .danger: Color.dangerInk
        case .accent: .accentColor
        case .success: Color.successInk
        }
    }
}

struct SessionGlyphView: View {
    let glyph: SessionGlyph
    var size: CGFloat = 28
    var muted = false
    var review = false

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: size * 0.5, weight: .semibold))
            .foregroundStyle(muted ? Color.captionInk : foreground)
            .frame(width: size, height: size)
            .background(muted ? Color(.tertiarySystemFill) : background, in: shape)
            .accessibilityHidden(true)
    }

    private var shape: AnyShape {
        glyph == .step ? AnyShape(.rect(cornerRadius: size * 0.22)) : AnyShape(.circle)
    }

    private var symbol: String {
        if review, glyph == .permission { return "exclamationmark.shield.fill" }
        return switch glyph {
        case .connect: "folder.fill.badge.plus"
        case .permission: "hand.raised.fill"
        case .question: "questionmark.bubble.fill"
        case .step: "dollarsign"
        case .cap: "exclamationmark"
        case .childWaiting: "exclamationmark"
        case .asksParent: "arrow.up"
        case .running: "play.fill"
        case .stopped: "xmark"
        case .paused: "pause.fill"
        case .review: "eye"
        case .open, .done: "checkmark"
        case .closed: "archivebox"
        case .aside: "bubble.left.and.bubble.right"
        case .gate: "exclamationmark"
        case .held: "shield.fill"
        }
    }

    private var foreground: Color {
        switch glyph {
        case .connect, .permission, .question, .step, .cap, .gate: Color.onWarning
        case .childWaiting: Color.warningInk
        case .asksParent, .running, .review, .held: .accentColor
        case .stopped: Color.dangerInk
        case .open: Color.successInk
        case .paused, .done, .closed, .aside: Color.captionInk
        }
    }

    private var background: Color {
        switch glyph {
        case .connect, .permission, .question, .step, .cap, .gate: Color.warning
        case .childWaiting: Color.warning.opacity(0.18)
        case .asksParent, .running, .review, .held: Color.accentColor.opacity(0.18)
        case .stopped: Color.dangerInk.opacity(0.18)
        case .paused, .open, .done, .closed, .aside: Color(.tertiarySystemFill)
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
                        .foregroundStyle(connection.state == .connecting ? Color.captionInk : Color.primary)
                    if let detail = connection.offlineDetail {
                        Text(detail).font(.footnote).foregroundStyle(Color.captionInk)
                    }
                }
            } icon: {
                Image(systemName: connection.state == .unauthorized ? "lock.slash" : "wifi.slash")
                    .foregroundStyle(Color.captionInk)
            }
        }
        .disabled(connection.state == .connecting)
    }
}

struct ListAgeSection: View {
    let line: (Date) -> String?
    @State private var now = Date.now

    var body: some View {
        Group {
            if let text = line(now) {
                Section {
                    Label(text, systemImage: "clock.arrow.circlepath")
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .accessibilityIdentifier("listAge")
                }
            }
        }
        .onReceive(Timer.publish(every: 30, on: .main, in: .common).autoconnect()) { now = $0 }
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
                Text("\(connection.computer.name) no longer accepts this phone. Pair it again from Clubmate Settings › Remote.")
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
    @State private var composerFocused = false

    var body: some View {
        ContentUnavailableView("New Session", systemImage: "sparkles", description: Text("Describe the task and attach photos, videos or files. The session starts when you send."))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .contentShape(.rect)
            .onTapGesture { if composerFocused { composerFocused = false } }
            .safeAreaInset(edge: .bottom) {
                ComposerView(
                    draft: $draft,
                    dictation: AgentsPluginInfo.newSession,
                    attachments: $attachments,
                    focused: $composerFocused,
                    session: nil,
                    enabled: context.connection.canWrite,
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
                    context.navigation.replaceTop(.session(id))
                } else {
                    draft = ""
                    attachments = []
                    context.navigation.pop()
                }
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
            }
            sending = false
        }
    }
}
