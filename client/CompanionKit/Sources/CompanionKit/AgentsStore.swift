import Foundation
import Observation

public enum AnswerError: Error, Equatable, LocalizedError {
    case alreadyAnswered

    static let unknownRequest = "unknown permission request"

    public var errorDescription: String? {
        "Already answered"
    }
}

public enum TranscriptImage: Sendable, Equatable {
    case loading
    case loaded(Data)
    case missing
}

@MainActor
@Observable
public final class AgentsStore {
    public let workspace: String
    private var list = AgentSessionList()
    public private(set) var transcripts: [String: AgentTranscript] = [:]
    public private(set) var loaded = false
    public private(set) var error: String?
    public private(set) var transcriptErrors: [String: String] = [:]
    public private(set) var openedDocs: [String: String] = [:]
    public var expanded: Set<String> = []
    public var searchQuery = "" {
        didSet { if searchQuery != oldValue { scheduleSearch() } }
    }
    public private(set) var searchResults: [AgentSearchResult]?
    public private(set) var searchError: String?
    public private(set) var images: [String: TranscriptImage] = [:]
    public private(set) var runs: [String: GraphRun] = [:]
    public private(set) var runErrors: [String: String] = [:]
    public private(set) var openQueue: [OpenQueueEntry] = []
    public private(set) var openPass: OpenPass?
    private var outgoing: [String: [Outgoing]] = [:]
    private var elicitationDrafts: [String: [String: ElicitationDraft]] = [:]
    private var openDrafts: [String: OpenDraft] = [:]
    private var connectDrafts: [String: ConnectDraft] = [:]
    private var reposFolds: [String: ReposFold] = [:]
    public private(set) var archivePlans: [String: ArchivePlan] = [:]
    public var landLater: Set<String> = []
    public private(set) var foldedTasks: Set<String> = []
    private var foldedOnce: Set<String> = []

    public static let followInterval: Duration = .seconds(2)
    public static let imageCommand = "agents.image"
    public static let reviewedCommand = "agents.reviewed"
    public static let searchCommand = "agents.search"
    public static let searchDelay: Duration = .milliseconds(250)

    @ObservationIgnored private weak var runner: (any CommandRunner)?
    @ObservationIgnored public let queue: ActionQueue?
    @ObservationIgnored private let computer: String
    @ObservationIgnored private let queues: @MainActor () -> Bool
    @ObservationIgnored private let retry: @MainActor () -> Void
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var watched: [String: Int] = [:]
    @ObservationIgnored private var followers: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var inflight: Set<String> = []
    @ObservationIgnored private var again: Set<String> = []
    @ObservationIgnored private var searchTask: Task<Void, Never>?
    @ObservationIgnored private var watchedRuns: [String: Int] = [:]

    public init(workspace: String, runner: any CommandRunner, queue: ActionQueue? = nil, computer: String = "", queues: @escaping @MainActor () -> Bool = { false }, retry: @escaping @MainActor () -> Void = {}, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.workspace = workspace
        self.runner = runner
        self.queue = queue
        self.computer = computer
        self.queues = queues
        self.retry = retry
        self.sleep = sleep
    }

    public var sessions: [AgentSession] {
        list.sessions
    }

    public var groups: [(group: SessionGroup, sessions: [SessionNode])] {
        AgentSession.grouped(sessions)
    }

    public var needsInputCount: Int {
        sessions.count(where: \.needsYou)
    }

    public func session(_ id: String) -> AgentSession? {
        sessions.first { $0.id == id } ?? transcripts[id]?.session
    }

    public func entries(_ session: String) -> [TranscriptEntry] {
        (transcripts[session]?.entries ?? []) + (outgoing[session] ?? []).map(\.entry)
    }

    public func pendingPermission(_ session: String) -> PermissionRequest? {
        guard let pending = self.session(session)?.pending, !pending.isEmpty else { return nil }
        return entries(session).lazy.compactMap { entry -> PermissionRequest? in
            guard case .permission(let request) = entry.content, request.outcome == nil, pending.contains(request.id) else { return nil }
            return request
        }.first
    }

