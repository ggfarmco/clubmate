import PhotosUI
import SwiftUI

import CompanionKit

struct SessionDetailView: View {
    let context: PluginContext
    let store: AgentsStore
    let sessionID: String
    @State private var draft = ""
    @State private var attachments: [ComposerAttachment] = []
    @State private var sending = false
    @State private var sent = 0
    @State private var failure: String?
    @State private var answeredElsewhere = 0
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var confirmingClose = false
    @State private var confirmingStop = false
    @State private var showingSettings = false
    @FocusState private var composerFocused: Bool

    var body: some View {
        let transcript = store.transcripts[sessionID]
        let entries = store.entries(sessionID)
        let session = store.session(sessionID)
        let screen = SessionScreen(entries: entries, session: session)
        let blocked = context.connection.blocked
        let answerDocked = switch screen.dock {
        case .permission, .elicitation: true
        default: false
        }
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 16) {
                if let parent = session?.parentSession.flatMap(store.session) {
                    StartedByCard(parent: parent) { context.navigation.detail = parent.id }
                }
                ForEach(screen.rows) { row in
                    RowView(row: row)
                }
                if session?.status == .running {
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Working…").foregroundStyle(.secondary)
                    }
                    .font(.callout)
                    .id("working")
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 12)
            .frame(maxWidth: 820)
            .frame(maxWidth: .infinity)
        }
        .environment(\.subagentStop, context.connection.supports("agents.stop-subagent") ? SubagentStop(enabled: blocked == nil) { id in
            do {
                try await store.stopSubagent(id, in: sessionID)
                return true
            } catch {
                failure = Self.message(error)
                return false
            }
        } : nil)
        .safeAreaInset(edge: .top, spacing: 0) {
            RequestBannerHost()
                .padding(.horizontal)
                .padding(.vertical, 6)
                .frame(maxWidth: 820)
                .frame(maxWidth: .infinity)
                .background(Color(.systemGroupedBackground))
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .scrollDismissesKeyboard(.interactively)
        .scrollEdgeEffectStyle(.soft, for: .top)
        .background(Color(.systemGroupedBackground))
        .overlay {
            if transcript == nil, let failure = store.transcriptErrors[sessionID] {
                ContentUnavailableView {
                    Label("Couldn't Load the Transcript", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(failure)
                } actions: {
                    Button("Try Again") { Task { await store.loadTranscript(sessionID) } }
                }
            } else if transcript == nil && entries.isEmpty {
                ProgressView()
            } else if entries.isEmpty {
                ContentUnavailableView("No Messages Yet", systemImage: "text.bubble", description: Text("Send the first prompt below."))
            }
        }
        .simultaneousGesture(TapGesture().onEnded { composerFocused = false })
        .safeAreaInset(edge: .bottom) {
            VStack(spacing: 10) {
                if transcript != nil, let failure = store.transcriptErrors[sessionID] {
                    RefreshFailedBanner(message: failure) {
                        Task { await store.loadTranscript(sessionID, incremental: true) }
                    }
                }
                if answeredElsewhere > 0 {
                    AnsweredElsewhereBanner()
                        .transition(.opacity)
                        .task(id: answeredElsewhere) {
                            try? await Task.sleep(for: .seconds(3))
                            withAnimation { answeredElsewhere = 0 }
                        }
                }
                if let session, !session.isClosed, let dock = screen.dock {
                    Group {
                        switch dock {
                        case .permission(let request, let more):
                            PermissionCard(request: request, more: more, agentName: session.agentName, place: session.worktreeName, blocked: blocked) { option in
                                try await answer(request: request.id, option: option)
                            }
                            .id(request.id)
                        case .elicitation(let question):
                            ElicitationCard(question: question, agentName: session.agentName, blocked: blocked) { action, answers in
                                try await store.respond(sessionID, request: question.id, action: action, answers: answers)
                            }
                            .id(question.id)
                        case .question(let question):
                            QuestionCard(question: question, agentName: session.agentName, subagent: session.parentSession != nil)
                        }
                    }
                    .padding(.horizontal, 12)
                    .frame(maxWidth: 820)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
                if let session {
                    ComposerView(
                        draft: $draft,
                        attachments: $attachments,
                        focused: $composerFocused,
                        session: session,
                        enabled: blocked == nil,
                        blocked: answerDocked ? nil : blocked,
                        sending: sending,
                        send: send
                    )
                }
            }
            .animation(.snappy, value: screen.dock)
        }
        .navigationTitle(session?.displayTitle ?? "Session")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                if let session {
                    SessionHeader(session: session, mac: context.model.tag(context.connection.computer.id))
                }
            }
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
                    if session?.isTurnActive == true {
                        Button("Stop…", systemImage: "stop.circle") { confirmingStop = true }
                    }
                    if session?.isClosed == false && context.connection.supports("agents.config") {
                        Button("Session Settings", systemImage: "slider.horizontal.3") { showingSettings = true }
                    }
                    Button("Refresh", systemImage: "arrow.clockwise") {
                        Task { await store.loadTranscript(sessionID) }
                    }
                    if session?.isClosed == false {
                        Button("Close Session…", systemImage: "archivebox", role: .destructive) { confirmingClose = true }
                    }
                }
            }
        }
        .sheet(isPresented: $showingSettings) {
            SessionSettingsView(store: store, sessionID: sessionID)
        }
        .confirmationDialog("Stop \(session?.agentName ?? "the agent")?", isPresented: $confirmingStop, titleVisibility: .visible) {
            Button("Stop", role: .destructive) {
                perform { try await store.cancel(sessionID) }
            }
        } message: {
            Text("The current turn ends now. The session stays open for your next message.")
        }
        .confirmationDialog("Close this session?", isPresented: $confirmingClose, titleVisibility: .visible) {
            Button("Close Session", role: .destructive) {
                perform { try await store.close(sessionID) }
            }
        } message: {
            Text("The agent stops when no other session uses its worktree. The transcript stays readable.")
        }
        .alert("Something Went Wrong", isPresented: Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })) {
            Button("OK") { failure = nil }
        } message: {
            Text(failure ?? "")
        }
        .sensoryFeedback(.impact(weight: .medium), trigger: sent)
        .sessionKeys(enabled: context.connection.state == .online, pending: store.pendingPermission(sessionID), answer: { request, option in
            perform { try await answer(request: request.id, option: option.optionId) }
        }, send: send)
        .onChange(of: entries.count) { _, _ in
            withAnimation { position.scrollTo(edge: .bottom) }
        }
        .task(id: screen.dock) {
            try? await Task.sleep(for: .milliseconds(150))
            withAnimation { position.scrollTo(edge: .bottom) }
        }
        .task(id: sessionID) {
            store.watch(sessionID)
            try? await Task.sleep(for: .seconds(31_536_000))
            store.unwatch(sessionID)
        }
    }

    private func send() {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        let attached = attachments
        guard !text.isEmpty || !attached.isEmpty else { return }
        let typed = draft
        sending = true
        draft = ""
        attachments = []
        Task {
            do {
                try await store.send(text, attachments: attached, to: sessionID)
                sent += 1
            } catch {
                if draft.isEmpty { draft = typed }
                if attachments.isEmpty { attachments = attached }
                failure = Self.message(error)
            }
            sending = false
        }
    }

    private func answer(request: String, option: String) async throws {
        do {
            try await store.answer(sessionID, request: request, option: option)
        } catch AnswerError.alreadyAnswered {
            withAnimation { answeredElsewhere += 1 }
            return
        }
        sent += 1
    }

    private func perform(_ work: @escaping @MainActor () async throws -> Void) {
        Task {
            do {
                try await work()
            } catch {
                failure = Self.message(error)
            }
        }
    }

    private static func message(_ error: any Error) -> String {
        (error as? RemoteError)?.message ?? error.localizedDescription
    }
}

