import SwiftUI

import CompanionKit

struct SessionDetailView: View {
    let context: PluginContext
    let store: AgentsStore
    let sessionID: String
    @State private var draft = ""
    @State private var sending = false
    @State private var sent = 0
    @State private var failure: String?
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var confirmingClose = false

    var body: some View {
        let transcript = store.transcripts[sessionID]
        let session = store.session(sessionID)
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 16) {
                ForEach(transcript?.entries ?? []) { entry in
                    EntryView(entry: entry, session: session) { request, option in
                        try await answer(request: request, option: option)
                    }
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
            if transcript == nil {
                ProgressView()
            } else if transcript?.entries.isEmpty == true {
                ContentUnavailableView("No Messages Yet", systemImage: "text.bubble", description: Text("Send the first prompt below."))
            }
        }
        .safeAreaInset(edge: .bottom) {
            if let session {
                ComposerView(
                    draft: $draft,
                    session: session,
                    enabled: context.connection.state == .online,
                    sending: sending,
                    send: send,
                    stop: stop
                )
            }
        }
        .navigationTitle(session?.displayTitle ?? "Session")
        .navigationBarTitleDisplayMode(.inline)
        .navigationSubtitle(subtitle(session))
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
                    Button("Refresh", systemImage: "arrow.clockwise") {
                        Task { await store.loadTranscript(sessionID) }
                    }
                    if session?.closed == false {
                        Button("Close Session…", systemImage: "archivebox", role: .destructive) { confirmingClose = true }
                    }
                }
            }
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
        .onChange(of: transcript?.entries.count) { _, _ in
            withAnimation { position.scrollTo(edge: .bottom) }
        }
        .task(id: sessionID) {
            store.watch(sessionID)
            await store.loadTranscript(sessionID)
            try? await Task.sleep(for: .seconds(31_536_000))
            store.unwatch(sessionID)
        }
    }

    private func subtitle(_ session: AgentSession?) -> String {
        guard let session else { return "" }
        return [session.agentName, session.worktreeName, session.status.label].joined(separator: " · ")
    }

    private func send() {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        sending = true
        Task {
            do {
                try await store.send(text, to: sessionID)
                draft = ""
                sent += 1
            } catch {
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

struct ComposerView: View {
    @Binding var draft: String
    let session: AgentSession
    let enabled: Bool
    let sending: Bool
    let send: () -> Void
    let stop: () -> Void
    @FocusState private var focused: Bool

    var body: some View {
        if session.closed {
            Label("This session is closed", systemImage: "archivebox")
                .font(.callout)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 18)
                .padding(.vertical, 12)
                .glassEffect()
                .padding(.bottom, 8)
        } else {
            GlassEffectContainer(spacing: 10) {
                HStack(alignment: .bottom, spacing: 10) {
                    TextField(session.isTurnActive ? "Queue a message" : "Message \(session.agentName)", text: $draft, axis: .vertical)
                        .lineLimit(1...6)
                        .focused($focused)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 11)
                        .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 22))
                    if session.isTurnActive && draft.isEmpty {
                        Button(action: stop) {
                            Image(systemName: "stop.fill")
                                .font(.body.weight(.semibold))
                                .frame(width: 30, height: 30)
                        }
                        .buttonStyle(.glass)
                        .buttonBorderShape(.circle)
                        .accessibilityLabel("Stop")
                    } else {
                        Button(action: send) {
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
                        .disabled(draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || sending)
                        .accessibilityLabel("Send")
                    }
                }
            }
            .disabled(!enabled)
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .frame(maxWidth: 820)
        }
    }
}
