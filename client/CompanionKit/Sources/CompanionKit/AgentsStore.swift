import Foundation
import Observation

public enum AnswerError: Error, Equatable, LocalizedError {
    case alreadyAnswered

    static let unknownRequest = "unknown permission request"

    public var errorDescription: String? {
        "Already answered"
    }
}

@MainActor
@Observable
public final class AgentsStore {
    public let workspace: String
    public private(set) var sessions: [AgentSession] = []
    public private(set) var transcripts: [String: AgentTranscript] = [:]
    public private(set) var loaded = false
    public private(set) var error: String?
    public private(set) var transcriptErrors: [String: String] = [:]
    public var expanded: Set<String> = []
    private var outgoing: [String: [Outgoing]] = [:]

    public static let followInterval: Duration = .seconds(2)

    @ObservationIgnored private weak var runner: (any CommandRunner)?
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var watched: [String: Int] = [:]
    @ObservationIgnored private var followers: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var inflight: Set<String> = []
    @ObservationIgnored private var again: Set<String> = []

    public init(workspace: String, runner: any CommandRunner, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.workspace = workspace
        self.runner = runner
        self.sleep = sleep
    }

    public var groups: [(group: SessionGroup, sessions: [SessionNode])] {
        AgentSession.grouped(sessions)
    }

    public var needsInputCount: Int {
        sessions.filter { $0.group == .needsInput }.count
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
        case .event:
            break
        }
    }

    public func refreshAll() async {
        await refresh()
        for session in watched.keys {
            await loadTranscript(session, incremental: true)
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
            sessions = try await runner.command(AgentCommand.sessions, workspace: workspace, args: nil).decode([AgentSession].self)
            error = nil
        } catch {
            self.error = Self.message(error)
        }
        loaded = true
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
                    let fetched = try await Self.transcript(from: runner.command("agents.transcript", workspace: workspace, args: .object(args)))
                    transcripts[session] = fromTurn > 0 ? loaded?.merging(fetched, fromTurn: fromTurn) ?? fetched : fetched
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

    @concurrent
    private nonisolated static func transcript(from value: JSONValue) async throws -> AgentTranscript {
        try value.decode(AgentTranscript.self)
    }

    public func send(_ text: String, attachments: [ComposerAttachment] = [], to session: String) async throws {
        let images = attachments.flatMap(\.images)
        let prompt = ComposerAttachment.promptText(text, attachments)
        var args: [String: JSONValue] = ["session": .string(session), "text": .string(prompt)]
        if !images.isEmpty {
            args["attachments"] = .array(images.map { .object(["kind": .string("image"), "mimeType": .string("image/jpeg"), "data": .string($0.base64EncodedString())]) })
        }
        let echo = self.echo(prompt, images: images, in: session)
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
        sessions = try await runner.command(AgentCommand.sessions, workspace: workspace, args: nil).decode([AgentSession].self)
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
        await refreshSession(session)
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
    private func echo(_ text: String, images: [Data], in session: String, sent: Bool = false) -> String {
        let entry = TranscriptEntry(outgoing: text, images: images)
        let known = Set((transcripts[session]?.entries ?? []).map(\.id))
        outgoing[session, default: []].append(Outgoing(entry: entry, text: text, known: known, sent: sent))
        return entry.id
    }

    private func reconcile(_ session: String) {
        guard let transcript = transcripts[session], let list = outgoing[session] else { return }
        let kept = list.filter { prompt in
            if prompt.sent && ![.running, .waiting].contains(transcript.session.status) { return false }
            return !transcript.entries.contains { entry in
                guard !prompt.known.contains(entry.id), case .user(let text, _, _) = entry.content else { return false }
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