struct SessionHeader: View {
    let session: AgentSession
    var mac: MacTag?

    var body: some View {
        let state = session.state
        let detail = session.headerDetail
        VStack(spacing: 1) {
            Text(session.displayTitle)
                .font(.headline)
                .lineLimit(1)
            (Text(state.label).foregroundStyle(state.needsYou ? Color.warning : .secondary)
                + (mac != nil || !detail.isEmpty ? Text(" · ") + mac.lead(detail) : Text("")).foregroundStyle(.secondary))
                .font(.footnote)
                .lineLimit(1)
        }
        .frame(maxWidth: 260)
        .accessibilityElement(children: .combine)
    }
}

struct StartedByCard: View {
    let parent: AgentSession
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            HStack(spacing: 12) {
                Image(systemName: "chevron.left")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Started by")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Text(parent.displayTitle)
                        .lineLimit(1)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .frame(minHeight: 44)
            .background(Color.surface1, in: .rect(cornerRadius: 20))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityHint("Opens the parent session")
    }
}

struct AnsweredElsewhereBanner: View {
    var body: some View {
        Label("Already answered", systemImage: "checkmark.circle")
            .font(.footnote)
            .foregroundStyle(.secondary)
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .glassEffect(in: .capsule)
    }
}

struct RefreshFailedBanner: View {
    let message: String
    let retry: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Label {
                Text("Couldn't refresh: \(message)").lineLimit(2)
            } icon: {
                Image(systemName: "exclamationmark.triangle")
            }
            .font(.footnote)
            .foregroundStyle(.secondary)
            Spacer(minLength: 0)
            Button("Retry", action: retry)
                .font(.footnote.weight(.semibold))
                .buttonStyle(.glass)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
        .glassEffect(in: .rect(cornerRadius: 18))
        .padding(.horizontal, 12)
        .frame(maxWidth: 820)
    }
}

