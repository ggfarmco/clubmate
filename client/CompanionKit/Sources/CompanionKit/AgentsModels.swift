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
        case .needsInput: "Needs you"
        case .running: "Running"
        case .idle: "Done"
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

    public var displayStatus: AgentSessionStatus {
        childStatus ?? status
    }

    public var statusLabel: String {
        switch childStatus {
        case .running: "Waiting for child sessions"
        case .waiting: "Child session waiting"
        default: status.label
        }
    }

    public var isTurnActive: Bool {
        status == .running || status == .waiting
    }

    public var group: SessionGroup {
        if isClosed { return .closed }
        if status == .waiting || !pending.isEmpty || asks || status == .error { return .needsInput }
        if status == .running { return .running }
        return .idle
    }

    public static func grouped(_ sessions: [AgentSession]) -> [(group: SessionGroup, sessions: [SessionNode])] {
        let ids = Set(sessions.map(\.id))
        var kids: [String: [AgentSession]] = [:]
        var roots: [AgentSession] = []
        for session in sessions {
            if let parent = session.parentSession, parent != session.id, ids.contains(parent) {
                kids[parent, default: []].append(session)
            } else {
                roots.append(session)
            }
        }
        var seen: Set<String> = []
        var trees = roots.map { SessionNode.build($0, kids: kids, seen: &seen) }
        for session in sessions where !seen.contains(session.id) {
            trees.append(SessionNode.build(session, kids: kids, seen: &seen))
        }
        let buckets = Dictionary(grouping: trees, by: \.group)
        return SessionGroup.allCases.compactMap { group in
            guard let list = buckets[group], !list.isEmpty else { return nil }
            return (group, list.sorted(by: SessionNode.newerFirst))
        }
    }
}

public struct SessionNode: Sendable, Equatable, Identifiable {
    public let session: AgentSession
    public let children: [SessionNode]
    public let group: SessionGroup
    public let updatedAt: Date

    public var id: String { session.id }

    public var descendants: Int {
        children.reduce(children.count) { $0 + $1.descendants }
    }

    static func build(_ session: AgentSession, kids: [String: [AgentSession]], seen: inout Set<String>) -> SessionNode {
        seen.insert(session.id)
        var children: [SessionNode] = []
        for kid in kids[session.id] ?? [] where !seen.contains(kid.id) {
            children.append(build(kid, kids: kids, seen: &seen))
        }
        let groups = Set(children.map(\.group).filter { $0 == .needsInput || $0 == .running } + [session.group])
        return SessionNode(
            session: session,
            children: children.sorted(by: newerFirst),
            group: SessionGroup.allCases.first(where: groups.contains) ?? session.group,
            updatedAt: children.map(\.updatedAt).reduce(session.updatedAt, max)
        )
    }

    static func newerFirst(_ a: SessionNode, _ b: SessionNode) -> Bool {
        a.updatedAt != b.updatedAt ? a.updatedAt > b.updatedAt : a.id < b.id
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

public struct AgentConfigOption: Decodable, Sendable, Equatable, Identifiable {
    public struct Choice: Decodable, Sendable, Equatable, Hashable {
        public let value: String
        public let name: String
        let options: [Choice]

        enum CodingKeys: String, CodingKey {
            case value, name, options
        }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            value = try c.decodeIfPresent(String.self, forKey: .value) ?? ""
            name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
            options = try c.decodeIfPresent([Choice].self, forKey: .options) ?? []
        }

        public var label: String {
            name.isEmpty ? value : name
        }
    }

    public let id: String
    public let name: String
    public let category: String
    public let type: String
    public let currentValue: String
    public let choices: [Choice]

    enum CodingKeys: String, CodingKey {
        case id, name, category, type, currentValue, options
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        category = try c.decodeIfPresent(String.self, forKey: .category) ?? ""
        type = try c.decodeIfPresent(String.self, forKey: .type) ?? ""
        if let text = try? c.decodeIfPresent(String.self, forKey: .currentValue) {
            currentValue = text
        } else if let on = try? c.decodeIfPresent(Bool.self, forKey: .currentValue) {
            currentValue = String(on)
        } else {
            currentValue = ""
        }
        choices = (try c.decodeIfPresent([Choice].self, forKey: .options) ?? []).flatMap { $0.options.isEmpty ? [$0] : $0.options }
    }

    public var pickable: Bool {
        (type == "select" || isToggle) && category != "mode"
    }

    public var isToggle: Bool {
        type == "boolean"
    }
}

public struct AgentUsage: Decodable, Sendable, Equatable {
    public struct Cost: Decodable, Sendable, Equatable {
        public let amount: Double
        public let currency: String
    }