    public func handle(_ signal: ConnectionSignal) {
        switch signal {
        case .online:
            Task { await refreshAll() }
        case .event(let event) where event.topic == Topic.agentsChanged:
            let target = event.payload["workspace"]?.stringValue ?? event.workspace
            guard target.isEmpty || target == workspace else { return }
            Task { await refresh() }
            if let session = event.payload["session"]?.stringValue, watched[session] != nil {
                Task { await loadTranscript(session, incremental: true) }
            }
            for run in watchedRuns.keys {
                Task { await loadRun(run) }
            }
        case .event(let event) where event.topic == Topic.docsOpened:
            let target = event.payload["workspace"]?.stringValue ?? event.workspace
            guard target.isEmpty || target == workspace, let session = event.payload["session"]?.stringValue, watched[session] != nil, let path = event.payload["path"]?.stringValue else { return }
            openedDocs[session] = path
        case .event:
            break
        }
    }

    public func image(_ path: String, entry: String, in session: String) -> TranscriptImage? {
        images[session + "|" + entry + "|" + path]
    }

    public func loadImage(_ path: String, entry: String, in session: String) async {
        let key = session + "|" + entry + "|" + path
        switch images[key] {
        case .loading, .loaded: return
        case .missing, nil: break
        }
        guard let runner else { return }
        images[key] = .loading
        do {
            let reply = try await runner.command(Self.imageCommand, workspace: workspace, args: .object(["session": .string(session), "path": .string(path)]))
            images[key] = reply["data"]?.stringValue.flatMap { Data(base64Encoded: $0) }.map(TranscriptImage.loaded) ?? .missing
        } catch {
            images[key] = .missing
        }
    }

    public func dismissOpenedDoc(_ session: String) {
        openedDocs[session] = nil
    }

    public func refreshAll() async {
        await refresh()
        for session in watched.keys {
            await loadTranscript(session, incremental: true)
        }
        for run in watchedRuns.keys {
            await loadRun(run)
        }
    }

    public func refresh() async {
        await coalesced("sessions") { [weak self] in
            await Task { [weak self] in await self?.loadSessions() }.value
        }
    }

    private func loadSessions() async {
        guard let runner else { return }
        do {
            let reply = try await runner.command(AgentCommand.sessions, workspace: workspace, args: list.args)
            try list.apply(reply)
            error = nil
        } catch {
            self.error = Self.message(error)
        }
        loaded = true
        await loadOpenQueue()
    }

