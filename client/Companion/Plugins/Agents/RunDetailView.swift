import SwiftUI

import CompanionKit

struct RunDetailView: View {
    let context: PluginContext
    let store: AgentsStore
    let runID: String
    @Environment(AppNavigation.self) private var navigation
    @State private var confirmingStop = false
    @State private var failure: String?
    @State private var position = ScrollPosition(edge: .top)

    private static let stepNote = "Square marks a step: Clubmate runs the command itself. Round is an agent."
    private static let followInterval: Duration = .seconds(5)

    var body: some View {
        let run = store.runs[runID]
        let entry = store.session(runID)
        let request = run?.request(entry)
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                if let run {
                    if !run.inputLine.isEmpty {
                        Text(run.inputLine)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                            .padding(.horizontal, 4)
                    }
                    TimelineView(.periodic(from: .now, by: 30)) { timeline in
                        RunNodesCard(lines: run.lines(now: timeline.date))
                    }
                    Text(Self.stepNote)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 4)
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 12)
            .frame(maxWidth: 820)
            .frame(maxWidth: .infinity)
        }
        .scrollPosition($position)
        .contentMargins(.bottom, navigation.floatingMic(context.model) && request == nil ? VoiceButton.floating + 16 : 0, for: .scrollContent)
        .scrollEdgeEffectStyle(.soft, for: .top)
        .background(Color(.systemGroupedBackground))
        .safeAreaInset(edge: .top, spacing: 0) {
            RequestBannerHost()
                .padding(.horizontal)
                .padding(.vertical, 6)
                .frame(maxWidth: 820)
                .frame(maxWidth: .infinity)
                .background(Color(.systemGroupedBackground))
        }
        .overlay {
            if run == nil, let error = store.runErrors[runID] {
                ContentUnavailableView {
                    Label("Couldn't Load the Run", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(error)
                } actions: {
                    Button("Try Again") { Task { await store.loadRun(runID) } }
                }
            } else if run == nil {
                ProgressView()
            }
        }
        .safeAreaInset(edge: .bottom) {
            VStack(spacing: 10) {
                if let run, let request {
                    RunRequestCard(request: request, graph: run.title, blocked: context.connection.blocked) { decision in
                        guard case .step(let node, _, _, _, _) = request else { return }
                        try await store.approve(run: runID, node: node, decision: decision)
                    } proceed: { edge in
                        try await store.continueRun(runID, edge: edge)
                    } stop: {
                        try await store.stopRun(runID)
                    }
                    .id(request.id)
                    .padding(.horizontal, 12)
                    .frame(maxWidth: 820)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
            .animation(.snappy, value: request)
            .padding(.bottom, navigation.floatingMic(context.model) && request != nil ? VoiceButton.floating + 8 : 0)
        }
        .navigationTitle(title(run, entry))
        .toolbarTitleDisplayMode(wide ? .inlineLarge : .inline)
        .toolbar {
            if wide {
                ToolbarItem(placement: .largeSubtitle) {
                    subtitle(run, entry)
                        .font(.subheadline)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            } else {
                ToolbarItem(placement: .principal) {
                    VStack(spacing: 1) {
                        Text(title(run, entry))
                            .font(.headline)
                            .lineLimit(1)
                        subtitle(run, entry)
                            .font(.footnote)
                            .lineLimit(1)
                    }
                    .frame(maxWidth: 260)
                    .accessibilityElement(children: .combine)
                }
            }
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
                    Button("Refresh", systemImage: "arrow.clockwise") {
                        Task { await store.loadRun(runID) }
                    }
                    if run?.running == true {
                        Button("Stop Run…", systemImage: "stop.circle", role: .destructive) { confirmingStop = true }
                            .disabled(context.connection.blocked != nil)
                    }
                }
            }
        }
        .confirmationDialog("Stop this run?", isPresented: $confirmingStop, titleVisibility: .visible) {
            Button("Stop Run", role: .destructive) {
                Task {
                    do {
                        try await store.stopRun(runID)
                    } catch {
                        failure = (error as? RemoteError)?.message ?? error.localizedDescription
                    }
                }
            }
        } message: {
            Text("The run ends here. Its sessions stay readable.")
        }
        .alert("Something Went Wrong", isPresented: Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })) {
            Button("OK") { failure = nil }
        } message: {
            Text(failure ?? "")
        }
        .onScrollGeometryChange(for: CGFloat.self) { geometry in
            max(geometry.contentSize.height - geometry.containerSize.height, 0)
        } action: { _, bottom in
            guard request != nil else { return }
            position.scrollTo(y: bottom)
        }
        .task(id: runID) {
            store.watchRun(runID)
            await store.loadRun(runID)
            while !Task.isCancelled {
                try? await Task.sleep(for: Self.followInterval)
                if Task.isCancelled { break }
                if store.runs[runID]?.nodes.contains(where: { $0.isStep && $0.status == "running" }) == true {
                    await store.loadRun(runID)
                }
            }
            store.unwatchRun(runID)
        }
    }

    private var wide: Bool {
        navigation.layout != .compact
    }

    private func title(_ run: GraphRun?, _ entry: AgentSession?) -> String {
        run?.displayTitle ?? entry?.displayTitle ?? "Graph Run"
    }

    private func subtitle(_ run: GraphRun?, _ entry: AgentSession?) -> Text {
        let needsYou = entry?.needsYou == true
        let state = run?.headerState(needsYou: needsYou) ?? (needsYou ? SessionState.waitingForYou.label : "")
        let mac = context.model.tag(context.connection.computer.id)
        let workspace = context.navigation.workspace(for: context.connection)?.name ?? ""
        return Text(state).foregroundStyle(needsYou ? Color.warning : .secondary)
            + (mac != nil || !workspace.isEmpty ? Text(" · ") + mac.lead(workspace) : Text("")).foregroundStyle(.secondary)
    }
}