    public let used: Int
    public let size: Int
    public let cost: Cost?

    public var percent: Int {
        size > 0 ? Int((Double(used) * 100 / Double(size)).rounded()) : 0
    }

    public var nearlyFull: Bool {
        percent >= 80
    }

    public var contextLabel: String {
        "\(percent)% context"
    }

    public var tokensLabel: String {
        "\(Self.tokens(used)) of \(Self.tokens(size)) tokens"
    }

    public var costLabel: String? {
        guard let cost, cost.amount > 0 else { return nil }
        let amount = String(format: "%.2f", cost.amount)
        return cost.currency.isEmpty || cost.currency == "USD" ? "$" + amount : amount + " " + cost.currency
    }

    static func tokens(_ n: Int) -> String {
        n >= 1000 ? "\(Int((Double(n) / 1000).rounded()))k" : String(n)
    }
}

public struct AgentTranscript: Decodable, Sendable, Equatable {
    public let session: AgentSession
    public let modes: [AgentMode]
    public let configOptions: [AgentConfigOption]
    public let advisor: String
    public let usage: AgentUsage?
    public let turns: [AgentTurn]
    public let entries: [TranscriptEntry]

    enum CodingKeys: String, CodingKey {
        case session, modes, configOptions, advisor, usage, turns, entries
    }

    init(session: AgentSession, modes: [AgentMode], configOptions: [AgentConfigOption], advisor: String, usage: AgentUsage?, turns: [AgentTurn], entries: [TranscriptEntry]) {
        self.session = session
        self.modes = modes
        self.configOptions = configOptions
        self.advisor = advisor
        self.usage = usage
        self.turns = turns
        self.entries = entries
    }

    public var lastTurn: Int {
        entries.map(\.turn).max() ?? 0
    }

    public func merging(_ newer: AgentTranscript, fromTurn: Int) -> AgentTranscript {
        AgentTranscript(
            session: newer.session, modes: newer.modes, configOptions: newer.configOptions, advisor: newer.advisor, usage: newer.usage, turns: newer.turns,
            entries: entries.filter { $0.turn < fromTurn } + newer.entries
        )
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        session = try c.decode(AgentSession.self, forKey: .session)
        modes = (try? c.decodeIfPresent([AgentMode].self, forKey: .modes)) ?? []
        configOptions = (try? c.decodeIfPresent([AgentConfigOption].self, forKey: .configOptions)) ?? []
        advisor = (try? c.decodeIfPresent(String.self, forKey: .advisor)) ?? ""
        usage = try? c.decodeIfPresent(AgentUsage.self, forKey: .usage)
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
        case elicitation(Elicitation)
        case notice(Notice)
        case compaction(Compaction)
        case other(String)
    }

    public let id: String
    public let entryID: String
    public let turn: Int
    public let content: Content

    enum CodingKeys: String, CodingKey {
        case kind, id, turn, text, content, tool, plan, toolCall, options, outcome, notice, from, compaction, elicitation, response, completed
    }

    static let outgoingPrefix = "outgoing/"

    init(outgoing text: String, images: [Data]) {
        entryID = UUID().uuidString
        id = Self.outgoingPrefix + entryID
        turn = 0
        content = .user(text: text, fromAgent: false, images: images)
    }

