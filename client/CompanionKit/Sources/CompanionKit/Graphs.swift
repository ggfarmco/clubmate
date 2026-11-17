import Foundation

import ClubmateProtocol

public enum GraphCommand {
    public static let show = "agents.graph-show"
    public static let approve = "agents.graph-approve"
    public static let `continue` = "agents.graph-continue"
    public static let stop = "agents.graph-stop"
    public static let rerun = "agents.graph-rerun"

    private static let before = [show: "graphs.show", approve: "graphs.approve", `continue`: "graphs.continue", stop: "graphs.stop"]

    public static func id(_ command: String, supports: (String) -> Bool) -> String {
        supports(command) ? command : before[command] ?? command
    }
}

public struct GraphRun: Decodable, Sendable, Equatable, Identifiable {
    public static let idPrefix = "gr_"
    public static let stepRequest = "step-"
    public static let capRequest = "cap-"

    public struct Node: Decodable, Sendable, Equatable, Identifiable {
        public let id: String
        public let role: String
        public let step: Bool?
        public let session: String
        public let title: String?
        public let status: String
        public let command: String?
        public let outcome: String?
        public let durationMs: Int?
        public let startedAt: Date?
        public let last: StepMove?
        public let pending: StepPending?

        public var isStep: Bool { step == true }
    }

    public struct StepMove: Decodable, Sendable, Equatable {
        public let status: String
        public let outcome: String?
        public let reason: String?
        public let exitCode: Int?
        public let dir: String?
        public let command: String?
        public let waitingAt: Date?
        public let startedAt: Date?
        public let endedAt: Date?
        public let durationMs: Int?
    }

    public struct StepPending: Decodable, Sendable, Equatable {
        public let command: String
        public let dir: String
        public let main: String?
        public let rule: String?
    }

    public struct Edge: Decodable, Sendable, Equatable {
        public let from: String
        public let to: String
        public let on: String
    }

    public struct Held: Decodable, Sendable, Equatable {
        public let edge: Int
        public let at: Date?
    }

    public let id: String
    public let graph: String
    public let title: String
    public let number: Int?
    public let status: String
    public let reason: String?
    public let input: String?
    public let turns: Int
    public let maxTurns: Int
    public let startedAt: Date?
    public let stoppedAt: Date?
    public let edges: [Edge]
    public let held: [Held]?
    public let nodes: [Node]

    public static func isRun(_ id: String) -> Bool {
        id.hasPrefix(idPrefix)
    }

    public var running: Bool {
        status == "running"
    }

    public var displayTitle: String {
        Self.numbered(title, number)
    }

    public static func numbered(_ title: String, _ number: Int?) -> String {
        guard let number, number > 0 else { return title }
        return "\(title) · #\(number)"
    }

    public var inputLine: String {
        let turn = maxTurns > 0 ? "turn \(turns) of \(maxTurns)" : nil
        return [input.flatMap { $0.isEmpty ? nil : $0 }, turn].compactMap { $0 }.joined(separator: " · ")
    }

    public func headerState(needsYou: Bool) -> String {
        if needsYou && running { return SessionState.waitingForYou.label }
        if running { return SessionState.running.label }
        guard let reason, !reason.isEmpty else { return SessionState.stopped.label }
        return "Stopped · \(reason)"
    }

    public func lines(now: Date) -> [RunNodeLine] {
        nodes.map { node in
            guard node.isStep else {
                let time: String? = node.status == "running" ? node.startedAt.map { ShortAge.since($0, now: now) } : node.durationMs.flatMap { $0 > 0 ? Self.duration($0) : nil }
                return RunNodeLine(id: node.id, role: node.role, step: false, detail: "agent", word: node.outcome ?? node.status, tone: node.outcome == nil ? Self.agentTone(node.status) : .success, time: time)
            }
            let last = node.last
            let time: String? = switch node.status {
            case "waiting": last?.waitingAt.map { ShortAge.since($0, now: now) }
            case "running": last?.startedAt.map { ShortAge.since($0, now: now) }
            default: last?.durationMs.map(Self.duration)
            }
            let command = last?.command ?? node.pending?.command ?? node.command
            return RunNodeLine(id: node.id, role: node.role, step: true, detail: command ?? "step", word: node.status, tone: Self.stepTone(node.status), time: time)
        }
    }

    public func request(_ entry: AgentSession?) -> RunRequest? {
        guard running, let entry, entry.needsYou, let pending = entry.pending.first else { return nil }
        if pending.hasPrefix(Self.stepRequest) {
            let id = String(pending.dropFirst(Self.stepRequest.count))
            guard let node = nodes.first(where: { $0.id == id }) else { return nil }
            let command = node.pending?.command ?? entry.pendingSummary?.text ?? ""
            return .step(node: id, role: node.role, command: command, place: Self.place(node.pending), rule: node.pending?.rule)
        }
        guard pending.hasPrefix(Self.capRequest), let edge = Int(pending.dropFirst(Self.capRequest.count)) else { return nil }
        return .cap(edge: edge, text: entry.pendingSummary?.text ?? SessionState.waitingForYou.label)
    }

