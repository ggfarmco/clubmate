import PhotosUI
import SwiftUI

import CompanionKit

struct SessionDetailView: View {
    let context: PluginContext
    let store: AgentsStore
    let sessionID: String
    var role: SessionRole = .plain
    @Environment(AppNavigation.self) private var navigation
    @Environment(VoiceSession.self) private var voice
    @Environment(\.verticalSizeClass) private var verticalSizeClass
    @State private var draft = ""
    @State private var attachments: [ComposerAttachment] = []
    @State private var sending = false
    @State private var sent = 0
    @State private var failure: String?
    @State private var answeredElsewhere = 0
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var confirmingClose = false
    @State private var confirmingStop = false
    @State private var confirmingHeaderStop = false
    @State private var showingSettings = false
    @State private var showingInfo = false
    @State private var alwaysAsked: AlwaysAsk?
    @State private var width = 0.0
    @State private var composerFocused = false
    @FocusState private var cardField: String?
    @State private var reviewing: LandCard?
    @State private var archiving = false
    @State private var window = TranscriptWindow()
    @State private var atBottom = true
    @State private var draftSeen: Int?
    @State private var showingAsides = false
    @State private var pickedAside: String?
    @State private var asideSheetUp = false
    @State private var pushReady = false
    @State private var sendingBack: SendBackStart?
    @State private var closeQuestion: String?
    @State private var foldingOpen = false

    private static let pairedWidth = 520.0

    private var mode: SessionRole {
        role == .plain && store.session(sessionID)?.isAside == true ? .asideFull : role
    }

    var body: some View {
        let session = store.session(sessionID)
        let transcript = store.transcripts[sessionID]
        let blocked = context.connection.blocked
        screen
        .sheet(item: asideSheet, onDismiss: asideSheetDismissed) { item in
            NavigationStack {
                SessionDetailView(context: context, store: store, sessionID: item.id, role: .asideSheet)
            }
            .presentationDetents([.fraction(0.62)])
            .presentationDragIndicator(.visible)
            .onAppear { asideSheetUp = true }
        }
        .sheet(isPresented: $showingAsides, onDismiss: {
            if let id = pickedAside { store.asidePresentation[sessionID] = .sheet(id) }
            pickedAside = nil
        }) {
            AsideListSheet(asides: store.asides(of: sessionID)) { pickedAside = $0 }
        }
        .sheet(item: $sendingBack) { start in
            if let parent = session?.parentSession {
                SendBackSheet(store: store, aside: sessionID, parent: parent, parentTitle: store.session(parent)?.displayTitle ?? "the session", blocked: blocked, to: start.to)
                    .presentationDetents([.large])
            }
        }
        .navigationDestination(item: asidePushed) { id in
            SessionDetailView(context: context, store: store, sessionID: id, role: .asideFull)
        }
        .confirmationDialog(closeQuestion ?? "", isPresented: Binding(get: { closeQuestion != nil }, set: { if !$0 { closeQuestion = nil } }), titleVisibility: .visible) {
            Button("Send back…") { sendingBack = SendBackStart(to: transcript?.asidePanel?.answer == true ? .answer : .draft) }
            Button("Close", role: .destructive) { perform { try await closeAside() } }
        }
        .task(id: sessionID) {
            guard role != .plain else { return }
            store.watch(sessionID)
            defer { store.unwatch(sessionID) }
            while !Task.isCancelled { try? await Task.sleep(for: .seconds(3600)) }
        }
        .onChange(of: session?.composerDraft, initial: true) { _, next in
            guard let next else { return }
            if let text = next.apply(local: draft, seen: draftSeen) { draft = text }
            draftSeen = next.rev
        }
        .onChange(of: store.asidePresentation[sessionID]) { _, presentation in
            switch presentation {
            case .full where !asideSheetUp: pushReady = true
            case .kept(let id) where !asideSheetUp:
                store.asidePresentation[sessionID] = nil
                context.navigation.detail = id
            case .full, .kept: break
            default: pushReady = false
            }
        }
    }

    @ViewBuilder private func loadState(loaded: Bool, empty: Bool) -> some View {
        if !loaded, let failure = store.transcriptErrors[sessionID] {
            ContentUnavailableView {
                Label("Couldn't Load the Transcript", systemImage: "exclamationmark.triangle")
            } description: {
                Text(failure)
            } actions: {
                Button("Try Again") { Task { await store.loadTranscript(sessionID) } }
            }
        } else if !loaded && empty {
            ProgressView()
        } else if empty {
            ContentUnavailableView("No Messages Yet", systemImage: "text.bubble", description: Text("Send the first prompt below."))
        }
    }

