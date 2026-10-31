import Foundation
import Observation

@MainActor
@Observable
public final class AgentsStore {
    public let workspace: String
    public private(set) var sessions: [AgentSession] = []
    public private(set) var transcripts: [String: AgentTranscript] = [:]
    public private(set) var loaded = false
    public private(set) var error: String?

    @ObservationIgnored private weak var runner: (any CommandRunner)?
    @ObservationIgnored private var watched: [String: Int] = [:]
    @ObservationIgnored private var inflight: Set<String> = []
    @ObservationIgnored private var again: Set<String> = []

    public init(workspace: String, runner: any CommandRunner) {
        self.workspace = workspace
        self.runner = runner
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
            await loadTranscript(session)
        }
    }

    public func refresh() async {
        await coalesced("sessions") { [weak self] in
            guard let self, let runner else { return }
            do {
                sessions = try await runner.command(AgentCommand.sessions, workspace: workspace, args: nil).decode([AgentSession].self)
                error = nil
            } catch {
                self.error = Self.message(error)
            }
            loaded = true
        }
    }

    public func watch(_ session: String) {
        watched[session, default: 0] += 1
    }

    public func unwatch(_ session: String) {
        guard let count = watched[session] else { return }
        watched[session] = count > 1 ? count - 1 : nil
    }

    public func loadTranscript(_ session: String, incremental: Bool = false) async {
        await coalesced("transcript/" + session) { [weak self] in
            guard let self, let runner else { return }
            do {
                let loaded = incremental ? transcripts[session] : nil
                let fromTurn = loaded?.lastTurn ?? 0
                var args: [String: JSONValue] = ["session": .string(session)]
                if fromTurn > 0 { args["fromTurn"] = .number(Double(fromTurn)) }
                let fetched = try await runner.command("agents.transcript", workspace: workspace, args: .object(args)).decode(AgentTranscript.self)
                transcripts[session] = fromTurn > 0 ? loaded?.merging(fetched, fromTurn: fromTurn) ?? fetched : fetched
            } catch {
                self.error = Self.message(error)
            }
        }
    }

    public func send(_ text: String, images: [Data] = [], to session: String) async throws {
        var args: [String: JSONValue] = ["session": .string(session), "text": .string(text)]
        if !images.isEmpty {
            args["attachments"] = .array(images.map { .object(["kind": .string("image"), "mimeType": .string("image/jpeg"), "data": .string($0.base64EncodedString())]) })
        }
        try await run("agents.prompt", args)
        await refreshSession(session)
    }

    public func cancel(_ session: String) async throws {
        try await run("agents.cancel", ["session": .string(session)])
        await refreshSession(session)
    }

    public func answer(_ session: String, request: String, option: String) async throws {
        try await run("agents.permission", ["session": .string(session), "request": .string(request), "option": .string(option)])
        await refreshSession(session)
    }

    public func close(_ session: String) async throws {
        try await run("agents.close", ["session": .string(session)])
        await refreshSession(session)
    }

    public func create(prompt: String, title: String) async throws -> String? {
        var args: [String: JSONValue] = [:]
        if !prompt.isEmpty { args["prompt"] = .string(prompt) }
        if !title.isEmpty { args["title"] = .string(title) }
        guard let runner else { return nil }
        let result = try await runner.command("agents.new", workspace: workspace, args: .object(args))
        await refresh()
        return result["session"]?.stringValue
    }

    private func run(_ command: String, _ args: [String: JSONValue]) async throws {
        guard let runner else { return }
        _ = try await runner.command(command, workspace: workspace, args: .object(args))
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

    private static func message(_ error: any Error) -> String {
        (error as? RemoteError)?.message ?? error.localizedDescription
    }
}
