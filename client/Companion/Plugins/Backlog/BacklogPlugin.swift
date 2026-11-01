import SwiftUI

import ClubmateDocument
import CompanionKit

struct BacklogPlugin: CompanionPluginUI {
    let id = BacklogPluginInfo.id
    let title = "Backlog"
    let systemImage = "checklist"
    let requiredCommands = BacklogPluginInfo.requiredCommands
    let inWorkspace = true

    @MainActor
    func makeList(context: PluginContext) -> AnyView {
        AnyView(BacklogListView(context: context))
    }

    @MainActor
    func makeDetail(context: PluginContext, selection: String?) -> AnyView {
        if context.connection.state == .unauthorized {
            return AnyView(ConnectionUnavailableView(connection: context.connection))
        }
        guard let selection, let store = Self.store(context) else {
            return AnyView(ContentUnavailableView("No Task Selected", systemImage: "checklist", description: Text("Pick a task to read it, start it or mark it done.")))
        }
        if selection == BacklogPluginInfo.newTask {
            return AnyView(NewTaskView(context: context, store: store))
        }
        guard let id = Int(selection) else {
            return AnyView(ContentUnavailableView("Task Not Found", systemImage: "checklist"))
        }
        return AnyView(TaskDetailView(context: context, store: store, taskID: id).id(selection))
    }

    @MainActor
    func badge(context: PluginContext) -> Int {
        0
    }

    @MainActor
    static func store(_ context: PluginContext) -> BacklogStore? {
        guard let workspace = context.navigation.workspace(for: context.connection) else { return nil }
        return context.model.backlog(computer: context.connection.computer.id, workspace: workspace.id)
    }

    @MainActor
    static func startNew(_ context: PluginContext) {
        context.navigation.detail = BacklogPluginInfo.newTask
        context.navigation.preferredColumn = .detail
    }

    @MainActor
    static func owner(_ task: BacklogTask, context: PluginContext) -> String? {
        guard let owner = task.claimedBy else { return nil }
        if task.claimedByUser { return "you" }
        guard AgentSessionID.matches(owner), let workspace = context.navigation.workspace(for: context.connection),
              let session = context.model.agents(computer: context.connection.computer.id, workspace: workspace.id)?.session(owner) else { return owner }
        return "\(session.agentName) · \(session.displayTitle)"
    }
}

struct BacklogListView: View {
    let context: PluginContext

    var body: some View {
        let workspace = context.navigation.workspace(for: context.connection)
        Group {
            if let workspace, let store = BacklogPlugin.store(context) {
                TaskListView(context: context, store: store)
                    .task(id: workspace.id) { await store.refresh() }
            } else if context.connection.state == .online {
                ContentUnavailableView("No Workspaces", systemImage: "folder", description: Text("Open a workspace in Clubmate on \(context.connection.computer.name)."))
            } else {
                ConnectionUnavailableView(connection: context.connection)
            }
        }
        .workspaceChrome(context, title: workspace?.name ?? "Backlog")
        .toolbar {
            WorkspaceMenu(context: context)
            ToolbarItem(placement: .primaryAction) {
                Button("New Task", systemImage: "plus") { BacklogPlugin.startNew(context) }
                    .disabled(workspace == nil || context.connection.state != .online)
            }
        }
    }
}