    public var isOutgoing: Bool {
        id.hasPrefix(Self.outgoingPrefix)
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
            let images = blocks.compactMap { $0["type"]?.stringValue == "image" ? Data(base64Encoded: $0["data"]?.stringValue ?? "") ?? Data() : nil }
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
                options: PermissionOption.ordered((try? c.decodeIfPresent([PermissionOption].self, forKey: .options)) ?? []),
                outcome: try? c.decodeIfPresent(PermissionOutcome.self, forKey: .outcome)
            ))
        case .elicitation:
            content = .elicitation(Elicitation(
                id: entryID,
                request: (try? c.decodeIfPresent(Elicitation.Request.self, forKey: .elicitation)) ?? Elicitation.Request(),
                response: try? c.decodeIfPresent(Elicitation.Response.self, forKey: .response),
                completed: (try? c.decodeIfPresent(Bool.self, forKey: .completed)) ?? false
            ))
        case .notice:
            let notice = (try? c.decodeIfPresent(JSONValue.self, forKey: .notice)) ?? .null
            content = .notice(Notice(title: notice["title"]?.stringValue ?? "", description: notice["description"]?.stringValue ?? ""))
        case .compaction:
            let state = (try? c.decodeIfPresent(JSONValue.self, forKey: .compaction)) ?? .null
            content = .compaction(Compaction(status: state["status"]?.stringValue ?? "", summary: text, error: state["error"]?.stringValue ?? ""))
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
    public let intent: String?

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
        intent = fields["rawInput"]?["description"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
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

    static func ordered(_ options: [PermissionOption]) -> [PermissionOption] {
        let rank: [PermissionKind] = [.allowOnce, .allowAlways, .rejectOnce, .rejectAlways]
        return options.enumerated().sorted { a, b in
            let ra = rank.firstIndex(of: a.element.kind) ?? rank.count, rb = rank.firstIndex(of: b.element.kind) ?? rank.count
            return ra == rb ? a.offset < b.offset : ra < rb
        }.map(\.element)
    }
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

public struct Elicitation: Sendable, Equatable {
    public struct Request: Decodable, Sendable, Equatable {
        public var mode = ""
        public var message = ""
        public var url: String?
        public var fields: [Field] = []

        enum CodingKeys: String, CodingKey {
            case mode, message, url, fields
        }

        init() {}

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            mode = try c.decodeIfPresent(String.self, forKey: .mode) ?? ""
            message = try c.decodeIfPresent(String.self, forKey: .message) ?? ""
            url = try c.decodeIfPresent(String.self, forKey: .url)
            fields = (try? c.decodeIfPresent([Field].self, forKey: .fields)) ?? []
        }
    }

    public struct Field: Decodable, Sendable, Equatable, Identifiable {
        public let key: String
        public let type: String
        public let title: String
        public let description: String
        public let required: Bool
        public let options: [Option]

        public var id: String { key }
        public var label: String { title.isEmpty ? key : title }

        enum CodingKeys: String, CodingKey {
            case key, type, title, description, required, options
        }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            key = try c.decode(String.self, forKey: .key)
            type = try c.decodeIfPresent(String.self, forKey: .type) ?? "text"
            title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
            description = try c.decodeIfPresent(String.self, forKey: .description) ?? ""
            required = try c.decodeIfPresent(Bool.self, forKey: .required) ?? false
            options = try c.decodeIfPresent([Option].self, forKey: .options) ?? []
        }
    }

    public struct Option: Decodable, Sendable, Equatable, Identifiable {
        public let value: String
        public let label: String
        public let description: String?

        public var id: String { value }
    }

    public struct Response: Decodable, Sendable, Equatable {
        public let action: String
        public let content: [String: JSONValue]?
    }

    public struct Answer: Sendable, Equatable {
        public let key: String
        public let value: String

        public init(key: String, value: String) {
            self.key = key
            self.value = value
        }
    }

    public let id: String
    public let request: Request
    public let response: Response?
    public let completed: Bool

    public var isURL: Bool { request.mode == "url" }

    public var singleChoice: Field? {
        let choices = request.fields.filter { $0.type == "choice" }
        guard choices.count == 1, !request.fields.contains(where: { $0.type == "multi" || $0.type == "boolean" }) else { return nil }
        return choices[0]
    }

    public var answer: String? {
        guard let response else { return nil }
        switch response.action {
        case "decline": return isURL ? "Declined" : "Skipped"
        case "accept" where isURL: return completed ? "Done" : "Opened in the browser"
        case "accept":
            let parts = request.fields.compactMap { field -> String? in
                guard let value = response.content?[field.key] else { return nil }
                return Self.text(field, value)
            }
            return parts.isEmpty ? "Answered" : parts.joined(separator: " · ")
        default: return "Cancelled"
        }
    }

    private static func text(_ field: Field, _ value: JSONValue) -> String? {
        let label: (String) -> String = { raw in field.options.first { $0.value == raw }?.label ?? raw }
        if let list = value.arrayValue {
            let labels = list.compactMap(\.stringValue).map(label)
            return labels.isEmpty ? nil : labels.joined(separator: ", ")
        }
        if let string = value.stringValue {
            return string.isEmpty ? nil : label(string)
        }
        if case .bool(let on) = value {
            return on ? field.label : nil
        }
        if case .number(let number) = value {
            return "\(field.label): \(number.formatted())"
        }
        return nil
    }
}

public struct Compaction: Sendable, Equatable {
    public let status: String
    public let summary: String
    public let error: String

    public var title: String {
        switch status {
        case "in_progress": "Compacting context…"
        case "failed": "Context compaction failed"
        case "cancelled": "Context compaction cancelled"
        default: "Context compacted"
        }
    }
}