struct ComposerView: View {
    @Binding var draft: String
    @Binding var attachments: [ComposerAttachment]
    var focused: FocusState<Bool>.Binding
    let session: AgentSession?
    let enabled: Bool
    var blocked: String? = nil
    let sending: Bool
    let send: () -> Void
    @State private var picked: [PhotosPickerItem] = []
    @State private var importing = 0
    @State private var choosingPhotos = false
    @State private var choosingFiles = false
    @State private var tooLarge = false

    private var attachedBytes: Int {
        attachments.reduce(0) { $0 + $1.bytes }
    }

    private var empty: Bool {
        draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && attachments.isEmpty
    }

    private var placeholder: String {
        session?.composerPlaceholder ?? "What should the agent do?"
    }

    var body: some View {
        if session?.isClosed == true {
            Label("This session is closed", systemImage: "archivebox")
                .font(.callout)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 18)
                .padding(.vertical, 12)
                .glassEffect()
                .padding(.bottom, 8)
        } else {
            GlassEffectContainer(spacing: 10) {
                VStack(alignment: .leading, spacing: 8) {
                    if !attachments.isEmpty || importing > 0 {
                        AttachmentsRow(attachments: $attachments, importing: importing)
                    }
                    HStack(alignment: .bottom, spacing: 10) {
                        Menu {
                            Button("Photos or Videos", systemImage: "photo.on.rectangle") { choosingPhotos = true }
                            Button("Files", systemImage: "folder") { choosingFiles = true }
                        } label: {
                            Image(systemName: "paperclip")
                                .font(.body.weight(.semibold))
                                .frame(width: 30, height: 30)
                        }
                        .buttonStyle(.glass)
                        .buttonBorderShape(.circle)
                        .accessibilityLabel("Attach")
                        .photosPicker(isPresented: $choosingPhotos, selection: $picked, maxSelectionCount: 4, matching: .any(of: [.images, .videos]))
                        .fileImporter(isPresented: $choosingFiles, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
                            if case .success(let urls) = result { addFiles(urls) }
                        }
                        TextField(placeholder, text: $draft, axis: .vertical)
                            .lineLimit(1...6)
                            .focused(focused)
                            .accessibilityIdentifier("composer")
                            .pasteDestination(for: PastedImage.self, action: paste)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 11)
                            .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 22))
                        Button(action: submit) {
                            Group {
                                if sending {
                                    ProgressView()
                                } else {
                                    Image(systemName: "arrow.up").font(.body.weight(.bold))
                                }
                            }
                            .frame(width: 30, height: 30)
                        }
                        .buttonStyle(.glassProminent)
                        .buttonBorderShape(.circle)
                        .disabled(empty || sending || importing > 0 || attachedBytes > ComposerAttachment.maxBytes)
                        .accessibilityLabel(session?.sendLabel ?? "Send")
                    }
                    if let caption = blocked ?? sizeCaption ?? session?.composerCaption {
                        Text(caption)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .padding(.leading, 4)
                    }
                }
            }
            .onChange(of: picked) { _, items in
                guard !items.isEmpty else { return }
                picked = []
                importing += items.count
                Task {
                    for item in items {
                        if let attachment = await ComposerMedia.load(item) {
                            attachments.append(attachment)
                        }
                        importing -= 1
                    }
                }
            }
            .onAppear { if session == nil { focused.wrappedValue = true } }
            .disabled(!enabled)
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .frame(maxWidth: 820)
        }
    }

    private func submit() {
        focused.wrappedValue = false
        send()
    }

    private var sizeCaption: String? {
        guard tooLarge || attachedBytes > ComposerAttachment.maxBytes else { return nil }
        return "Attachments are limited to \(ByteCountFormatter.string(fromByteCount: Int64(ComposerAttachment.maxBytes), countStyle: .file)) per message"
    }

    private func addFiles(_ urls: [URL]) {
        tooLarge = false
        for url in urls {
            let scoped = url.startAccessingSecurityScopedResource()
            defer { if scoped { url.stopAccessingSecurityScopedResource() } }
            let size = (try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0
            guard attachedBytes + size <= ComposerAttachment.maxBytes, let data = try? Data(contentsOf: url) else {
                tooLarge = true
                continue
            }
            attachments.append(.file(name: url.lastPathComponent, data: data))
        }
    }

    private func paste(_ images: [PastedImage]) {
        attachments += images.compactMap { PhotoEncoder.jpeg($0.data).map(ComposerAttachment.photo) }
    }
}
