import Foundation

public enum BacklogStatus: String, Sendable, Codable, CaseIterable, Identifiable {
    case inProgress = "in_progress"
    case todo
    case idea
    case notNow = "not_now"
    case done
    case other

    public var id: String { rawValue }

    public static let groupOrder: [BacklogStatus] = [.inProgress, .todo, .idea, .notNow, .done]
    public static let moveTargets: [BacklogStatus] = [.todo, .idea, .notNow]
    public static let addable: [BacklogStatus] = [.todo, .idea, .notNow]
    public static let folded: Set<BacklogStatus> = [.notNow, .done]

    public init(from decoder: any Decoder) throws {
        self = BacklogStatus(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .other
    }

    public var label: String {
        switch self {
        case .inProgress: "In progress"
        case .todo: "Todo"
        case .idea: "Idea"
        case .notNow: "Not now"
        case .done: "Done"
        case .other: "Other"
        }
    }

    public var symbol: String {
        switch self {
        case .inProgress: "play.circle"
        case .todo: "circle"
        case .idea: "lightbulb"
        case .notNow: "moon.zzz"
        case .done: "checkmark.circle"
        case .other: "questionmark.circle"
        }
    }
}

public enum BacklogPriority: String, Sendable, Codable, CaseIterable, Identifiable {
    case high
    case normal
    case low

    public var id: String { rawValue }

    public init(from decoder: any Decoder) throws {
        self = BacklogPriority(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .normal
    }

    public var label: String {
        switch self {
        case .high: "High"
        case .normal: "Normal"
        case .low: "Low"
        }
    }

    public var glyph: String? {
        switch self {
        case .high: "!!"
        case .normal: nil
        case .low: "↓"
        }
    }
}

public struct BacklogLink: Sendable, Equatable, Identifiable, Decodable {
    public let kind: String
    public let task: Int
    public let title: String?
    public let status: BacklogStatus?

    public var id: Int { task }

    public init(kind: String, task: Int, title: String? = nil, status: BacklogStatus? = nil) {
        self.kind = kind
        self.task = task
        self.title = title
        self.status = status
    }

    public var words: String {
        switch kind {
        case "blocked_by": "blocked by"
        case "blocks": "blocks"
        default: "relates to"
        }
    }

    public var detail: String {
        ([words] + [status.map { $0.label.lowercased() }].compactMap { $0 }).joined(separator: " · ")
    }
}

public struct BacklogAttachment: Sendable, Equatable, Decodable {
    public enum Opens: String, Sendable, Decodable {
        case url, doc, editor, system, other

        public init(from decoder: any Decoder) throws {
            self = Opens(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .other
        }
    }

    public let kind: String
    public let ref: String
    public let title: String?
    public let path: String?
    public let opens: Opens?

    public init(kind: String, ref: String, title: String? = nil, path: String? = nil, opens: Opens? = nil) {
        self.kind = kind
        self.ref = ref
        self.title = title
        self.path = path
        self.opens = opens
    }

    public var label: String {
        title.flatMap { $0.isEmpty ? nil : $0 } ?? ref
    }

    public var symbol: String {
        switch kind {
        case "image": "photo"
        case "artifact": "square.on.square"
        case "url": "link"
        default: "doc"
        }
    }

    public var kindWord: String {
        switch kind {
        case "image": "image"
        case "artifact": "artifact"
        case "url": "link"
        default: "worktree"
        }
    }
}

public struct BacklogOption: Sendable, Equatable, Hashable, Decodable {
    public let value: String
    public let label: String

    public init(value: String, label: String) {
        self.value = value
        self.label = label
    }
}

public struct BacklogStart: Sendable, Equatable, Decodable {
    public let agents: [BacklogOption]
    public let modes: [BacklogOption]
    public let `where`: [BacklogOption]
    public let prompt: String
    public let owns: String?

    public init(agents: [BacklogOption] = [], modes: [BacklogOption] = [], where: [BacklogOption] = [], prompt: String = "", owns: String? = nil) {
        self.agents = agents
        self.modes = modes
        self.where = `where`
        self.prompt = prompt
        self.owns = owns
    }

