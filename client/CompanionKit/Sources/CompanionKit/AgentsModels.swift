import Foundation

import ClubmateProtocol

public enum SessionGroup: String, Sendable, CaseIterable, Identifiable {
    case needsInput
    case running
    case idle
    case closed

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .needsInput: "Needs Input"
        case .running: "Running"
        case .idle: "Idle"
        case .closed: "Closed"
        }
    }
}

extension AgentSession {
    public var displayTitle: String {
        title.isEmpty ? "New Session" : title
    }

    public var worktreeName: String {
        worktree.split(separator: "/").last.map(String.init) ?? worktree
    }

    public var agentName: String {
        agent == "claude" ? "Claude" : agent.capitalized
    }

    public var isTurnActive: Bool {
        status == .running || status == .waiting
    }

    public var group: SessionGroup {
        if closed || status == .closed { return .closed }
        if status == .waiting || !pending.isEmpty || asks || status == .error { return .needsInput }
        if status == .running { return .running }
        return .idle
    }

    public static func grouped(_ sessions: [AgentSession]) -> [(group: SessionGroup, sessions: [AgentSession])] {
        let buckets = Dictionary(grouping: sessions, by: \.group)
        return SessionGroup.allCases.compactMap { group in
            guard let list = buckets[group], !list.isEmpty else { return nil }
            return (group, list.sorted { $0.updatedAt != $1.updatedAt ? $0.updatedAt > $1.updatedAt : $0.id < $1.id })
        }
    }
}

public struct AgentMode: Decodable, Sendable, Equatable, Identifiable {
    public let id: String
    public let name: String
}

public struct AgentTurn: Decodable, Sendable, Equatable {
    public let turn: Int
    public let stopReason: String?
}

public struct AgentTranscript: Decodable, Sendable, Equatable {
    public let session: AgentSession
    public let modes: [AgentMode]
    public let turns: [AgentTurn]
    public let entries: [TranscriptEntry]

    enum CodingKeys: String, CodingKey {
        case session, modes, turns, entries
    }

    init(session: AgentSession, modes: [AgentMode], turns: [AgentTurn], entries: [TranscriptEntry]) {
        self.session = session
        self.modes = modes
        self.turns = turns
        self.entries = entries
    }

    public var lastTurn: Int {
        entries.map(\.turn).max() ?? 0
    }

    public func merging(_ newer: AgentTranscript, fromTurn: Int) -> AgentTranscript {
        AgentTranscript(session: newer.session, modes: newer.modes, turns: newer.turns, entries: entries.filter { $0.turn < fromTurn } + newer.entries)
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        session = try c.decode(AgentSession.self, forKey: .session)
        modes = (try? c.decodeIfPresent([AgentMode].self, forKey: .modes)) ?? []
        turns = (try? c.decodeIfPresent([AgentTurn].self, forKey: .turns)) ?? []
        entries = (try c.decodeIfPresent([TranscriptEntry].self, forKey: .entries)) ?? []
    }
}

public struct TranscriptEntry: Decodable, Sendable, Equatable, Identifiable {
    public enum Content: Sendable, Equatable {
        case user(text: String, fromAgent: Bool, images: [Data])
        case agent(String)
        case thought(String)
        case tool(ToolCall)
        case plan([PlanItem])
        case permission(PermissionRequest)
        case notice(Notice)
        case other(String)
    }

    public let id: String
    public let entryID: String
    public let turn: Int
    public let content: Content