    public static func duration(_ ms: Int) -> String {
        let seconds = max(0, ms / 1000)
        let (h, m, s) = (seconds / 3_600, seconds / 60 % 60, seconds % 60)
        switch seconds {
        case ..<60: return "\(s) s"
        case ..<600: return s > 0 ? "\(m) min \(s) s" : "\(m) min"
        case ..<3_600: return "\(m) min"
        default: return m > 0 ? "\(h) h \(m) min" : "\(h) h"
        }
    }

    private static func place(_ pending: StepPending?) -> String {
        guard let pending else { return "not in the allow list" }
        let dir = pending.dir.replacing(/^\/Users\/[^\/]+/, with: "~")
        let main = pending.main == pending.dir ? " · the main checkout" : ""
        return "in \(dir)\(main) · not in the allow list"
    }

    private static func agentTone(_ status: String) -> RunNodeLine.Tone {
        switch status {
        case "running": .accent
        case "waiting": .warning
        case "done": .success
        case "error": .danger
        default: .secondary
        }
    }

    private static func stepTone(_ status: String) -> RunNodeLine.Tone {
        switch status {
        case "running": .accent
        case "waiting": .warning
        case "pass", "done": .success
        case "error": .danger
        default: .secondary
        }
    }
}

public struct RunNodeLine: Sendable, Equatable, Identifiable {
    public enum Tone: Sendable, Equatable {
        case success
        case warning
        case danger
        case accent
        case secondary
    }

    public let id: String
    public let role: String
    public let step: Bool
    public let detail: String
    public let word: String
    public let tone: Tone
    public let time: String?
}

public enum RunRequest: Sendable, Equatable {
    case step(node: String, role: String, command: String, place: String, rule: String?)
    case cap(edge: Int, text: String)

    public var id: String {
        switch self {
        case .step(let node, _, _, _, _): GraphRun.stepRequest + node
        case .cap(let edge, _): GraphRun.capRequest + String(edge)
        }
    }
}

extension AgentSession {
    public static let runKind = "run"

    public var isRun: Bool {
        kind == Self.runKind
    }
}

public struct AgentRunRow: Codable, Sendable, Equatable, Identifiable {
    public let id: String
    public let graph: String
    public let title: String
    public let number: Int?
    public let worktree: String?
    public let state: String
    public let detail: String
    public let needsYou: Bool?
    public let request: String?
    public let pointer: String?
    public let sessions: [String]
    public let home: String
    public let group: String?
    public let status: String?
}

public struct AgentDockRow: Codable, Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let icon: String
    public let detail: String
    public let phoneDetail: String
    public let attention: Bool?
}

public struct RunEndRow: Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let icon: String
    public let detail: String
    public let section: String
}

public enum RunRows {
    private static let sections = ["gates": "agents.gates", "graphs": "agents.graphs"]

    public static func sessions(_ rows: [JSONValue], runs: [AgentRunRow]?) -> [AgentSession] {
        guard let runs else { return rows.compactMap { try? $0.decode(AgentSession.self) } }
        let ids = Set(runs.map(\.id))
        let listed = Set(rows.compactMap { $0["id"]?.stringValue })
        var out: [AgentSession] = []
        var workspace = ""
        var entries: [String: AgentSession] = [:]
        for row in rows {
            guard case .object(var fields) = row else { continue }
            if fields["kind"]?.stringValue == "run" {
                if let id = fields["id"]?.stringValue, let entry = try? row.decode(AgentSession.self) { entries[id] = entry }
                continue
            }
            workspace = fields["workspace"]?.stringValue ?? workspace
            if let run = fields["run"]?["id"]?.stringValue, ids.contains(run), fields["parentSession"]?.stringValue.map(listed.contains) != true {
                fields["parentSession"] = .string(run)
            }
            if let session = try? JSONValue.object(fields).decode(AgentSession.self) { out.append(session) }
        }
        for run in runs {
            if let entry = entries[run.id] {
                out.append(entry)
            } else if let session = try? session(run, workspace: workspace) {
                out.append(session)
            }
        }
        out += entries.values.filter { !ids.contains($0.id) && $0.rowUnder != nil }.sorted { $0.id < $1.id }
        return out
    }

    public static func endRows(_ dock: [AgentDockRow]) -> [RunEndRow] {
        dock.compactMap { row in sections[row.id].map { RunEndRow(id: row.id, label: row.label, icon: row.icon, detail: row.phoneDetail, section: $0) } }
    }

    private static func session(_ run: AgentRunRow, workspace: String) throws -> AgentSession {
        var fields: [String: JSONValue] = [
            "id": .string(run.id), "kind": .string("run"), "workspace": .string(workspace), "title": .string(run.title),
            "status": .string(run.status ?? ""), "group": .string(run.group ?? ""), "needsYou": .bool(run.needsYou == true), "rowDetail": .string(run.detail),
            "run": .object(["id": .string(run.id), "title": .string(run.title), "number": run.number.map { .number(Double($0)) } ?? .null]),
        ]
        if let worktree = run.worktree { fields["worktree"] = .string(worktree) }
        if let request = run.request { fields["pending"] = .array([.string(request)]) }
        return try JSONValue.object(fields).decode(AgentSession.self)
    }
}