    private enum CodingKeys: String, CodingKey {
        case agents, modes, `where`, prompt, owns
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            agents: try c.decodeIfPresent([BacklogOption].self, forKey: .agents) ?? [],
            modes: try c.decodeIfPresent([BacklogOption].self, forKey: .modes) ?? [],
            where: try c.decodeIfPresent([BacklogOption].self, forKey: .where) ?? [],
            prompt: try c.decodeIfPresent(String.self, forKey: .prompt) ?? "",
            owns: try c.decodeIfPresent(String.self, forKey: .owns)
        )
    }
}

public struct BacklogOwnerEvent: Sendable, Equatable, Decodable {
    public let who: String
    public let what: String
    public let at: Date?
    public let tone: String?
    public let session: String?

    public init(who: String, what: String, at: Date? = nil, tone: String? = nil, session: String? = nil) {
        self.who = who
        self.what = what
        self.at = at
        self.tone = tone
        self.session = session
    }

    private enum CodingKeys: String, CodingKey {
        case who, what, at, tone, session
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let session = try c.decodeIfPresent(String.self, forKey: .session) ?? ""
        self.init(
            who: try c.decodeIfPresent(String.self, forKey: .who) ?? "",
            what: try c.decodeIfPresent(String.self, forKey: .what) ?? "",
            at: (try? c.decodeIfPresent(String.self, forKey: .at)).flatMap { $0.flatMap(Date.parseRFC3339) },
            tone: try c.decodeIfPresent(String.self, forKey: .tone),
            session: session.isEmpty ? nil : session
        )
    }

    public var danger: Bool {
        tone == "danger"
    }
}

public struct BacklogPrimary: Sendable, Equatable, Decodable {
    public enum Action: String, Sendable, Decodable {
        case start, reveal, takeback, other

        public init(from decoder: any Decoder) throws {
            self = Action(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .other
        }
    }

    public let action: Action
    public let label: String
    public let summary: String?
    public let session: String?
    public let run: String?
    public let start: BacklogStart?

    public init(action: Action, label: String, summary: String? = nil, session: String? = nil, run: String? = nil, start: BacklogStart? = nil) {
        self.action = action
        self.label = label
        self.summary = summary
        self.session = session
        self.run = run
        self.start = start
    }
}

public struct BacklogTask: Sendable, Equatable, Identifiable, Decodable {
    public let id: Int
    public let title: String
    public let description: String
    public let descriptionHtml: String?
    public let status: BacklogStatus
    public let priority: BacklogPriority
    public let claimedBy: String?
    public let claimedAt: Date?
    public let note: String
    public let createdBy: String
    public let createdAt: Date?
    public let updatedAt: Date?
    public let links: [BacklogLink]
    public let attachments: [BacklogAttachment]
    public let primary: BacklogPrimary?
    public let ownerState: String?
    public let lastOwner: BacklogOwnerEvent?
    public let history: [BacklogOwnerEvent]

    public init(id: Int, title: String, description: String = "", descriptionHtml: String? = nil, status: BacklogStatus, priority: BacklogPriority = .normal, claimedBy: String? = nil, claimedAt: Date? = nil, note: String = "", createdBy: String = "user", createdAt: Date? = nil, updatedAt: Date? = nil, links: [BacklogLink] = [], attachments: [BacklogAttachment] = [], primary: BacklogPrimary? = nil, ownerState: String? = nil, lastOwner: BacklogOwnerEvent? = nil, history: [BacklogOwnerEvent] = []) {
        self.id = id
        self.title = title
        self.description = description
        self.descriptionHtml = descriptionHtml
        self.status = status
        self.priority = priority
        self.claimedBy = claimedBy
        self.claimedAt = claimedAt
        self.note = note
        self.createdBy = createdBy
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.links = links
        self.attachments = attachments
        self.primary = primary
        self.ownerState = ownerState
        self.lastOwner = lastOwner
        self.history = history
    }