    public var searching: Bool {
        !searchQuery.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func scheduleSearch() {
        searchTask?.cancel()
        searchResults = nil
        searchError = nil
        guard searching else { return }
        let query = searchQuery
        searchTask = Task { [weak self, sleep] in
            do {
                try await sleep(Self.searchDelay)
            } catch {
                return
            }
            await self?.search(query)
        }
    }

    private func search(_ query: String) async {
        guard let runner else { return }
        do {
            let results = try await runner.command(Self.searchCommand, workspace: workspace, args: .object(["query": .string(query)]), as: [AgentSearchResult].self)
            guard query == searchQuery else { return }
            searchResults = results
        } catch {
            guard query == searchQuery, !Task.isCancelled else { return }
            searchResults = []
            searchError = Self.message(error)
        }
    }

    public func watch(_ session: String) {
        watched[session, default: 0] += 1
        guard followers[session] == nil else { return }
        followers[session] = Task { [weak self] in await self?.follow(session) }
    }

    public func unwatch(_ session: String) {
        guard let count = watched[session] else { return }
        guard count == 1 else {
            watched[session] = count - 1
            return
        }
        watched[session] = nil
        followers.removeValue(forKey: session)?.cancel()
    }

    public func watchRun(_ id: String) {
        watchedRuns[id, default: 0] += 1
    }

    public func unwatchRun(_ id: String) {
        guard let count = watchedRuns[id] else { return }
        watchedRuns[id] = count == 1 ? nil : count - 1
    }

    public func loadRun(_ id: String) async {
        await coalesced("run/" + id) { [weak self] in
            guard let self, let runner else { return }
            do {
                let run = try await runner.command(GraphCommand.show, workspace: workspace, args: .object(["id": .string(id)]), as: GraphRun.self)
                if runs[id] != run { runs[id] = run }
                runErrors[id] = nil
            } catch {
                if Task.isCancelled { return }
                runErrors[id] = Self.message(error)
            }
        }
    }

    public func approve(run id: String, node: String, decision: String) async throws {
        try await run(GraphCommand.approve, ["id": .string(id), "node": .string(node), "decision": .string(decision)])
        await refreshRun(id)
    }

    public func continueRun(_ id: String, edge: Int) async throws {
        try await run(GraphCommand.continue, ["id": .string(id), "edge": .number(Double(edge))])
        await refreshRun(id)
    }

    public func stopRun(_ id: String) async throws {
        try await run(GraphCommand.stop, ["id": .string(id)])
        await refreshRun(id)
    }

    private func refreshRun(_ id: String) async {
        await refresh()
        await loadRun(id)
    }

    public func loadTranscript(_ session: String, incremental: Bool = false) async {
        await coalesced("transcript/" + session) { [weak self] in
            guard let self, let runner else { return }
            var failure: (any Error)?
            for _ in 0..<2 {
                do {
                    let loaded = incremental ? transcripts[session] : nil
                    let fromTurn = loaded?.lastTurn ?? 0
                    var args: [String: JSONValue] = ["session": .string(session), "compact": .bool(true)]
                    if fromTurn > 0 { args["fromTurn"] = .number(Double(fromTurn)) }
                    if let cursor = loaded?.cursor { args["since"] = .string(cursor) }
                    let fetched = try await runner.command("agents.transcript", workspace: workspace, args: .object(args), as: AgentTranscript.self)
                    let merged = if let loaded, fetched.delta { loaded.merging(delta: fetched) } else if let loaded, fromTurn > 0 { loaded.merging(fetched, fromTurn: fromTurn) } else { fetched }
                    if transcripts[session] != merged { transcripts[session] = merged }
                    transcriptErrors[session] = nil
                    reconcile(session)
                    return
                } catch {
                    if Task.isCancelled { return }
                    failure = error
                }
            }
            transcriptErrors[session] = failure.map(Self.message)
        }
    }

    private func follow(_ session: String) async {
        await load(session)
        while !Task.isCancelled {
            do {
                try await sleep(Self.followInterval)
            } catch {
                return
            }
            guard sessions.first(where: { $0.id == session })?.status == .running else { continue }
            await load(session)
            if let status = transcripts[session]?.session.status, status != .running { await refresh() }
        }
    }

    private func load(_ session: String) async {
        await Task { await loadTranscript(session, incremental: true) }.value
    }

    public func send(_ text: String, attachments: [ComposerAttachment] = [], to session: String) async throws {
        let images = attachments.flatMap(\.images)
        let prompt = ComposerAttachment.promptText(text, attachments)
        var args: [String: JSONValue] = ["session": .string(session), "text": .string(prompt)]
        if !attachments.isEmpty {
            args["attachments"] = .array(attachments.flatMap(\.payload))
        }
        let echo = self.echo(prompt, images: images, files: attachments.compactMap(\.fileName), in: session)
        do {
            try await run("agents.prompt", args)
        } catch {
            outgoing[session]?.removeAll { $0.entry.id == echo }
            throw error
        }
        if let i = outgoing[session]?.firstIndex(where: { $0.entry.id == echo }) {
            outgoing[session]?[i].sent = true
        }
        await refreshSession(session)
    }

    public func cancel(_ session: String) async throws {
        try await run("agents.cancel", ["session": .string(session)])
        await refreshSession(session)
    }

    public func stopSubagent(_ subagent: String, in session: String) async throws {
        try await run("agents.stop-subagent", ["session": .string(session), "subagent": .string(subagent)])
        await refreshSession(session)
    }

    public func answer(_ session: String, request: String, option: String) async throws {
        guard let runner else { return }
        list.replace(try await runner.command(AgentCommand.sessions, workspace: workspace, args: .object(["session": .string(session)]), as: [AgentSession].self), for: session)
        do {
            guard self.session(session)?.pending.contains(request) == true else { throw AnswerError.alreadyAnswered }
            try await run("agents.permission", ["session": .string(session), "request": .string(request), "option": .string(option)])
        } catch RemoteError.server(_, let message) where message.contains(AnswerError.unknownRequest) {
            await refreshSession(session)
            throw AnswerError.alreadyAnswered
        } catch AnswerError.alreadyAnswered {
            await refreshSession(session)
            throw AnswerError.alreadyAnswered
        }
        await refreshSession(session)
    }

    public func respond(_ session: String, request: String, action: String, answers: [Elicitation.Answer] = []) async throws {
        var args: [String: JSONValue] = ["session": .string(session), "request": .string(request), "action": .string(action)]
        if !answers.isEmpty {
            args["answers"] = .array(answers.map { .object(["key": .string($0.key), "value": .string($0.value)]) })
        }
        try await run("agents.elicitation", args)
        setElicitationDraft(ElicitationDraft(), session: session, request: request)
        await refreshSession(session)
    }

    public func request(_ session: String) -> RequestCard? {
        guard let card = transcripts[session]?.request, self.session(session)?.requests.contains(card.request) == true else { return nil }
        return card
    }

    public func repos(_ session: String) -> ReposCard? {
        transcripts[session]?.repos
    }

    public func connectDraft(_ request: String) -> ConnectDraft {
        connectDrafts[request] ?? ConnectDraft()
    }

    public func setConnectDraft(_ draft: ConnectDraft, request: String) {
        connectDrafts[request] = draft
    }

    public func reposFold(_ session: String) -> ReposFold {
        reposFolds[session] ?? ReposFold()
    }

    public func setReposFold(_ fold: ReposFold, session: String) {
        reposFolds[session] = fold
    }

    public func answerConnect(_ session: String, card: ConnectCard, answers: [ConnectAnswer]) async throws -> Delivery {
        let action = QueuedAction(computer: computer, workspace: workspace, session: session, kind: .connect, title: "Connect \(card.subject)", request: card.request, seen: card.seen, answers: answers)
        if queues(), let queue {
            queue.enqueue(action)
            return .queued
        }
        if queue?.holdsAttempted(action) == true { return .queued }
        try await ensureWaiting(session, request: card.request)
        do {
            guard try await deliver(action, TaskCommand.connectAnswer, action.connectArgs) != nil else { return .queued }
        } catch RemoteError.server(ServerReason.answered, _) {
            await refreshSession(session)
            throw AnswerError.alreadyAnswered
        }
        connectDrafts[card.request] = nil
        await refreshSession(session)
        return .sent
    }

    public func land(_ session: String, card: LandCard, action: String, tip: String? = nil) async throws {
        guard !queues() else { throw OfflineRefused() }
        try await ensureWaiting(session, request: card.request)
        var args: [String: JSONValue] = ["request": .string(card.request), "action": .string(action)]
        if let tip { args["tip"] = .string(tip) }
        do {
            try await run(TaskCommand.landAnswer, args)
        } catch RemoteError.server(ServerReason.answered, _) {
            await refreshSession(session)
            throw AnswerError.alreadyAnswered
        }
        await refreshSession(session)
    }

    public func landCommits(_ request: String) async throws -> LandCommits {
        guard let runner else { throw RemoteError.badResponse }
        return try await runner.command(TaskCommand.landCommits, workspace: workspace, args: .object(["request": .string(request)]), as: LandCommits.self)
    }

    public func repoFix(_ session: String, repo: String) async throws {
        try await run(TaskCommand.repoFix, ["session": .string(session), "repo": .string(repo)])
        await refreshSession(session)
    }

    public func repoLeave(_ session: String, repo: String) async throws {
        try await run(TaskCommand.repoLeave, ["session": .string(session), "repo": .string(repo)])
        await refreshSession(session)
    }

    public func loadArchivePlan(_ session: String) async throws {
        guard let runner else { return }
        archivePlans[session] = try await runner.command(TaskCommand.archivePlan, workspace: workspace, args: .object(["session": .string(session)]), as: ArchivePlan.self)
    }

    public func archive(_ session: String, plan: ArchivePlan, mode: ArchiveMode) async throws -> (Delivery, kept: [String]) {
        let title = mode == .archive ? "Archive \(plan.title)" : "Close \(plan.title)"
        let action = QueuedAction(computer: computer, workspace: workspace, session: session, kind: .archive, title: title, seen: plan.seen, choices: mode == .archive ? plan.choices : [])
        if queues(), let queue {
            queue.enqueue(action)
            return (.queued, [])
        }
        if queue?.holdsAttempted(action) == true { return (.queued, []) }
        guard let result = try await deliver(action, TaskCommand.archive, action.archiveArgs) else { return (.queued, []) }
        archivePlans[session] = nil
        await refreshSession(session)
        return (.sent, result["kept"]?.arrayValue?.compactMap(\.stringValue) ?? [])
    }

    public func removeFolder(_ path: String) async throws -> String? {
        guard let runner else { return nil }
        let result = try await runner.command(TaskCommand.folderRemove, workspace: workspace, args: .object(["path": .string(path)]))
        await refresh()
        return result["held"]?.stringValue
    }

    private func deliver(_ action: QueuedAction, _ command: String, _ args: JSONValue) async throws -> JSONValue? {
        guard let runner else { return .null }
        do {
            return try await runner.command(command, workspace: workspace, args: args)
        } catch let error where Self.outcomeLost(error) && queue != nil {
            var kept = action
            kept.attempted = true
            queue?.enqueue(kept)
            Task { [sleep, retry] in
                try? await sleep(Self.retryDelay)
                retry()
            }
            return nil
        }
    }

    private static func outcomeLost(_ error: any Error) -> Bool {
        switch error as? RemoteError {
        case .unreachable, .http: true
        default: false
        }
    }

    private static let retryDelay: Duration = .seconds(5)

    private func ensureWaiting(_ session: String, request: String) async throws {
        guard let runner else { return }
        list.replace(try await runner.command(AgentCommand.sessions, workspace: workspace, args: .object(["session": .string(session)]), as: [AgentSession].self), for: session)
        guard self.session(session)?.requests.contains(request) == true else {
            await refreshSession(session)
            throw AnswerError.alreadyAnswered
        }
    }

    public func toggleTree(_ item: SessionRowItem) {
        if item.expanded {
            expanded.remove(item.id)
            foldedTasks.insert(item.id)
        } else {
            foldedTasks.remove(item.id)
            expanded.insert(item.id)
        }
    }

    public func opened(_ session: String) {
        guard self.session(session)?.taskFinished == true, foldedOnce.insert(session).inserted else { return }
        expanded.remove(session)
        foldedTasks.insert(session)
    }

    public func elicitationDraft(_ session: String, request: String) -> ElicitationDraft {
        elicitationDrafts[session]?[request] ?? ElicitationDraft()
    }

    public func setElicitationDraft(_ draft: ElicitationDraft, session: String, request: String) {
        elicitationDrafts[session, default: [:]][request] = draft.isEmpty ? nil : draft
        if elicitationDrafts[session]?.isEmpty == true { elicitationDrafts[session] = nil }
    }

    public func loadOpenQueue() async {
        guard sessions.contains(where: { $0.openCount > 0 }) else {
            setOpenQueue([])
            return
        }
        await coalesced("open") { [weak self] in
            guard let self, let runner else { return }
            guard let queue = try? await runner.command(OpenCommand.queue, workspace: workspace, args: .object([:]), as: [OpenQueueEntry].self) else { return }
            setOpenQueue(queue)
        }
    }

    private func setOpenQueue(_ queue: [OpenQueueEntry]) {
        if openQueue != queue { openQueue = queue }
        openPass?.update(queue)
    }

    public func openKinds(_ session: String) -> OpenKinds? {
        openQueue.first { $0.session == session }.map { OpenKinds($0.items) }
    }

    public func beginOpenPass() {
        if openPass == nil, !openQueue.isEmpty { openPass = OpenPass(openQueue) }
    }

    public func endOpenPass() {
        openPass = nil
    }

    public func moveOpenPass(_ move: (inout OpenPass) -> Void) {
        openPass.map { var pass = $0; move(&pass); openPass = pass }
    }

    public func openDraft(_ session: String) -> OpenDraft {
        openDrafts[session] ?? OpenDraft()
    }

    public func setOpenDraft(_ draft: OpenDraft, session: String) {
        openDrafts[session] = draft.isEmpty ? nil : draft
    }

    public func answerOpen(_ session: String, items: [OpenItem], note: String = "") async throws {
        var draft = openDraft(session)
        try await run(OpenCommand.answer, draft.reply(session: session, items: items, note: note))
        draft.clear(items)
        setOpenDraft(draft, session: session)
        await afterOpen(session)
    }

    public func laterOpen(_ session: String) async throws {
        try await run(OpenCommand.later, ["session": .string(session)])
        var draft = openDraft(session)
        draft.unfolded = false
        setOpenDraft(draft, session: session)
        await afterOpen(session)
    }

    public func backlogOpen(_ session: String, items: [OpenItem] = []) async throws {
        try await close(OpenCommand.backlog, session, items)
    }

    public func noOpen(_ session: String, items: [OpenItem] = []) async throws {
        try await close(OpenCommand.no, session, items)
    }

    public func convertOpen(_ session: String) async throws {
        try await run(OpenCommand.convert, ["session": .string(session)])
        await afterOpen(session)
    }

    public func sendOpenPass() async throws {
        var failure: (any Error)?
        for session in openPass?.sessions ?? [] {
            let items = openPass?.items(session) ?? []
            guard !openDraft(session).answered(items).isEmpty else { continue }
            do {
                try await answerOpen(session, items: items)
            } catch {
                failure = failure ?? error
            }
        }
        if let failure { throw failure }
    }

    private func close(_ command: String, _ session: String, _ items: [OpenItem]) async throws {
        var args: [String: JSONValue] = ["session": .string(session)]
        if !items.isEmpty { args["items"] = .array(items.map { .string($0.id) }) }
        try await run(command, args)
        var draft = openDraft(session)
        if items.isEmpty { draft = OpenDraft() } else { draft.clear(items) }
        setOpenDraft(draft, session: session)
        await afterOpen(session)
    }

    private func afterOpen(_ session: String) async {
        await refresh()
        await loadTranscript(session, incremental: true)
    }

    public func setMode(_ session: String, mode: String) async throws {
        try await run("agents.mode", ["session": .string(session), "mode": .string(mode)])
        await refreshSession(session)
    }

    public func setConfig(_ session: String, id: String, value: String) async throws {
        try await run("agents.config", ["session": .string(session), "id": .string(id), "value": .string(value)])
        await refreshSession(session)
    }

    public func setNotify(_ session: String, policy: NotificationPolicy) async throws {
        try await run("agents.notify", ["session": .string(session), "policy": .string(policy.rawValue)])
        await refreshSession(session)
    }

    public func close(_ session: String) async throws {
        try await run("agents.close", ["session": .string(session)])
        await refreshSession(session)
    }

    public func reviewed(_ session: String) async throws {
        try await run(Self.reviewedCommand, ["session": .string(session)])
        await refreshSession(session)
    }

    public func recover(_ session: String, with action: FailureAction) async throws -> String? {
        guard let runner, let command = action.command else { return nil }
        let result = try await runner.command(command, workspace: workspace, args: .object((action.args ?? [:]).mapValues(JSONValue.string)))
        await refreshSession(session)
        return result["session"]?.stringValue.flatMap { $0 == session ? nil : $0 }
    }

    public func create(_ text: String, attachments: [ComposerAttachment] = []) async throws -> String? {
        guard let runner else { return nil }
        let prompt = attachments.isEmpty ? text : ""
        let result = try await runner.command("agents.new", workspace: workspace, args: .object(prompt.isEmpty ? [:] : ["prompt": .string(prompt)]))
        guard let id = result["session"]?.stringValue, !attachments.isEmpty else {
            if let id = result["session"]?.stringValue, !prompt.isEmpty {
                echo(prompt, images: [], in: id, sent: true)
            }
            await refresh()
            return result["session"]?.stringValue
        }
        try await send(text, attachments: attachments, to: id)
        return id
    }

    private func run(_ command: String, _ args: [String: JSONValue]) async throws {
        guard let runner else { return }
        _ = try await runner.command(command, workspace: workspace, args: .object(args))
    }

    @discardableResult
    private func echo(_ text: String, images: [Data], files: [String] = [], in session: String, sent: Bool = false) -> String {
        let entry = TranscriptEntry(outgoing: text, images: images, files: files)
        let known = Set((transcripts[session]?.entries ?? []).map(\.id))
        outgoing[session, default: []].append(Outgoing(entry: entry, text: text, known: known, sent: sent))
        return entry.id
    }

    private func reconcile(_ session: String) {
        guard let transcript = transcripts[session], let list = outgoing[session] else { return }
        let kept = list.filter { prompt in
            if prompt.sent && ![.running, .waiting].contains(transcript.session.status) { return false }
            return !transcript.entries.contains { entry in
                guard !prompt.known.contains(entry.id), case .user(let text, _, _, _) = entry.content else { return false }
                return text.contains(prompt.text)
            }
        }
        outgoing[session] = kept.isEmpty ? nil : kept
    }

    private func refreshSession(_ session: String) async {
        await refresh()
        if watched[session] != nil { await loadTranscript(session, incremental: true) }
    }

    private func coalesced(_ key: String, _ work: @MainActor () async -> Void) async {
        guard !inflight.contains(key) else {
            again.insert(key)
            return
        }
        inflight.insert(key)
        defer { inflight.remove(key) }
        repeat {
            again.remove(key)
            await work()
        } while again.contains(key)
    }

    private struct Outgoing {
        let entry: TranscriptEntry
        let text: String
        let known: Set<String>
        var sent: Bool
    }

    private static func message(_ error: any Error) -> String {
        (error as? RemoteError)?.message ?? error.localizedDescription
    }
}