struct TaskListView: View {
    let context: PluginContext
    let store: BacklogStore
    @State private var expanded: Set<BacklogStatus> = []
    @State private var failure: String?

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
            ForEach(store.groups, id: \.status) { group in
                if BacklogStatus.folded.contains(group.status) {
                    Section(isExpanded: Binding(get: { expanded.contains(group.status) }, set: { open in
                        if open { expanded.insert(group.status) } else { expanded.remove(group.status) }
                    })) {
                        rows(group.tasks)
                    } header: {
                        Button {
                            Motion.disclose { toggle(group.status) }
                        } label: {
                            HStack {
                                Text("\(group.status.label) · \(group.tasks.count)")
                                Spacer()
                                Image(systemName: "chevron.right")
                                    .rotationEffect(.degrees(expanded.contains(group.status) ? 90 : 0))
                            }
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                    }
                } else {
                    Section {
                        rows(group.tasks)
                    } header: {
                        HStack {
                            Text(group.status.label)
                            Spacer()
                            Text("\(group.tasks.count)")
                        }
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .overlay {
            if !store.loaded {
                ProgressView()
            } else if store.tasks.isEmpty {
                ContentUnavailableView {
                    Label("No Tasks", systemImage: "checklist")
                } description: {
                    Text(store.error ?? "Add a task here or in Clubmate; agents pick up Todo tasks.")
                } actions: {
                    Button("New Task", systemImage: "plus") { BacklogPlugin.startNew(context) }
                        .buttonStyle(.glassProminent)
                        .disabled(context.connection.state != .online)
                }
            }
        }
        .refreshable { await store.refresh() }
        .actionFailure($failure)
    }

    private func rows(_ tasks: [BacklogTask]) -> some View {
        ForEach(tasks) { task in
            TaskRow(task: task, owner: BacklogPlugin.owner(task, context: context))
                .tag(String(task.id))
                .swipeActions(edge: .trailing) {
                    if task.status != .done {
                        Button("Done", systemImage: "checkmark") { perform { try await store.done(task.id) } }
                            .tint(.green)
                    }
                }
                .swipeActions(edge: .leading) {
                    if task.status == .todo || task.status == .idea {
                        Button("Start", systemImage: "play.fill") { perform { try await store.claim(task.id) } }
                            .tint(.accentColor)
                    } else if task.status == .inProgress {
                        Button("Release", systemImage: "arrow.uturn.backward") { perform { try await store.release(task.id) } }
                    }
                }
                .contextMenu {
                    TaskActions(task: task, store: store, failure: $failure)
                }
        }
    }

    private func toggle(_ status: BacklogStatus) {
        if expanded.contains(status) { expanded.remove(status) } else { expanded.insert(status) }
    }

    private func perform(_ action: @escaping @MainActor () async throws -> Void) {
        attempt($failure, action)
    }
}

struct TaskActions: View {
    let task: BacklogTask
    let store: BacklogStore
    @Binding var failure: String?

    var body: some View {
        if task.status == .todo || task.status == .idea {
            Button("Start", systemImage: "play") { perform { try await store.claim(task.id) } }
        }
        if task.status == .inProgress {
            Button("Release", systemImage: "arrow.uturn.backward") { perform { try await store.release(task.id) } }
        }
        if task.status != .done {
            Button("Mark Done", systemImage: "checkmark.circle") { perform { try await store.done(task.id) } }
        }
        Menu("Move to", systemImage: "arrow.right.circle") {
            ForEach(BacklogStatus.moveTargets.filter { $0 != task.status }) { status in
                Button(status.label, systemImage: status.symbol) { perform { try await store.move(task.id, to: status) } }
            }
        }
        Menu("Priority", systemImage: "flag") {
            Picker("Priority", selection: Binding(get: { task.priority }, set: { priority in
                perform { try await store.update(task.id, ["priority": .string(priority.rawValue)]) }
            })) {
                ForEach(BacklogPriority.allCases) { priority in
                    Text(priority.label).tag(priority)
                }
            }
        }
    }

    private func perform(_ action: @escaping @MainActor () async throws -> Void) {
        attempt($failure, action)
    }
}

struct TaskRow: View {
    let task: BacklogTask
    let owner: String?

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            TaskStatusIcon(status: task.status)
            VStack(alignment: .leading, spacing: 2) {
                Text("\(PriorityTag(priority: task.priority).text)\(task.title)")
                    .lineLimit(2)
                Text(subtitle)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            RowChevron()
                .frame(height: 22)
        }
        .frame(minHeight: 44)
        .listRowDensity()
    }

    private var subtitle: String {
        let detail = task.status == .inProgress ? owner ?? task.noteLine : task.noteLine
        return ([task.number] + [detail].compactMap { $0 }).joined(separator: " · ")
    }
}

struct TaskStatusIcon: View {
    let status: BacklogStatus

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: 14, weight: .semibold))
            .foregroundStyle(status == .inProgress ? Color.accentColor : .secondary)
            .frame(width: 28, height: 28)
            .background(status == .inProgress ? Color.accentColor.opacity(0.18) : Color(.tertiarySystemFill), in: .circle)
            .accessibilityLabel(status.label)
    }

    private var symbol: String {
        switch status {
        case .inProgress: "play"
        case .done: "checkmark"
        default: status.symbol
        }
    }
}