    @ViewBuilder private var screen: some View {
        let transcript = store.transcripts[sessionID]
        let entries = store.entries(sessionID)
        let session = store.session(sessionID)
        let repos = store.repos(sessionID)
        let screen = SessionScreen(entries: entries, session: session, request: store.request(sessionID), repos: repos)
        let blocked = context.connection.blocked
        let queued = store.queue?.item(sessionID)
        let rows = screen.rows
        let skip = window.skip(rows)
        let cardTyping = typingInCard(screen)
        let keySend: (() -> Void)? = cardTyping ? nil : { send() }
        let answerDocked = switch screen.dock {
        case .permission, .elicitation, .connect, .land: true
        case .open(let card): !closing && (!folded(card) || store.openDraft(sessionID).unfolded)
        default: false
        }
        let parent = session?.aside == nil ? session?.parentSession.flatMap(store.session) : nil
        let leadCard = session?.aside != nil || parent != nil
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                if let session, let aside = session.aside {
                    ForkedFromCard(anchor: aside.anchor, panel: transcript?.asidePanel)
                } else if let parent {
                    StartedByCard(parent: parent) { context.navigation.detail = parent.id }
                }
                if leadCard && entries.isEmpty && transcript == nil {
                    loadState(loaded: false, empty: true)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 24)
                }
                if skip > 0 {
                    EarlierRowsButton(left: skip, page: TranscriptWindow.page) { window.showEarlier(rows) }
                }
                ForEach(Self.blocks(Array(rows[skip...]), paired: width >= Self.pairedWidth), id: \.[0].id) { block in
                    if block.count == 2 {
                        HStack(alignment: .top, spacing: 10) {
                            RowView(row: block[0])
                            RowView(row: block[1])
                        }
                    } else {
                        RowView(row: block[0])
                    }
                }
                if let repos {
                    ReposCardView(card: repos, fold: store.reposFold(sessionID), blocked: blocked) {
                        var fold = store.reposFold(sessionID)
                        fold.toggle(repos)
                        store.setReposFold(fold, session: sessionID)
                    } toggleRow: { row in
                        var fold = store.reposFold(sessionID)
                        fold.toggle(row)
                        store.setReposFold(fold, session: sessionID)
                    } run: { action, row in
                        run(action, row: row)
                    } archive: {
                        archiving = true
                    }
                    if case .land = screen.dock {
                        Text("Nothing lands until you say so. The next screen lists the commits and has its own Merge.")
                            .font(.footnote)
                            .foregroundStyle(Color.captionInk)
                            .padding(.horizontal, 4)
                    }
                }
                if session?.status == .error, let line = session?.failure?.line, !line.isEmpty {
                    Label(line, systemImage: "xmark.circle")
                        .font(.footnote)
                        .foregroundStyle(Color.dangerInk)
                }
                if let session, !failureButtons.isEmpty || !session.failureOnMac(context.connection.computer.name).isEmpty {
                    FailureActionsView(buttons: failureButtons, onMac: session.failureOnMac(context.connection.computer.name), blocked: blocked) { action in
                        perform {
                            if let next = try await store.recover(sessionID, with: action) {
                                context.navigation.detail = next
                            }
                        }
                    }
                }
                if reviewable {
                    ReviewedActionView(blocked: blocked) {
                        perform { try await store.reviewed(sessionID) }
                    }
                }
                if session?.status == .running {
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Working…").foregroundStyle(Color.captionInk)
                    }
                    .font(.callout)
                    .id("working")
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 12)
            .frame(maxWidth: 820)
            .onGeometryChange(for: Double.self) { $0.size.width } action: { width = $0 }
            .frame(maxWidth: .infinity)
            .onChange(of: rows.count, initial: true) { _, count in
                if transcript != nil { window.update(count: count, atBottom: atBottom) }
            }
            .onChange(of: atBottom) { _, atBottom in
                if transcript != nil { window.update(count: rows.count, atBottom: atBottom) }
            }
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
            if mode == .asideFull, let pinned = transcript?.pinned, let session, let parent = session.parentSession {
                AsideStrip(pinned: pinned, panel: transcript?.asidePanel, anchorOption: session.aside?.anchor.option, blocked: blocked) { value in
                    perform { try await store.asidePick(sessionID, parent: parent, value: value) }
                } other: {
                    sendingBack = SendBackStart(to: .answer)
                }
            } else if mode == .plain, !(answerDocked && verticalSizeClass == .compact) {
                RequestBannerHost()
                    .padding(.horizontal)
                    .padding(.vertical, 6)
                    .frame(maxWidth: 820)
                    .frame(maxWidth: .infinity)
                    .background(Color(.systemGroupedBackground))
            }
        }
        .environment(\.folderRemove, blocked == nil && context.connection.supports(TaskCommand.folderRemove) ? FolderRemove { path in
            do {
                failure = try await store.removeFolder(path)
                await context.connection.refreshWorkspaces()
            } catch {
                failure = Self.message(error)
            }
        } : nil)
        .environment(\.openDoc, docOpener)
        .environment(\.openTask, taskOpener)
        .environment(\.toolCallLoader, ToolCallLoader { [store, sessionID] id in await store.toolCall(id, in: sessionID) })
        .environment(\.imageStore, context.connection.supports(AgentsStore.imageCommand) ? store : nil)
        .environment(\.imageSession, sessionID)
        .onScrollGeometryChange(for: Bool.self) { geometry in
            geometry.contentOffset.y + geometry.containerSize.height >= geometry.contentSize.height - 80
        } action: { _, bottom in
            atBottom = bottom
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .scrollDismissesKeyboard(.interactively)
        .scrollEdgeEffectStyle(.soft, for: .top)
        .background(Color(.systemGroupedBackground))
        .overlay {
            if !leadCard {
                loadState(loaded: transcript != nil, empty: entries.isEmpty)
            }
        }
        .simultaneousGesture(TapGesture().onEnded {
            if composerFocused || cardField != nil {
                composerFocused = false
                cardField = nil
            } else if cardDock(screen) == "open", blocked == nil {
                foldOpen()
            }
        })
        .safeAreaInset(edge: .bottom) {
            VStack(spacing: 10) {
                docBanner
                if transcript != nil, let failure = store.transcriptErrors[sessionID] {
                    RefreshFailedBanner(message: failure) {
                        Task { await store.loadTranscript(sessionID, incremental: true) }
                    }
                }
                ForEach(store.queue?.notes(session: sessionID) ?? []) { note in
                    QueueNoteRow(note: note) { store.queue?.dismiss(note) }
                        .padding(.horizontal, 12)
                        .frame(maxWidth: 820)
                }
                if let queued, queued.kind != .connect {
                    QueuedLine(item: queued, queuedLine: context.connection.queuedLine) {
                        store.queue?.cancel(queued)
                    }
                    .padding(12)
                    .background(Color.surface1, in: .rect(cornerRadius: 20))
                    .padding(.horizontal, 12)
                    .frame(maxWidth: 820)
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
                            PermissionCard(request: request, more: more, agentName: session.agentName, place: session.worktreeName, blocked: blocked, askAlways: alwaysAsked) { option in
                                try await answer(request: request.id, option: option)
                            }
                            .id(request.id)
                        case .elicitation(let question):
                            ElicitationCard(question: question, agentName: session.agentName, blocked: blocked, draft: Binding(get: { store.elicitationDraft(sessionID, request: question.id) }, set: { store.setElicitationDraft($0, session: sessionID, request: question.id) }), focused: $cardField) { action, answers in
                                try await store.respond(sessionID, request: question.id, action: action, answers: answers)
                            }
                            .id(question.id)
                        case .question(let question):
                            QuestionCard(question: question, agentName: session.agentName, subagent: session.parentSession != nil)
                        case .onMac(let text):
                            AnswerOnMacRow(text: text)
                        case .connect(let card):
                            ConnectRepoCard(card: card, blocked: blocked, canQueue: context.connection.queues, queued: queued?.request == card.request ? queued : nil, queuedLine: context.connection.queuedLine, draft: Binding(get: { store.connectDraft(card.request) }, set: { store.setConnectDraft($0, request: card.request) })) { answers in
                                do {
                                    if try await store.answerConnect(sessionID, card: card, answers: answers) == .sent { sent += 1 }
                                } catch AnswerError.alreadyAnswered {
                                    withAnimation { answeredElsewhere += 1 }
                                }
                            } undo: {
                                if let queued { store.queue?.cancel(queued) }
                            }
                            .id(card.request)
                        case .land(let card):
                            LandDock(card: card, later: store.landLater.contains(card.request), session: repos?.row(named: card.name)?.session, blocked: blocked) {
                                reviewing = card
                            } openSession: { session in
                                context.navigation.detail = session
                            } notNow: { later in
                                withAnimation {
                                    if later { store.landLater.insert(card.request) } else { store.landLater.remove(card.request) }
                                }
                            }
                        case .open(let card):
                            if closing {
                                OpenClosingLine(count: session.openCount, agentName: session.agentName) {
                                    withAnimation { store.undoNoOpen(sessionID) }
                                }
                            } else {
                                OpenItemsCard(card: card, count: session.openCount, folded: folded(card), blocked: blocked, draft: Binding(get: { store.openDraft(sessionID) }, set: { store.setOpenDraft($0, session: sessionID) }), focused: $cardField) {
                                    try await store.laterOpen(sessionID)
                                } send: {
                                    try await store.answerOpen(sessionID, items: card.items)
                                    sent += 1
                                } no: {
                                    withAnimation {
                                        store.noOpenSoon(sessionID) { failure = Self.message($0) }
                                    }
                                }
                            }
                        }
                    }
                    .padding(.horizontal, 12)
                    .frame(maxWidth: 820)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
                if let session, session.isAside, !session.isClosed, mode != .plain {
                    AsideActions(blocked: blocked) {
                        sendingBack = SendBackStart(to: transcript?.asidePanel?.answer == true ? .answer : .draft)
                    } keep: {
                        keepAside()
                    }
                }
                if let session {
                    composer(session, screen: screen, blocked: blocked, answerDocked: answerDocked, hidden: cardTyping)
                }
            }
            .animation(.snappy, value: screen.dock)
            .animation(.snappy, value: store.openedDocs[sessionID])
            .voiceDock()
        }
        .environment(\.asideEntry, asideEntry(session, blocked: blocked))
        .navigationTitle(session?.displayTitle ?? "Session")
        .toolbarTitleDisplayMode(stopInHeader && mode == .plain ? .inlineLarge : .inline)
        .toolbar {
            if mode != .plain {
                asideToolbar(session)
            } else {
            if let session {
                if stopInHeader {
                    ToolbarItem(placement: .largeSubtitle) {
                        TimelineView(.periodic(from: .now, by: 30)) { timeline in
                            SessionHeader.subtitle(session, mac: context.model.tag(context.connection.computer.id), mode: true, now: timeline.date)
                                .font(.subheadline)
                                .lineLimit(1)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                    }
                } else {
                    ToolbarItem(placement: .principal) {
                        SessionHeader(session: session, mac: context.model.tag(context.connection.computer.id))
                    }
                }
            }
            if stopInHeader, session?.isTurnActive == true {
                ToolbarItem(placement: .primaryAction) {
                    Button("Stop", systemImage: "stop.fill") { confirmingHeaderStop = true }
                        .stopConfirmation(agentName: session?.agentName, isPresented: $confirmingHeaderStop) {
                            perform { try await store.cancel(sessionID) }
                        }
                }
            }
            ToolbarItem(placement: .primaryAction) {
                Menu("More", systemImage: "ellipsis") {
                    if case .open = screen.dock {
                        Button("To Backlog", systemImage: "tray.and.arrow.down") {
                            perform { try await store.backlogOpen(sessionID) }
                        }
                        .disabled(blocked != nil)
                    }
                    if session?.archivable == true, context.connection.supports(TaskCommand.archive) {
                        Button("Archive Task…", systemImage: "archivebox") { archiving = true }
                    }
                    if session?.mayAsk == true, session?.isClosed == false, context.connection.supports(OpenCommand.convert) {
                        Button("Turn into Items", systemImage: "checklist") {
                            perform { try await store.convertOpen(sessionID) }
                        }
                        .disabled(blocked != nil)
                    }
                    if let docs = docsList?.docs, !docs.isEmpty {
                        Menu("Docs", systemImage: "doc.richtext") {
                            ForEach(docs) { doc in
                                Button {
                                    open(doc: doc.path)
                                } label: {
                                    Label(doc.name, systemImage: doc.symbol)
                                    if let folder = doc.folder {
                                        Text(folder)
                                    }
                                }
                            }
                        }
                    }
                    if !stopInHeader, session?.isTurnActive == true {
                        Button("Stop…", systemImage: "stop.circle") { confirmingStop = true }
                    }
                    if session?.isClosed == false && context.connection.supports("agents.config") {
                        Button("Session Settings", systemImage: "slider.horizontal.3") { showingSettings = true }
                    }
                    if session != nil {
                        Button("Session Info", systemImage: "info.circle") { showingInfo = true }
                            .accessibilityIdentifier("session.info.open")
                    }
                    Button("Refresh", systemImage: "arrow.clockwise") {
                        Task { await store.loadTranscript(sessionID) }
                    }
                    if session?.isClosed == false {
                        Button("Close Session…", systemImage: "archivebox", role: .destructive) { confirmingClose = true }
                    }
                }
            }
            if store.asides(of: sessionID).count >= 2 {
                ToolbarItem(placement: .primaryAction) {
                    AsidesPill(count: store.asides(of: sessionID).count) { showingAsides = true }
                }
            }
            }
        }
        .sheet(isPresented: $showingSettings) {
            SessionSettingsView(store: store, sessionID: sessionID)
        }
        .sheet(isPresented: $showingInfo) {
            if let session = store.session(sessionID) {
                SessionInfoView(session: session, context: store.transcripts[sessionID]?.usage.map { "\($0.percent)%" } ?? "—", computerID: context.connection.computer.id)
            }
        }
        .sheet(item: $reviewing, content: reviewSheet)
        .sheet(isPresented: $archiving, content: archiveSheet)
        .stopConfirmation(agentName: session?.agentName, isPresented: $confirmingStop) {
            perform { try await store.cancel(sessionID) }
        }
        .confirmationDialog("Close this session?", isPresented: $confirmingClose, titleVisibility: .visible) {
            Button("Close Session", role: .destructive) {
                perform { try await store.close(sessionID) }
            }
        } message: {
            Text("The agent stops when no other session uses its worktree. The transcript stays readable.")
        }
        .alert("Something Went Wrong", isPresented: failureShown) {
            Button("OK") { failure = nil }
        } message: {
            Text(failure ?? "")
        }
        .sensoryFeedback(.impact(weight: .medium), trigger: sent)
        .sessionKeys(enabled: context.connection.state == .online, pending: store.pendingPermission(sessionID), answer: { request, option in
            perform { try await answer(request: request.id, option: option) }
        }, confirmAlways: { alwaysAsked = AlwaysAsk(option: $0) }, send: keySend)
        .onChange(of: entries.count) { _, _ in
            withAnimation { position.scrollTo(edge: .bottom) }
        }
        .onChange(of: cardDock(screen)) {
            cardField = nil
        }
        .task(id: screen.dock) {
            try? await Task.sleep(for: .milliseconds(150))
            withAnimation { position.scrollTo(edge: .bottom) }
        }
        .task(id: sessionID) {
            await docsList?.refresh()
        }
        .task(id: session?.taskFinished == true) {
            store.opened(sessionID)
        }
        .task(id: planDue, loadPlan)
    }

    private var asideSheet: Binding<AsideItem?> {
        Binding(get: {
            guard mode == .plain, case .sheet(let id) = store.asidePresentation[sessionID], liveAside(id) else { return nil }
            return AsideItem(id: id)
        }, set: { item in
            if item == nil, case .sheet = store.asidePresentation[sessionID] { store.asidePresentation[sessionID] = nil }
        })
    }

    private var asidePushed: Binding<String?> {
        Binding(get: {
            guard mode == .plain, pushReady, case .full(let id) = store.asidePresentation[sessionID], liveAside(id) else { return nil }
            return id
        }, set: { id in
            guard id == nil else { return }
            pushReady = false
            if case .full(let aside) = store.asidePresentation[sessionID] { store.asidePresentation[sessionID] = .sheet(aside) }
        })
    }

    private func liveAside(_ id: String) -> Bool {
        store.session(id).map { $0.isAside && !$0.isClosed } ?? false
    }

    private func asideSheetDismissed() {
        asideSheetUp = false
        switch store.asidePresentation[sessionID] {
        case .full: pushReady = true
        case .kept(let id):
            store.asidePresentation[sessionID] = nil
            context.navigation.detail = id
        default: break
        }
    }

    private func asideEntry(_ session: AgentSession?, blocked: String?) -> AsideEntry? {
        guard mode == .plain, let session, session.canAside, !session.isAside, blocked == nil, context.connection.supports(AsideCommand.ask) else { return nil }
        let transcript = store.transcripts[sessionID]
        return AsideEntry { anchor in
            perform {
                let id = try await store.askAside(sessionID, anchor: anchor)
                store.asidePresentation[sessionID] = .sheet(id)
            }
        } mark: { anchor in
            guard let option = anchor.option else { return nil }
            let mark = if let request = anchor.elicitation, let field = anchor.field, let key = Self.fieldKey(transcript, request: request, index: field) {
                transcript?.optionAside(elicitation: request, field: key, option: option)
            } else if let item = anchor.open {
                transcript?.optionAside(open: item, option: option)
            } else {
                nil as OptionAside?
            }
            return mark.map { store.session($0.id)?.aside?.state ?? $0.state }
        } message: { row in
            guard let entry = transcript?.entries.first(where: { $0.id == row }), case .agent(let text) = entry.content else { return nil }
            return text
        }
    }

    private static func fieldKey(_ transcript: AgentTranscript?, request: String, index: Int) -> String? {
        for entry in transcript?.entries ?? [] {
            if case .elicitation(let question) = entry.content, question.id == request, question.request.fields.indices.contains(index) {
                return question.request.fields[index].key
            }
        }
        return nil
    }

    @ToolbarContentBuilder
    private func asideToolbar(_ session: AgentSession?) -> some ToolbarContent {
        if let session {
            ToolbarItem(placement: .principal) {
                AsideHeader(session: session, place: mode == .asideSheet ? (store.transcripts[sessionID]?.asidePanel?.place ?? "an aside") + " · swipe down to the card" : "read-only · Plan")
            }
        }
        ToolbarItemGroup(placement: .primaryAction) {
            if mode == .asideSheet {
                Button("Full Screen", systemImage: "arrow.up.left.and.arrow.down.right") { moveAside { .full($0) } }
                    .accessibilityIdentifier("aside.expand")
            } else {
                Button("Beside", systemImage: "arrow.down.right.and.arrow.up.left") { collapseAside() }
                    .accessibilityIdentifier("aside.collapse")
            }
            Button("Close Aside", systemImage: "xmark") { askClose() }
                .accessibilityIdentifier("aside.close")
                .disabled(context.connection.blocked != nil)
        }
    }

    private func moveAside(_ next: (String) -> AsidePresentation) {
        guard let parent = store.session(sessionID)?.parentSession else { return }
        store.asidePresentation[parent] = next(sessionID)
    }

    private func collapseAside() {
        guard let parent = asideParent else { return }
        let pushed = store.asidePresentation[parent] == .full(sessionID)
        store.asidePresentation[parent] = .sheet(sessionID)
        if !pushed { context.navigation.detail = parent }
    }

    private var asideParent: String? {
        store.session(sessionID)?.parentSession ?? store.transcripts[sessionID]?.session.parentSession
    }

    private func askClose() {
        guard context.connection.supports(AsideCommand.busy) else {
            closeQuestion = "Close this aside?"
            return
        }
        perform {
            if let question = try await store.closeAsk(sessionID) {
                closeQuestion = question
            } else {
                try await closeAside()
            }
        }
    }

    private func closeAside() async throws {
        let parent = asideParent
        try await store.close(sessionID)
        guard let parent else { return }
        store.asidePresentation[parent] = nil
        if role == .plain { context.navigation.detail = parent }
        await store.loadTranscript(parent, incremental: true)
    }

    private func keepAside() {
        let parent = asideParent
        perform {
            try await store.asideKeep(sessionID)
            guard let parent else { return }
            if role != .plain { store.asidePresentation[parent] = .kept(sessionID) }
            await store.loadTranscript(parent, incremental: true)
        }
    }

    private var failureShown: Binding<Bool> {
        Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })
    }

    private var planDue: Bool {
        store.session(sessionID)?.archivable == true && context.connection.state == .online
    }

    @Sendable private func loadPlan() async {
        guard planDue else { return }
        try? await store.loadArchivePlan(sessionID)
    }

    private func reviewSheet(_ card: LandCard) -> some View {
        LandReviewSheet(card: card, store: store, blocked: context.connection.blocked) { tip in
            do {
                try await store.land(sessionID, card: card, action: LandCard.merge, tip: tip)
            } catch AnswerError.alreadyAnswered {
                withAnimation { answeredElsewhere += 1 }
            }
        }
    }

    private func archiveSheet() -> some View {
        ArchiveSheet(plan: store.archivePlans[sessionID], mac: context.connection.computer.name, unavailable: context.connection.queues) { plan, mode in
            let (_, kept) = try await store.archive(sessionID, plan: plan, mode: mode)
            if !kept.isEmpty { failure = kept.joined(separator: "\n") }
        }
        .task {
            guard !context.connection.queues else { return }
            do {
                try await store.loadArchivePlan(sessionID)
            } catch {
                failure = Self.message(error)
            }
        }
    }

    @ViewBuilder private var docBanner: some View {
        if let path = store.openedDocs[sessionID] {
            DocOpenedBanner(name: URL(filePath: path).lastPathComponent) {
                open(doc: path)
            } dismiss: {
                withAnimation { store.dismissOpenedDoc(sessionID) }
            }
            .transition(.move(edge: .bottom).combined(with: .opacity))
            .task(id: path) {
                try? await Task.sleep(for: .seconds(8))
                withAnimation { store.dismissOpenedDoc(sessionID) }
            }
        }
    }

    private var failureButtons: [FailureAction] {
        store.session(sessionID)?.failureButtons(supported: context.connection.supports) ?? []
    }

    private var reviewable: Bool {
        store.session(sessionID)?.review == true && context.connection.supports(AgentsStore.reviewedCommand)
    }

    private var stopInHeader: Bool {
        navigation.layout != .compact
    }

    private static func blocks(_ rows: [TranscriptRow], paired: Bool) -> [[TranscriptRow]] {
        var blocks: [[TranscriptRow]] = []
        for row in rows {
            if paired, case .tools = row, let last = blocks.last, last.count == 1, case .entry(let entry) = last[0], case .plan = entry.content {
                blocks[blocks.count - 1].append(row)
            } else {
                blocks.append([row])
            }
        }
        return blocks
    }

    private var docsList: DocsListStore? {
        guard context.connection.supports(DocsCommand.list), context.connection.supports(DocsCommand.read) else { return nil }
        return context.model.docs(computer: context.connection.computer.id, workspace: store.workspace, session: sessionID)
    }

    private var docOpener: (@MainActor (String) -> Void)? {
        guard context.connection.supports(DocsCommand.read) else { return nil }
        return { path in open(doc: path) }
    }

    private var taskOpener: (@MainActor (String) -> Void)? {
        guard BacklogPluginInfo.requiredCommands.allSatisfy(context.connection.supports) else { return nil }
        let computer = context.connection.computer.id
        let workspace = store.workspace
        let model = context.model
        return { target in
            Task { @MainActor in
                let backlog = model.backlog(computer: computer, workspace: workspace)
                if let backlog, !backlog.fetched || MarkdownTaskLink.id(target).flatMap(backlog.task) == nil { await backlog.refresh() }
                guard let detail = backlog.map({ $0.detail(link: target) }) ?? MarkdownTaskLink.id(target).map(String.init) else { return }
                navigation.workspaces[computer] = workspace
                navigation.open(computer: computer, plugin: BacklogPluginInfo.id, detail: detail)
            }
        }
    }

    private func open(doc path: String) {
        store.dismissOpenedDoc(sessionID)
        navigation.openDoc(computer: context.connection.computer.id, workspace: store.workspace, path: path)
    }

    private func folded(_ card: OpenCard) -> Bool {
        card.later || store.session(sessionID)?.isTurnActive == true
    }

    private func foldOpen() {
        guard !foldingOpen else { return }
        foldingOpen = true
        perform {
            defer { foldingOpen = false }
            try await store.laterOpen(sessionID)
        }
    }

    private var closing: Bool {
        store.closingOpen.contains(sessionID)
    }

    private func answersGoWith(_ screen: SessionScreen) -> Bool {
        guard attachments.isEmpty, !closing, case .open(let card) = screen.dock else { return false }
        return !store.openDraft(sessionID).answered(card.items).isEmpty
    }

    private func composer(_ session: AgentSession, screen: SessionScreen, blocked: String?, answerDocked: Bool, hidden: Bool) -> some View {
        ComposerView(
            draft: $draft,
            dictation: sessionID,
            attachments: $attachments,
            focused: $composerFocused,
            session: session,
            enabled: blocked == nil || context.connection.queues,
            blocked: answerDocked || !failureButtons.isEmpty || reviewable ? nil : context.connection.queueNote ?? blocked,
            sending: sending,
            wide: stopInHeader,
            hint: answersGoWith(screen) ? "Your answers go with it" : nil,
            trailing: session.isAside ? "Plan" : nil,
            reopen: { action in
                perform {
                    if let next = try await store.recover(sessionID, with: action) {
                        context.navigation.detail = next
                    }
                }
            },
            send: send
        )
        .frame(height: hidden ? 0 : nil, alignment: .top)
        .opacity(hidden ? 0 : 1)
        .disabled(hidden)
        .accessibilityHidden(hidden)
    }

    private func typingInCard(_ screen: SessionScreen) -> Bool {
        cardField != nil && !voice.active && cardDock(screen) != nil
    }

    private func cardDock(_ screen: SessionScreen) -> String? {
        switch screen.dock {
        case .elicitation(let question): question.id
        case .open(let card) where !closing && (!folded(card) || store.openDraft(sessionID).unfolded): "open"
        default: nil
        }
    }

    private func send() {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        let attached = attachments
        guard !text.isEmpty || !attached.isEmpty else { return }
        let typed = draft
        let screen = SessionScreen(entries: store.entries(sessionID), session: store.session(sessionID), request: store.request(sessionID), repos: store.repos(sessionID))
        var answering: [OpenItem]?
        if answersGoWith(screen), case .open(let card) = screen.dock { answering = card.items }
        sending = true
        draft = ""
        attachments = []
        Task {
            do {
                try await store.flushNoOpen(sessionID)
                if let answering {
                    try await store.answerOpen(sessionID, items: answering, note: text)
                } else {
                    try await store.send(text, attachments: attached, to: sessionID)
                }
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

    private func run(_ action: ReposCard.Action, row: ReposCard.Row) {
        switch action.kind {
        case .session:
            if let session = row.session { context.navigation.detail = session }
        case .fix:
            guard let path = row.path else { return }
            perform { try await store.repoFix(sessionID, repo: path) }
        case .leave:
            guard let path = row.path else { return }
            perform { try await store.repoLeave(sessionID, repo: path) }
        }
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

extension View {
    func stopConfirmation(agentName: String?, isPresented: Binding<Bool>, stop: @escaping () -> Void) -> some View {
        confirmationDialog("Stop \(agentName ?? "the agent")?", isPresented: isPresented, titleVisibility: .visible) {
            Button("Stop", role: .destructive, action: stop)
        } message: {
            Text("The current turn ends now. The session stays open for your next message.")
        }
    }
}

struct SessionHeader: View {
    let session: AgentSession
    var mac: MacTag?

    var body: some View {
        VStack(spacing: 1) {
            Text(session.displayTitle)
                .font(.headline)
                .lineLimit(1)
            TimelineView(.periodic(from: .now, by: 30)) { timeline in
                Self.subtitle(session, mac: mac, now: timeline.date)
                    .font(.footnote)
                    .lineLimit(1)
            }
        }
        .frame(maxWidth: 260)
        .accessibilityElement(children: .combine)
    }

    static func subtitle(_ session: AgentSession, mac: MacTag?, mode: Bool = false, now: Date) -> Text {
        let detail = [session.headerDetail, (mode ? session.shareDetail : session.shareLabel) ?? "", mode ? session.modeLabel ?? "" : ""].filter { !$0.isEmpty }.joined(separator: " · ")
        let activity = session.headerActivity(now: now).map { Text(" · " + $0).foregroundStyle(Color.captionInk) } ?? Text("")
        let open = session.openLabel.map { Text(" · ").foregroundStyle(Color.captionInk) + Text($0).foregroundStyle(Color.warningInk) } ?? Text("")
        let done = session.openLabel != nil || session.archivable
        let tint = session.state.needsYou ? Color.warningInk : done ? Color.successInk : Color.captionInk
        return Text(session.headerState).foregroundStyle(tint)
            + activity
            + open
            + (mac != nil || !detail.isEmpty ? Text(" · ") + mac.lead(detail) : Text("")).foregroundStyle(Color.captionInk)
    }
}

struct FailureActionsView: View {
    let buttons: [FailureAction]
    let onMac: [String]
    let blocked: String?
    let run: (FailureAction) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !buttons.isEmpty {
                HStack(spacing: 8) {
                    ForEach(buttons) { action in
                        Button { run(action) } label: {
                            Label(action.label, systemImage: action.icon)
                                .font(.footnote.weight(.semibold))
                        }
                        .buttonStyle(.glass)
                    }
                }
                .disabled(blocked != nil)
            }
            ForEach(onMac, id: \.self) { line in
                Label(line, systemImage: "desktopcomputer")
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
            }
            if let blocked, !buttons.isEmpty {
                Text(blocked)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(2)
            }
        }
        .padding(.leading, 2)
    }
}

struct ReviewedActionView: View {
    let blocked: String?
    let run: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button(action: run) {
                Label("Reviewed", systemImage: "checkmark")
                    .font(.footnote.weight(.semibold))
            }
            .buttonStyle(.glass)
            .disabled(blocked != nil)
            if let blocked {
                Text(blocked)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(2)
            }
        }
        .padding(.leading, 2)
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
                    .foregroundStyle(Color.captionInk)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Started by")
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
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
            .foregroundStyle(Color.captionInk)
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .glassEffect(in: .capsule)
    }
}

