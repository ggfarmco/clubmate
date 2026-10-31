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
}

public struct BacklogTask: Sendable, Equatable, Identifiable, Decodable {
    public let id: Int
    public let title: String
    public let description: String
    public let status: BacklogStatus
    public let priority: BacklogPriority
    public let claimedBy: String?
    public let claimedAt: Date?
    public let note: String
    public let createdBy: String
    public let createdAt: Date?
    public let updatedAt: Date?

    public init(id: Int, title: String, description: String = "", status: BacklogStatus, priority: BacklogPriority = .normal, claimedBy: String? = nil, claimedAt: Date? = nil, note: String = "", createdBy: String = "user", createdAt: Date? = nil, updatedAt: Date? = nil) {
        self.id = id
        self.title = title
        self.description = description
        self.status = status
        self.priority = priority
        self.claimedBy = claimedBy
        self.claimedAt = claimedAt
        self.note = note
        self.createdBy = createdBy
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    private enum CodingKeys: String, CodingKey {
        case id, title, description, status, priority, claimedBy, claimedAt, note, createdBy, createdAt, updatedAt
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
            status: try c.decodeIfPresent(BacklogStatus.self, forKey: .status) ?? .other,
            priority: try c.decodeIfPresent(BacklogPriority.self, forKey: .priority) ?? .normal,
            claimedBy: owner.isEmpty ? nil : owner,
            claimedAt: date(.claimedAt),
            note: try text(.note),
            createdBy: try text(.createdBy),
            createdAt: date(.createdAt),
            updatedAt: date(.updatedAt)
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
