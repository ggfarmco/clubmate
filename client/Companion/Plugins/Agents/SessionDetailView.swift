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
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var confirmingClose = false
    @State private var showingSettings = false
    @FocusState private var composerFocused: Bool

    var body: some View {
        let transcript = store.transcripts[sessionID]
        let entries = store.entries(sessionID)
        let session = store.session(sessionID)
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 16) {
                ForEach(entries) { entry in
                    EntryView(entry: entry, session: session) { request, option in
                        try await answer(request: request, option: option)
                    }
                    .opacity(entry.isOutgoing ? 0.6 : 1)
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
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .scrollDismissesKeyboard(.interactively)
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
            VStack(spacing: 8) {
                if transcript != nil, let failure = store.transcriptErrors[sessionID] {
                    RefreshFailedBanner(message: failure) {
                        Task { await store.loadTranscript(sessionID, incremental: true) }
                    }
                }
                if let session {
                    ComposerView(
                        draft: $draft,
                        attachments: $attachments,
                        focused: $composerFocused,
                        session: session,
                        enabled: context.connection.state == .online,
                        sending: sending,
                        send: send,
                        stop: stop
                    )
                }
            }
        }
        .navigationTitle(session?.displayTitle ?? "Session")
        .navigationBarTitleDisplayMode(.inline)
        .navigationSubtitle(subtitle(session))
        .toolbar {
            if session?.isClosed == false && context.connection.supports("agents.config") {
                ToolbarItem(placement: .primaryAction) {
                    Button("Session Settings", systemImage: "slider.horizontal.3") { showingSettings = true }
                }
            }
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
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
        .onChange(of: entries.count) { _, _ in
            withAnimation { position.scrollTo(edge: .bottom) }
        }
        .task(id: sessionID) {
            store.watch(sessionID)
            try? await Task.sleep(for: .seconds(31_536_000))
            store.unwatch(sessionID)
        }
    }

    private func subtitle(_ session: AgentSession?) -> String {
        guard let session else { return "" }
        let usage = store.transcripts[sessionID]?.usage.map { [$0.contextLabel] } ?? []
        return ([session.agentName, session.worktreeName, session.status.label] + (session.afk ? ["AFK"] : []) + usage).joined(separator: " · ")
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

    private func stop() {
        perform { try await store.cancel(sessionID) }
    }

    private func answer(request: String, option: String) async throws {
        try await store.answer(sessionID, request: request, option: option)
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
    let sending: Bool
    let send: () -> Void
    let stop: () -> Void
    @State private var picked: [PhotosPickerItem] = []
    @State private var importing = 0

    private var empty: Bool {
        draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && attachments.isEmpty
    }

    private var turnActive: Bool {
        session?.isTurnActive == true
    }

    private var placeholder: String {
        guard let session else { return "What should the agent do?" }
        return session.isTurnActive ? "Queue a message" : "Message \(session.agentName)"
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
                        PhotosPicker(selection: $picked, maxSelectionCount: 4, matching: .any(of: [.images, .videos])) {
                            Image(systemName: "photo.on.rectangle")
                                .font(.body.weight(.semibold))
                                .frame(width: 30, height: 30)
                        }
                        .buttonStyle(.glass)
                        .buttonBorderShape(.circle)
                        .accessibilityLabel("Attach Photos or Videos")
                        TextField(placeholder, text: $draft, axis: .vertical)
                            .lineLimit(1...6)
                            .focused(focused)
                            .accessibilityIdentifier("composer")
                            .pasteDestination(for: PastedImage.self, action: paste)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 11)
                            .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 22))
                        if turnActive && draft.isEmpty && attachments.isEmpty && importing == 0 {
                            Button(action: stop) {
                                Image(systemName: "stop.fill")
                                    .font(.body.weight(.semibold))
                                    .frame(width: 30, height: 30)
                            }
                            .buttonStyle(.glass)
                            .buttonBorderShape(.circle)
                            .accessibilityLabel("Stop")
                        } else {
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
                            .disabled(empty || sending || importing > 0)
                            .accessibilityLabel("Send")
                        }
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

    private func paste(_ images: [PastedImage]) {
        attachments += images.compactMap { PhotoEncoder.jpeg($0.data).map(ComposerAttachment.photo) }
    }
}