struct DocOpenedBanner: View {
    let name: String
    let open: () -> Void
    let dismiss: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Button(action: dismiss) {
                Label {
                    Text("Opened \(name)").lineLimit(1).truncationMode(.middle)
                } icon: {
                    Image(systemName: "doc.richtext")
                }
                .font(.footnote)
                .foregroundStyle(Color.captionInk)
                .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint("Dismisses")
            Button("Open", action: open)
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
            .foregroundStyle(Color.captionInk)
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
    let dictation: String
    @Binding var attachments: [ComposerAttachment]
    @Binding var focused: Bool
    let session: AgentSession?
    let enabled: Bool
    var blocked: String? = nil
    let sending: Bool
    var wide = false
    var hint: String?
    var trailing: String?
    var reopen: (FailureAction) -> Void = { _ in }
    let send: () -> Void
    @State private var picked: [PhotosPickerItem] = []
    @State private var importing = 0
    @State private var choosingPhotos = false
    @State private var choosingFiles = false
    @State private var tooLarge = false
    @State private var caret = ComposerCaret()
    @Environment(VoiceSession.self) private var voice

    private var attachedBytes: Int {
        attachments.reduce(0) { $0 + $1.bytes }
    }

    private var empty: Bool {
        draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && attachments.isEmpty
    }

    private func closedLabel(short: Bool) -> some View {
        let auto = session?.autoClosed == true
        return Label(short ? (auto ? "Auto-closed" : "Closed") : (auto ? "Closed automatically" : "This session is closed"), systemImage: "archivebox")
            .font(.callout)
            .foregroundStyle(Color.captionInk)
            .lineLimit(1)
            .padding(.horizontal, 18)
            .padding(.vertical, 12)
            .glassEffect()
    }

    @ViewBuilder
    private var reopenButton: some View {
        if let action = session?.reopen {
            Button(action.label, systemImage: action.icon) { reopen(action) }
                .font(.callout.weight(.semibold))
                .lineLimit(1)
                .buttonStyle(.glassProminent)
                .controlSize(.large)
                .disabled(!enabled || blocked != nil)
        }
    }

    private var placeholder: String {
        hint ?? session?.composerPlaceholder ?? "What should the agent do?"
    }

    var body: some View {
        if session?.isClosed == true {
            HStack(spacing: 10) {
                ComposerMic()
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 10) { closedLabel(short: false); reopenButton }
                    HStack(spacing: 10) { closedLabel(short: true); reopenButton }
                    reopenButton
                }
            }
            .padding(.bottom, 8)
        } else {
            GlassEffectContainer(spacing: 10) {
                VStack(alignment: .leading, spacing: 8) {
                    if !attachments.isEmpty || importing > 0 {
                        AttachmentsRow(attachments: $attachments, importing: importing)
                            .disabled(!enabled)
                    }
                    HStack(alignment: .bottom, spacing: 10) {
                        ComposerMic(into: dictation)
                        Menu {
                            Button("Photos or Videos", systemImage: "photo.on.rectangle") { choosingPhotos = true }
                            Button("Files", systemImage: "folder") { choosingFiles = true }
                        } label: {
                            Label("Attach", systemImage: "photo")
                                .labelStyle(.iconOnly)
                                .font(.body.weight(.semibold))
                                .frame(width: 30, height: 30)
                        }
                        .buttonStyle(.glass)
                        .buttonBorderShape(.circle)
                        .accessibilityLabel("Attach")
                        .disabled(!enabled)
                        .photosPicker(isPresented: $choosingPhotos, selection: $picked, maxSelectionCount: 4, matching: .any(of: [.images, .videos]))
                        .fileImporter(isPresented: $choosingFiles, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
                            if case .success(let urls) = result { addFiles(urls) }
                        }
                        ComposerTextView(text: $draft, caret: caret, focused: $focused, placeholder: placeholder, enabled: enabled, pasteImage: paste)
                            .overlay(alignment: .topLeading) {
                                if draft.isEmpty {
                                    Text(placeholder)
                                        .foregroundStyle(Color(uiColor: .placeholderText))
                                        .lineLimit(1)
                                        .allowsHitTesting(false)
                                        .accessibilityHidden(true)
                                }
                            }
                            .overlay(alignment: .trailing) {
                                if let trailing, draft.isEmpty {
                                    Text(trailing)
                                        .font(.caption)
                                        .foregroundStyle(Color.captionInk)
                                        .allowsHitTesting(false)
                                }
                            }
                            .padding(.horizontal, 16)
                            .padding(.vertical, 11)
                            .glassEffect(.regular, in: .rect(cornerRadius: 22))
                            .disabled(!enabled)
                        if !(wide && empty && !sending) {
                            sendButton
                                .disabled(!enabled)
                        }
                    }
                    if let caption = blocked ?? sizeCaption ?? session?.composerCaption(stopInHeader: wide) {
                        Text(caption)
                            .font(.caption)
                            .foregroundStyle(Color.captionInk)
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
            .onAppear {
                if session == nil { focused = true }
            }
            .onChange(of: voice.dictated, initial: true) { insertDictation() }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .frame(maxWidth: 820)
        }
    }

    private var sendButton: some View {
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

    private func insertDictation() {
        guard let text = voice.take(dictation) else { return }
        let inserted = Dictation.insert(text, into: draft, at: caret.offset)
        caret.requested = inserted.caret
        draft = inserted.text
    }

    private func submit() {
        focused = false
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

    private func paste(_ data: Data) {
        if let jpeg = PhotoEncoder.jpeg(data) { attachments.append(.photo(jpeg)) }
    }
}