struct PriorityTag: View {
    let priority: BacklogPriority

    var text: Text {
        guard let glyph = priority.glyph else { return Text("") }
        return Text(glyph + " ").foregroundStyle(.secondary)
    }

    var body: some View {
        if priority.glyph != nil {
            text
                .accessibilityLabel("\(priority.label) priority")
        }
    }
}

private struct RenderedDescription: Equatable {
    let html: String?
}

struct TaskDetailView: View {
    let context: PluginContext
    let store: BacklogStore
    let taskID: Int
    @State private var editing = false
    @State private var failure: String?
    @State private var rendered: RenderedDescription?
    @Environment(\.colorScheme) private var scheme
    @Environment(\.openURL) private var openURL

    var body: some View {
        Group {
            if let task = store.task(taskID) {
                content(task)
                    .toolbar {
                        ToolbarItem(placement: .primaryAction) {
                            Button("Edit") { editing = true }
                                .disabled(context.connection.state != .online)
                        }
                        ToolbarItem(placement: .primaryAction) {
                            Menu("More", systemImage: "ellipsis") {
                                TaskActions(task: task, store: store, failure: $failure)
                            }
                            .disabled(context.connection.state != .online)
                        }
                    }
                    .sheet(isPresented: $editing) {
                        NavigationStack {
                            TaskEditorView(store: store, task: task) { _ in editing = false }
                        }
                    }
            } else if store.loaded {
                ContentUnavailableView("Task Not Found", systemImage: "checklist", description: Text("It was deleted or moved out of this workspace."))
            } else {
                ProgressView()
            }
        }
        .navigationTitle(store.task(taskID)?.number ?? "Task")
        .navigationBarTitleDisplayMode(.inline)
        .actionFailure($failure)
    }

    private func content(_ task: BacklogTask) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                HStack(spacing: 8) {
                    Label(task.status.label, systemImage: task.status.symbol)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.secondary)
                    PriorityTag(priority: task.priority)
                }
                Text(task.title)
                    .font(.title2.bold())
                    .textSelection(.enabled)
                if task.status == .inProgress, let owner = BacklogPlugin.owner(task, context: context) {
                    Label("In progress by \(owner)", systemImage: "person.crop.circle")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
                buttons(task)
                if !task.note.isEmpty {
                    Text(task.note)
                        .font(.callout)
                        .textSelection(.enabled)
                        .padding(12)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(.quaternary.opacity(0.5), in: .rect(cornerRadius: 12))
                }
                if !task.description.isEmpty, let rendered {
                    if let html = rendered.html {
                        DocumentWebView(content: DocumentContent(path: "", html: html, rev: html.hashValue), theme: .system(dark: scheme == .dark), size: .fit) { link in
                            if case .external(let url) = link { openURL(url) }
                        }
                    } else {
                        MarkdownView(text: task.description)
                            .textSelection(.enabled)
                    }
                }
                Text(footer(task))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .padding()
            .frame(maxWidth: 720, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .refreshable { await store.refresh() }
        .task(id: task.description) {
            guard !task.description.isEmpty else { return }
            let html = (try? await store.get(task.id))?.descriptionHtml
            guard !Task.isCancelled else { return }
            rendered = RenderedDescription(html: html)
        }
    }

    @ViewBuilder
    private func buttons(_ task: BacklogTask) -> some View {
        let online = context.connection.state == .online
        HStack(spacing: 10) {
            switch task.status {
            case .todo, .idea:
                Button("Start", systemImage: "play.fill") { perform { try await store.claim(task.id) } }
                    .buttonStyle(.glassProminent)
                Button("Done", systemImage: "checkmark") { perform { try await store.done(task.id) } }
                    .buttonStyle(.glass)
            case .inProgress:
                Button("Done", systemImage: "checkmark") { perform { try await store.done(task.id) } }
                    .buttonStyle(.glassProminent)
                Button("Release", systemImage: "arrow.uturn.backward") { perform { try await store.release(task.id) } }
                    .buttonStyle(.glass)
            case .notNow:
                Button("Move to Todo", systemImage: "circle") { perform { try await store.move(task.id, to: .todo) } }
                    .buttonStyle(.glass)
            case .done:
                Button("Reopen", systemImage: "arrow.counterclockwise") { perform { try await store.move(task.id, to: .todo) } }
                    .buttonStyle(.glass)
            case .other:
                EmptyView()
            }
        }
        .disabled(!online)
    }

    private func footer(_ task: BacklogTask) -> String {
        var parts: [String] = []
        if let created = task.createdAt {
            let by = task.createdBy == "user" ? "you" : task.createdBy.isEmpty ? nil : task.createdBy
            parts.append("Added \(created.formatted(.relative(presentation: .named)))" + (by.map { " by \($0)" } ?? ""))
        }
        if let updated = task.updatedAt, updated != task.createdAt {
            parts.append("updated \(updated.formatted(.relative(presentation: .named)))")
        }
        return parts.joined(separator: " · ")
    }

    private func perform(_ action: @escaping @MainActor () async throws -> Void) {
        attempt($failure, action)
    }
}