    enum CodingKeys: String, CodingKey {
        case kind, id, turn, text, content, tool, plan, toolCall, options, outcome, notice, from
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let raw = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        entryID = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
        id = raw + "/" + entryID
        turn = try c.decodeIfPresent(Int.self, forKey: .turn) ?? 0
        let text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        switch TranscriptEntryKind(raw) {
        case .user:
            let blocks = (try? c.decodeIfPresent([JSONValue].self, forKey: .content)) ?? []
            let images = blocks.compactMap { $0["type"]?.stringValue == "image" ? $0["data"]?.stringValue.flatMap { Data(base64Encoded: $0) } : nil }
            content = .user(text: text, fromAgent: !(try c.decodeIfPresent(String.self, forKey: .from) ?? "").isEmpty, images: images)
        case .agent:
            content = .agent(text)
        case .thought:
            content = .thought(text)
        case .tool:
            content = .tool(ToolCall(id: entryID, fields: (try? c.decodeIfPresent(JSONValue.self, forKey: .tool)) ?? .null))
        case .plan:
            let plan = (try? c.decodeIfPresent(JSONValue.self, forKey: .plan)) ?? .null
            content = .plan((plan["entries"]?.arrayValue ?? []).map(PlanItem.init))
        case .permission:
            content = .permission(PermissionRequest(
                id: entryID,
                toolCall: ToolCall(id: entryID, fields: (try? c.decodeIfPresent(JSONValue.self, forKey: .toolCall)) ?? .null),
                options: (try? c.decodeIfPresent([PermissionOption].self, forKey: .options)) ?? [],
                outcome: try? c.decodeIfPresent(PermissionOutcome.self, forKey: .outcome)
            ))
        case .notice:
            let notice = (try? c.decodeIfPresent(JSONValue.self, forKey: .notice)) ?? .null
            content = .notice(Notice(title: notice["title"]?.stringValue ?? "", description: notice["description"]?.stringValue ?? ""))
        case .other:
            content = .other(raw)
        }
    }
}

public struct ToolCall: Sendable, Equatable {
    public struct FileDiff: Sendable, Equatable {
        public let path: String
        public let oldText: String?
        public let newText: String

        public var fileName: String {
            path.split(separator: "/").last.map(String.init) ?? path
        }

        public var lines: [DiffLine] {
            LineDiff.lines(old: oldText ?? "", new: newText)
        }
    }

    public let id: String
    public let title: String
    public let kind: ToolKind
    public let status: ToolStatus
    public let diffs: [FileDiff]
    public let output: String
    public let paths: [String]
    public let command: String?

    public init(id: String, fields: JSONValue) {
        self.id = id
        title = fields["title"]?.stringValue ?? ""
        kind = ToolKind(fields["kind"]?.stringValue)
        status = fields["status"]?.stringValue.flatMap { $0.isEmpty ? nil : ToolStatus($0) } ?? .pending
        let content = fields["content"]?.arrayValue ?? []
        diffs = content.compactMap { item in
            guard item["type"]?.stringValue == "diff", let path = item["path"]?.stringValue else { return nil }
            return FileDiff(path: path, oldText: item["oldText"]?.stringValue, newText: item["newText"]?.stringValue ?? "")
        }
        output = content.compactMap { $0["type"]?.stringValue == "content" ? $0["content"]?["text"]?.stringValue : nil }.joined(separator: "\n")
        paths = (fields["locations"]?.arrayValue ?? []).compactMap { $0["path"]?.stringValue }
        command = fields["rawInput"]?["command"]?.stringValue
    }

    public var displayTitle: String {
        let plain = title.replacingOccurrences(of: "`", with: "")
        if kind.touchesFiles || kind == .read, let path = diffs.first?.path ?? paths.first, plain.contains(path) {
            return plain.replacingOccurrences(of: path, with: path.split(separator: "/").last.map(String.init) ?? path)
        }
        return plain.isEmpty ? kind.rawValue.capitalized : plain
    }

    public var changes: (added: Int, removed: Int) {
        let lines = diffs.flatMap(\.lines)
        return (lines.filter { $0.kind == .added }.count, lines.filter { $0.kind == .removed }.count)
    }
}

public struct PlanItem: Sendable, Equatable {
    public let content: String
    public let status: PlanStatus
    public let priority: String

    init(_ value: JSONValue) {
        content = value["content"]?.stringValue ?? ""
        status = PlanStatus(value["status"]?.stringValue)
        priority = value["priority"]?.stringValue ?? ""
    }
}

public struct PermissionOption: Decodable, Sendable, Equatable, Identifiable {
    public let optionId: String
    public let name: String
    public let kind: PermissionKind

    public var id: String { optionId }
}

public struct PermissionOutcome: Decodable, Sendable, Equatable {
    public let outcome: PermissionOutcomeKind
    public let optionId: String?
}

public struct PermissionRequest: Sendable, Equatable {
    public let id: String
    public let toolCall: ToolCall
    public let options: [PermissionOption]
    public let outcome: PermissionOutcome?

    public var answer: String? {
        guard let outcome else { return nil }
        if outcome.outcome == .cancelled { return "Cancelled" }
        return options.first { $0.optionId == outcome.optionId }?.name ?? outcome.optionId
    }
}

public struct Notice: Sendable, Equatable {
    public let title: String
    public let description: String
}