struct RunNodesCard: View {
    let lines: [RunNodeLine]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(lines.enumerated()), id: \.element.id) { index, line in
                if index > 0 {
                    Divider()
                }
                RunNodeRow(line: line)
            }
        }
        .background(Color.surface1, in: .rect(cornerRadius: 20))
    }
}

struct RunNodeRow: View {
    let line: RunNodeLine

    var body: some View {
        HStack(spacing: 12) {
            RunNodeBadge(line: line)
            VStack(alignment: .leading, spacing: 2) {
                Text(line.role)
                    .font(.body.weight(.semibold))
                    .lineLimit(1)
                Text(line.detail)
                    .font(line.step ? .subheadline.monospaced() : .subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .layoutPriority(1)
            VStack(alignment: .trailing, spacing: 2) {
                Text(line.word)
                    .font(.subheadline)
                    .foregroundStyle(line.tone.color)
                if let time = line.time {
                    Text(time)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
            }
            .lineLimit(1)
            .fixedSize()
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 11)
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(line.role), \(line.step ? "step" : "agent"), \(line.word)")
    }
}

struct RunNodeBadge: View {
    let line: RunNodeLine
    var size: CGFloat = 28

    var body: some View {
        let waiting = line.tone == .warning
        Group {
            if line.step {
                Text("$")
                    .font(.system(size: size * 0.55, weight: .bold, design: .rounded))
                    .foregroundStyle(waiting ? Color.onWarning : .secondary)
                    .frame(width: size, height: size)
                    .background(waiting ? Color.warning : Color(.tertiarySystemFill), in: .rect(cornerRadius: size * 0.22))
            } else {
                Image(systemName: symbol)
                    .font(.system(size: size * 0.55, weight: .semibold))
                    .foregroundStyle(waiting ? Color.onWarning : line.tone.color)
                    .frame(width: size, height: size)
                    .background(waiting ? Color.warning : Color(.tertiarySystemFill), in: .circle)
            }
        }
        .accessibilityHidden(true)
    }

    private var symbol: String {
        switch line.tone {
        case .success: "checkmark.circle"
        case .accent: "play.circle"
        case .warning: "hand.raised.fill"
        case .danger: "xmark.circle"
        case .secondary: "circle"
        }
    }
}

extension RunNodeLine.Tone {
    var color: Color {
        switch self {
        case .success: Color.successInk
        case .warning: Color.warningInk
        case .danger: Color.dangerInk
        case .accent: .accentColor
        case .secondary: .secondary
        }
    }
}

struct RunRequestCard: View {
    let request: RunRequest
    let graph: String
    let blocked: String?
    let approve: @MainActor (String) async throws -> Void
    let proceed: @MainActor (Int) async throws -> Void
    let stop: @MainActor () async throws -> Void
    @State private var chosen: String?
    @State private var failure: String?
    @State private var armed = false
    @State private var confirmingAlways = false
    @State private var confirmingStop = false
    @State private var commandHeight: CGFloat = 0

    var body: some View {
        DockCard {
            VStack(alignment: .leading, spacing: 12) {
                switch request {
                case .step(_, let role, let command, let place, let rule):
                    header(symbol: nil, title: "Run a command?", detail: "\(role) · a step of \(graph)")
                    ScrollView {
                        CommandText(text: command)
                            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { commandHeight = $0 }
                    }
                    .scrollBounceBehavior(.basedOnSize)
                    .frame(height: min(max(commandHeight, 44), 160))
                    .background(Color(.systemGroupedBackground), in: .rect(cornerRadius: 16))
                    Text(blocked ?? place)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                    VStack(spacing: 10) {
                        PrimaryCapsule(title: "Allow once") { pick("once") { try await approve("once") } }
                        HStack(spacing: 10) {
                            if rule != nil {
                                PlainCapsule(title: "Always allow…", tint: .primary) { confirmingAlways = true }
                            }
                            PlainCapsule(title: "Reject", tint: Color.danger) { pick("reject") { try await approve("reject") } }
                        }
                    }
                    .disabled(!answerable)
                    .opacity(enabled ? 1 : 0.5)
                    caption("Reject ends the step with error · the run stops")
                case .cap(let edge, let text):
                    header(symbol: "exclamationmark", title: text, detail: "a round cap of \(graph)")
                    if let blocked {
                        Text(blocked)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    VStack(spacing: 10) {
                        PrimaryCapsule(title: "Continue") { pick("continue") { try await proceed(edge) } }
                        PlainCapsule(title: "Stop run", tint: Color.danger) { confirmingStop = true }
                    }
                    .disabled(!answerable)
                    .opacity(enabled ? 1 : 0.5)
                    caption("Continue allows one more round")
                }
                if let failure {
                    Label(failure, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(Color.danger)
                }
            }
        }
        .task {
            try? await Task.sleep(for: .milliseconds(600))
            armed = true
        }
        .focusedSceneValue(\.sessionKeys, keys)
        .sheet(isPresented: $confirmingAlways) {
            if case .step(_, let role, _, _, let rule?) = request {
                RunAlwaysAllowSheet(rule: rule, role: role) {
                    confirmingAlways = false
                    pick("always") { try await approve("always") }
                }
            }
        }
        .confirmationDialog("Stop this run?", isPresented: $confirmingStop, titleVisibility: .visible) {
            Button("Stop Run", role: .destructive) { pick("stop") { try await stop() } }
        } message: {
            Text("The run ends here. Its sessions stay readable.")
        }
    }

    private var enabled: Bool {
        blocked == nil
    }

    private var keys: SessionKeyActions {
        switch request {
        case .step(_, _, _, _, let rule):
            var answers = [1: KeyedAction(label: "Allow once") { pick("once") { try await approve("once") } }, 3: KeyedAction(label: "Reject") { pick("reject") { try await approve("reject") } }]
            if rule != nil { answers[2] = KeyedAction(label: "Always allow…") { confirmingAlways = true } }
            return SessionKeyActions(enabled: answerable, answers: answers, send: nil)
        case .cap(let edge, _):
            return SessionKeyActions(enabled: answerable, answers: [:], send: KeyedAction(label: "Continue") { pick("continue") { try await proceed(edge) } })
        }
    }

    private var answerable: Bool {
        enabled && armed && chosen == nil
    }

    private func header(symbol: String?, title: String, detail: String) -> some View {
        HStack(spacing: 12) {
            DockBadge(symbol: symbol ?? "dollarsign", square: symbol == nil)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.title3.weight(.semibold))
                    .lineLimit(2)
                Text(detail)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
    }

    private func caption(_ text: String) -> some View {
        Text(text)
            .font(.footnote)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity)
            .multilineTextAlignment(.center)
    }

    private func pick(_ choice: String, _ work: @escaping @MainActor () async throws -> Void) {
        chosen = choice
        failure = nil
        Task {
            do {
                try await work()
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
                chosen = nil
            }
        }
    }
}

struct CommandText: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.callout.monospaced())
            .textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct PrimaryCapsule: View {
    let title: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(.headline)
                .frame(maxWidth: .infinity, minHeight: 50)
                .foregroundStyle(Color.onAccent)
                .background(Color.accentColor, in: .capsule)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
    }
}

struct RunAlwaysAllowSheet: View {
    let rule: String
    let role: String
    let confirm: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var height: CGFloat = 320

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Always allow this command?")
                .font(.title3.weight(.semibold))
            Text(Markdown.inline("\(role) and every step with this exact command will run in the main checkout without asking, on this Mac, until you remove the rule from `.claude/settings.local.json`."))
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            VStack(alignment: .leading, spacing: 4) {
                Text("Rule it stores")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                CommandText(text: rule)
                    .background(Color(.systemGroupedBackground), in: .rect(cornerRadius: 16))
            }
            VStack(spacing: 6) {
                PlainCapsule(title: "Always allow", tint: .primary, action: confirm)
                Button { dismiss() } label: {
                    Text("Cancel")
                        .font(.headline)
                        .frame(maxWidth: .infinity, minHeight: 44)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .keyboardShortcut(.cancelAction)
            }
        }
        .padding(24)
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
        .presentationDetents([.height(height)])
        .presentationDragIndicator(.hidden)
    }
}