struct NewTaskView: View {
    let context: PluginContext
    let store: BacklogStore

    var body: some View {
        TaskEditorView(store: store, task: nil) { id in
            context.navigation.detail = id.map(String.init)
        }
    }
}

struct TaskEditorView: View {
    let store: BacklogStore
    let task: BacklogTask?
    let finished: (Int?) -> Void
    @State private var draft: BacklogDraft
    @State private var saving = false
    @State private var failure: String?
    @FocusState private var titleFocused: Bool

    init(store: BacklogStore, task: BacklogTask?, finished: @escaping (Int?) -> Void) {
        self.store = store
        self.task = task
        self.finished = finished
        _draft = State(initialValue: task.map(BacklogDraft.init) ?? BacklogDraft())
    }

    var body: some View {
        Form {
            Section {
                TextField("Title", text: $draft.title)
                    .focused($titleFocused)
                    .submitLabel(.next)
                    .accessibilityIdentifier("taskTitle")
                TextField("Description (Markdown)", text: $draft.description, axis: .vertical)
                    .lineLimit(4...14)
            }
            Section {
                Picker("Priority", selection: $draft.priority) {
                    ForEach(BacklogPriority.allCases) { priority in
                        Text(priority.label).tag(priority)
                    }
                }
                .pickerStyle(.segmented)
                if task?.status == .inProgress {
                    LabeledContent("Status", value: BacklogStatus.inProgress.label)
                } else {
                    Picker("Status", selection: $draft.status) {
                        ForEach(task == nil ? BacklogStatus.addable : BacklogStatus.moveTargets + [.done]) { status in
                            Label(status.label, systemImage: status.symbol).tag(status)
                        }
                    }
                }
            } footer: {
                if task == nil {
                    Text("Agents pick up Todo tasks; Idea and Not now wait for you.")
                }
            }
        }
        .navigationTitle(task.map { "Edit \($0.number)" } ?? "New Task")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if task != nil {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { finished(nil) }
                }
            }
            ToolbarItem(placement: .confirmationAction) {
                if saving {
                    ProgressView()
                } else {
                    Button(task == nil ? "Add" : "Save") { save() }
                        .disabled(!draft.isValid)
                }
            }
        }
        .onAppear { if task == nil { titleFocused = true } }
        .actionFailure($failure)
    }

    private func save() {
        saving = true
        Task {
            do {
                if let task {
                    try await store.update(task.id, draft.changes(from: task))
                    finished(task.id)
                } else {
                    finished(try await store.add(draft)?.id)
                }
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
            }
            saving = false
        }
    }
}

@MainActor
func attempt(_ failure: Binding<String?>, _ action: @escaping @MainActor () async throws -> Void) {
    Task {
        do {
            try await action()
        } catch {
            failure.wrappedValue = (error as? RemoteError)?.message ?? error.localizedDescription
        }
    }
}

extension View {
    func actionFailure(_ failure: Binding<String?>) -> some View {
        alert("Something Went Wrong", isPresented: Binding(get: { failure.wrappedValue != nil }, set: { if !$0 { failure.wrappedValue = nil } })) {
            Button("OK") { failure.wrappedValue = nil }
        } message: {
            Text(failure.wrappedValue ?? "")
        }
    }
}