    private enum CodingKeys: String, CodingKey {
        case id, title, description, descriptionHtml, status, priority, claimedBy, claimedAt, note, createdBy, createdAt, updatedAt, links, attachments, primary, ownerState, lastOwner, history
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let text = { (key: CodingKeys) in try c.decodeIfPresent(String.self, forKey: key) ?? "" }
        let date = { (key: CodingKeys) in (try? c.decodeIfPresent(String.self, forKey: key)).flatMap { $0.flatMap(Date.parseRFC3339) } }
        let owner = try text(.claimedBy)
        self.init(
            id: try c.decode(Int.self, forKey: .id),
            title: try text(.title),
            description: try text(.description),
            descriptionHtml: try c.decodeIfPresent(String.self, forKey: .descriptionHtml),
            status: try c.decodeIfPresent(BacklogStatus.self, forKey: .status) ?? .other,
            priority: try c.decodeIfPresent(BacklogPriority.self, forKey: .priority) ?? .normal,
            claimedBy: owner.isEmpty ? nil : owner,
            claimedAt: date(.claimedAt),
            note: try text(.note),
            createdBy: try text(.createdBy),
            createdAt: date(.createdAt),
            updatedAt: date(.updatedAt),
            links: try c.decodeIfPresent([BacklogLink].self, forKey: .links) ?? [],
            attachments: try c.decodeIfPresent([BacklogAttachment].self, forKey: .attachments) ?? [],
            primary: try c.decodeIfPresent(BacklogPrimary.self, forKey: .primary),
            ownerState: try c.decodeIfPresent(String.self, forKey: .ownerState).flatMap { $0.isEmpty ? nil : $0 },
            lastOwner: try c.decodeIfPresent(BacklogOwnerEvent.self, forKey: .lastOwner),
            history: try c.decodeIfPresent([BacklogOwnerEvent].self, forKey: .history) ?? []
        )
    }

    public var number: String {
        "#\(id)"
    }

    public var noteLine: String? {
        note.split(whereSeparator: \.isNewline).first.map(String.init)
    }

    public var claimedByUser: Bool {
        claimedBy == "user"
    }

    public static func grouped(_ tasks: [BacklogTask]) -> [(status: BacklogStatus, tasks: [BacklogTask])] {
        let buckets = Dictionary(grouping: tasks, by: \.status)
        return (BacklogStatus.groupOrder + [.other]).compactMap { status in
            guard let list = buckets[status], !list.isEmpty else { return nil }
            return (status, list)
        }
    }
}

public struct BacklogDraft: Sendable, Equatable {
    public var title: String
    public var description: String
    public var priority: BacklogPriority
    public var status: BacklogStatus

    public init(title: String = "", description: String = "", priority: BacklogPriority = .normal, status: BacklogStatus = .todo) {
        self.title = title
        self.description = description
        self.priority = priority
        self.status = status
    }

    public init(_ task: BacklogTask) {
        self.init(title: task.title, description: task.description, priority: task.priority, status: task.status)
    }

    public var trimmedTitle: String {
        title.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    public var isValid: Bool {
        !trimmedTitle.isEmpty && !trimmedTitle.contains(where: \.isNewline)
    }

    public func addArgs() -> JSONValue {
        var args: [String: JSONValue] = ["title": .string(trimmedTitle), "priority": .string(priority.rawValue), "status": .string(status.rawValue)]
        if !description.isEmpty { args["description"] = .string(description) }
        return .object(args)
    }

    public func changes(from task: BacklogTask) -> [String: JSONValue] {
        var args: [String: JSONValue] = [:]
        if trimmedTitle != task.title { args["title"] = .string(trimmedTitle) }
        if description != task.description { args["description"] = .string(description) }
        if priority != task.priority { args["priority"] = .string(priority.rawValue) }
        if status != task.status, status != .inProgress, status != .other { args["status"] = .string(status.rawValue) }
        return args
    }
}
