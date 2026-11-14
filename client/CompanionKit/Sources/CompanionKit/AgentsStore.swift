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
    public private(set) var syncedAt: Date?
    public private(set) var fetched = false
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
    public var arrived: String?
    private var outgoing: [String: [Outgoing]] = [:]
    private var elicitationDrafts: [String: [String: ElicitationDraft]] = [:]
    private var openDrafts: [String: OpenDraft] = [:]
    private var connectDrafts: [String: ConnectDraft] = [:]
    private var reposFolds: [String: ReposFold] = [:]
    public private(set) var archivePlans: [String: ArchivePlan] = [:]
    public var landLater: Set<String> = []
    public private(set) var closingOpen: Set<String> = []
    public private(set) var foldedTasks: Set<String> = []
    private var foldedOnce: Set<String> = []

    public static let followInterval: Duration = .seconds(2)
    public static let imageCommand = "agents.image"
    public static let reviewedCommand = "agents.reviewed"
    public static let searchCommand = "agents.search"
    public static let searchDelay: Duration = .milliseconds(250)
    public static let imageLimit = 48
    public static let openNoUndo: Duration = .seconds(5)
    public static let imageBytes = 48 << 20

    @ObservationIgnored private weak var runner: (any CommandRunner)?
    @ObservationIgnored private var imageOrder: [String] = []
    @ObservationIgnored public let queue: ActionQueue?
    @ObservationIgnored private let computer: String
    @ObservationIgnored private let cache: ListCache
    @ObservationIgnored private let saves = SaveThrottle()
    @ObservationIgnored private let queues: @MainActor () -> Bool
    @ObservationIgnored private let retry: @MainActor () -> Void
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var watched: [String: Int] = [:]
    @ObservationIgnored public private(set) var shown: String?
    @ObservationIgnored private var followers: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var inflight: Set<String> = []
    @ObservationIgnored private var again: Set<String> = []
    @ObservationIgnored private var searchTask: Task<Void, Never>?
    @ObservationIgnored private var watchedRuns: [String: Int] = [:]
    @ObservationIgnored private var closings: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var promptKnown: [String: Set<String>] = [:]

    public init(workspace: String, runner: any CommandRunner, queue: ActionQueue? = nil, computer: String = "", cache: ListCache = ListCache(dir: nil), queues: @escaping @MainActor () -> Bool = { false }, retry: @escaping @MainActor () -> Void = {}, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.workspace = workspace
        self.runner = runner
        self.queue = queue
        self.computer = computer
        self.cache = cache
        self.queues = queues
        self.retry = retry
        self.sleep = sleep
        if let saved = cache.load(cacheKey, as: Snapshot.self), saved.build == Snapshot.build {
            list = saved.list
            syncedAt = saved.at
            loaded = true
        }
    }

    public func ageLine(now: Date = .now) -> String? {
        ListCache.ageLine(syncedAt: syncedAt, error: error, empty: sessions.isEmpty, now: now)
    }

    private var cacheKey: ListCache.Key {
        ListCache.Key(computer: computer, workspace: workspace, collection: "sessions")
    }

    private struct Snapshot: Codable {
        static let build = [Bundle.main.infoDictionary?["CFBundleShortVersionString"], Bundle.main.infoDictionary?["CFBundleVersion"]].compactMap { $0 as? String }.joined(separator: "+")

        var build = Self.build
        let list: AgentSessionList
        let at: Date
    }

    public var sessions: [AgentSession] {
        list.items
    }

    public var groups: [(group: SessionGroup, sessions: [SessionNode])] {
        AgentSession.grouped(sessions)
    }

    public var needsInputCount: Int {
        fetched ? sessions.count(where: \.needsYou) : 0
    }

    public func session(_ id: String) -> AgentSession? {
        sessions.first { $0.id == id } ?? transcripts[id]?.session
    }

    public func entries(_ session: String) -> [TranscriptEntry] {
        (transcripts[session]?.entries ?? []) + (outgoing[session] ?? []).map(\.entry) + queuedPrompts(session)
    }

    private func queuedPrompts(_ session: String) -> [TranscriptEntry] {
        writes.filter { $0.command == AgentCommand.prompt && $0.session == session }.map { TranscriptEntry(outgoing: $0.text ?? "", images: [], entryID: $0.id) }
    }

    public var pendingCreates: [QueuedAction] {
        writes.filter { $0.command == AgentCommand.new }
    }

    private var writes: [QueuedAction] {
        queue?.writes(computer: computer, workspace: workspace) ?? []
    }

    public func landed(_ landed: Landed) {
        let item = landed.item
        guard item.workspace == workspace else { return }
        switch item.command {
        case AgentCommand.new:
            guard let id = landed.result["session"]?.stringValue else { return }
            if let text = item.text { echo(text, images: [], in: id, sent: true) }
            arrived = id
        case AgentCommand.prompt:
            echo(item.text ?? "", images: [], in: item.session, sent: true, known: promptKnown.removeValue(forKey: item.id))
            reconcile(item.session)
        default:
            break
        }
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
            guard images[key] == .loading else { return }
            guard let data = reply["data"]?.stringValue.flatMap({ Data(base64Encoded: $0) }) else {
                images[key] = .missing
                return
            }
            images[key] = .loaded(data)
            keepImage(key)
        } catch {
            guard images[key] == .loading else { return }
            images[key] = .missing
        }
    }

    private func keepImage(_ key: String) {
        imageOrder.append(key)
        var bytes = imageOrder.reduce(0) { total, key in
            guard case .loaded(let data) = images[key] else { return total }
            return total + data.count
        }
        while imageOrder.count > 1, imageOrder.count > Self.imageLimit || bytes > Self.imageBytes {
            let oldest = imageOrder.removeFirst()
            if case .loaded(let data) = images[oldest] { bytes -= data.count }
            images[oldest] = nil
        }
    }

    private func forget(_ session: String) {
        let prefix = session + "|"
        images = images.filter { !$0.key.hasPrefix(prefix) }
        imageOrder.removeAll { $0.hasPrefix(prefix) }
        guard self.session(session)?.needsYou != true else { return }
        transcripts[session] = nil
        transcriptErrors[session] = nil
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
            if let rows = reply.arrayValue { try list.replace(rows) } else { try list.apply(reply) }
            error = nil
            fetched = true
            syncedAt = .now
            saves.request { [weak self] in
                guard let self, let at = self.syncedAt else { return }
                self.cache.save(Snapshot(list: self.list, at: at), self.cacheKey)
            }
        } catch {
            self.error = Self.message(error)
        }
        loaded = true
        if shown == AgentsPluginInfo.openQueue { await loadOpenQueue() }
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

    public func show(_ detail: String?) {
        guard detail != shown else { return }
        if let shown, Self.follows(shown) { unwatch(shown) }
        shown = detail
        if let detail, Self.follows(detail) { watch(detail) }
        if detail == AgentsPluginInfo.openQueue { Task { await loadOpenQueue() } }
    }

    private static func follows(_ detail: String) -> Bool {
        AgentsPluginInfo.isSession(detail) && !GraphRun.isRun(detail)
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
        forget(session)
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
        try await submit(GraphCommand.approve, ["id": .string(id), "node": .string(node), "decision": .string(decision)], target: "run/" + id, title: "Answer \(node)")
        await refreshRun(id)
    }

    public func continueRun(_ id: String, edge: Int) async throws {
        try await submit(GraphCommand.continue, ["id": .string(id), "edge": .number(Double(edge))], target: "run/" + id, title: "Continue the run")
        await refreshRun(id)
    }

    public func stopRun(_ id: String) async throws {
        try await submit(GraphCommand.stop, ["id": .string(id)], target: "run/" + id, title: "Stop the run")
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
        let prompt = ComposerAttachment.promptText(text, attachments)
        var args: [String: JSONValue] = ["session": .string(session), "text": .string(prompt)]
        if attachments.isEmpty {
            let item = QueuedAction(computer: computer, workspace: workspace, command: AgentCommand.prompt, args: .object(args), target: QueuedAction.sessionTarget + session, title: Self.sendTitle(prompt), text: prompt)
            let known = Set((transcripts[session]?.entries ?? []).map(\.id))
            promptKnown[item.id] = known
            if try await submit(item) != nil {
                promptKnown[item.id] = nil
                echo(prompt, images: [], in: session, sent: true, known: known)
            }
            await refreshSession(session)
            return
        }
        guard !queues() else { throw OfflineRefused("Sending attachments") }
        args["attachments"] = .array(attachments.flatMap(\.payload))
        let echo = self.echo(prompt, images: attachments.flatMap(\.images), files: attachments.compactMap(\.fileName), in: session)
        do {
            _ = try await runner?.command(AgentCommand.prompt, workspace: workspace, args: .object(args), requestId: UUID().uuidString)
        } catch {
            outgoing[session]?.removeAll { $0.entry.id == echo }
            throw error
        }
        if let i = outgoing[session]?.firstIndex(where: { $0.entry.id == echo }) {
            outgoing[session]?[i].sent = true
        }
        await refreshSession(session)
    }

    static func sendTitle(_ prompt: String) -> String {
        "Send " + quoted(prompt)
    }

    static func quoted(_ text: String) -> String {
        let line = text.split(whereSeparator: \.isNewline).first.map(String.init) ?? text
        return "“\(line.count > 40 ? line.prefix(40) + "…" : line)”"
    }

    public func cancel(_ session: String) async throws {
        var args: [String: JSONValue] = ["session": .string(session)]
        if let turn = currentTurn(session) { args["turn"] = .number(Double(turn)) }
        try await stopping(session) { try await self.submit("agents.cancel", args, target: QueuedAction.sessionTarget + session, title: "Stop the turn") }
    }

    public func stopSubagent(_ subagent: String, in session: String) async throws {
        var args: [String: JSONValue] = ["session": .string(session), "subagent": .string(subagent)]
        if let turn = entries(session).last(where: { $0.entryID == subagent && $0.turn > 0 })?.turn { args["turn"] = .number(Double(turn)) }
        try await stopping(session) { try await self.submit("agents.stop-subagent", args, target: QueuedAction.sessionTarget + session, title: "Stop the subagent") }
    }

    private func stopping(_ session: String, _ send: () async throws -> JSONValue?) async throws {
        do {
            _ = try await send()
        } catch RemoteError.server(let code, let message) where code == ServerReason.stale {
            await refreshSession(session)
            throw RemoteError.server(code: code, message: message)
        }
        await refreshSession(session)
    }

    private func sessionRows(_ session: String) async throws -> [JSONValue] {
        guard let runner, let rows = try await runner.command(AgentCommand.sessions, workspace: workspace, args: .object(["session": .string(session)])).arrayValue else { throw RemoteError.badResponse }
        return rows
    }

    func currentTurn(_ session: String) -> Int? {
        let turn = max(self.session(session)?.turns ?? 0, transcripts[session]?.entries.map(\.turn).max() ?? 0)
        return turn > 0 ? turn : nil
    }

    public func answer(_ session: String, request: String, option: String) async throws {
        guard let runner else { return }
        let online = !queues()
        if online {
            try list.replace(try await sessionRows(session), for: session)
        }
        do {
            guard !online || self.session(session)?.pending.contains(request) == true else { throw AnswerError.alreadyAnswered }
            try await submit("agents.permission", ["session": .string(session), "request": .string(request), "option": .string(option)], target: "request/" + request, title: "Answer the permission request")
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
        try await submit("agents.elicitation", args, target: "request/" + request, title: "Answer the question", text: answers.map(\.value).joined(separator: "\n").nilIfEmpty)
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
            try await submit(TaskCommand.landAnswer, args, target: "request/" + card.request, title: "Merge", keeps: false)
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
        try await submit(TaskCommand.repoFix, ["session": .string(session), "repo": .string(repo)], target: QueuedAction.sessionTarget + session, title: "Fix \(repo)", keeps: false)
        await refreshSession(session)
    }

    public func repoLeave(_ session: String, repo: String) async throws {
        try await submit(TaskCommand.repoLeave, ["session": .string(session), "repo": .string(repo)], target: QueuedAction.sessionTarget + session, title: "Leave \(repo) unlanded", keeps: false)
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
        let result = try await submit(TaskCommand.folderRemove, ["path": .string(path)], target: "folder/" + path, title: "Remove \(URL(filePath: path).lastPathComponent)", keeps: false)
        await refresh()
        return result?["held"]?.stringValue
    }

    private func deliver(_ action: QueuedAction, _ command: String, _ args: JSONValue) async throws -> JSONValue? {
        guard let runner else { return .null }
        do {
            return try await runner.command(command, workspace: workspace, args: args, requestId: action.id)
        } catch let error where Self.keeps(error) && queue != nil {
            var kept = action
            kept.attempted = Self.outcomeLost(error)
            kept.deduped = runner.dedupes
            queue?.enqueue(kept)
            scheduleRetry()
            return nil
        }
    }

    private func scheduleRetry() {
        Task { [sleep, retry] in
            try? await sleep(ActionQueue.retryDelay)
            retry()
        }
    }

    @discardableResult
    private func submit(_ command: String, _ args: [String: JSONValue], target: String, title: String, text: String? = nil, keeps: Bool = true) async throws -> JSONValue? {
        try await submit(QueuedAction(computer: computer, workspace: workspace, command: command, args: .object(args), target: target, title: title, text: text), keeps: keeps)
    }

    @discardableResult
    private func submit(_ item: QueuedAction, keeps: Bool = true) async throws -> JSONValue? {
        guard let runner else { return nil }
        guard let queue else { return try await runner.command(item.command, workspace: workspace, args: item.sendArgs, requestId: item.id) }
        let offline = queues()
        if !keeps, offline { throw OfflineRefused(item.title) }
        guard let result = try await queue.submit(item, runner: runner, queues: offline, keeps: keeps) else {
            if !offline { scheduleRetry() }
            return nil
        }
        return result
    }

    private static func keeps(_ error: any Error) -> Bool {
        if case .notSent = error as? RemoteError { return true }
        return outcomeLost(error)
    }

    private static func outcomeLost(_ error: any Error) -> Bool {
        ActionQueue.outcomeLost(error)
    }

    private func ensureWaiting(_ session: String, request: String) async throws {
        guard let runner else { return }
        try list.replace(try await sessionRows(session), for: session)
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
        try await submit(OpenCommand.answer, draft.reply(session: session, items: items, note: note), target: QueuedAction.sessionTarget + session, title: "Answer open items", text: note.nilIfEmpty)
        draft.clear(items)
        setOpenDraft(draft, session: session)
        await afterOpen(session)
    }

    public func laterOpen(_ session: String) async throws {
        try await submit(OpenCommand.later, ["session": .string(session)], target: QueuedAction.sessionTarget + session, title: "Open items later")
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

    public func noOpenSoon(_ session: String, failed: @escaping @MainActor (any Error) -> Void) {
        guard closings[session] == nil else { return }
        closingOpen.insert(session)
        closings[session] = Task { [weak self, sleep = self.sleep] in
            do { try await sleep(Self.openNoUndo) } catch { return }
            guard let self, self.closings.removeValue(forKey: session) != nil else { return }
            do { try await self.commitNo(session) } catch { failed(error) }
        }
    }

    public func undoNoOpen(_ session: String) {
        closings.removeValue(forKey: session)?.cancel()
        closingOpen.remove(session)
    }

    public func flushNoOpen(_ session: String) async throws {
        guard let task = closings.removeValue(forKey: session) else { return }
        task.cancel()
        try await commitNo(session)
    }

    private func commitNo(_ session: String) async throws {
        defer { closingOpen.remove(session) }
        try await noOpen(session)
    }

    public func convertOpen(_ session: String) async throws {
        try await submit(OpenCommand.convert, ["session": .string(session)], target: QueuedAction.sessionTarget + session, title: "Open items to a task")
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
        try await submit(command, args, target: QueuedAction.sessionTarget + session, title: command == OpenCommand.no ? "Close open items" : "Open items to the backlog")
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
        try await submit("agents.mode", ["session": .string(session), "mode": .string(mode)], target: QueuedAction.sessionTarget + session, title: "Change the mode")
        await refreshSession(session)
    }

    public func setConfig(_ session: String, id: String, value: String) async throws {
        try await submit("agents.config", ["session": .string(session), "id": .string(id), "value": .string(value)], target: QueuedAction.sessionTarget + session, title: "Change a setting")
        await refreshSession(session)
    }

    public func setNotify(_ session: String, policy: NotificationPolicy) async throws {
        try await submit("agents.notify", ["session": .string(session), "policy": .string(policy.rawValue)], target: QueuedAction.sessionTarget + session, title: "Change notifications")
        await refreshSession(session)
    }

    public func close(_ session: String) async throws {
        try await submit("agents.close", ["session": .string(session)], target: QueuedAction.sessionTarget + session, title: "Close the session")
        await refreshSession(session)
    }

    public func reviewed(_ session: String) async throws {
        try await submit(Self.reviewedCommand, ["session": .string(session)], target: QueuedAction.sessionTarget + session, title: "Mark reviewed")
        await refreshSession(session)
    }

    public func recover(_ session: String, with action: FailureAction) async throws -> String? {
        guard let command = action.command else { return nil }
        let result = try await submit(command, (action.args ?? [:]).mapValues(JSONValue.string), target: QueuedAction.sessionTarget + session, title: action.label)
        await refreshSession(session)
        return result?["session"]?.stringValue.flatMap { $0 == session ? nil : $0 }
    }

    public func create(_ text: String, attachments: [ComposerAttachment] = []) async throws -> String? {
        if !attachments.isEmpty, queues() { throw OfflineRefused("Sending attachments") }
        let prompt = attachments.isEmpty ? text : ""
        let result = try await submit(AgentCommand.new, prompt.isEmpty ? [:] : ["prompt": .string(prompt)], target: "new/" + UUID().uuidString, title: prompt.isEmpty ? "New session" : "New session " + Self.quoted(prompt), text: prompt.nilIfEmpty, keeps: attachments.isEmpty)
        guard let id = result?["session"]?.stringValue, !attachments.isEmpty else {
            if let id = result?["session"]?.stringValue, !prompt.isEmpty {
                echo(prompt, images: [], in: id, sent: true)
            }
            await refresh()
            return result?["session"]?.stringValue
        }
        try await send(text, attachments: attachments, to: id)
        return id
    }

    @discardableResult
    private func echo(_ text: String, images: [Data], files: [String] = [], in session: String, sent: Bool = false, known: Set<String>? = nil) -> String {
        let entry = TranscriptEntry(outgoing: text, images: images, files: files)
        let known = known ?? Set((transcripts[session]?.entries ?? []).map(\.id))
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
