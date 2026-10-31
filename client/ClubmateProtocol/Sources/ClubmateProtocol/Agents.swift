public protocol FallbackEnum: RawRepresentable, CaseIterable, Codable, Hashable, Sendable where RawValue == String {
    static var fallback: Self { get }
}

extension FallbackEnum {
    public init(_ raw: String?) {
        self = raw.flatMap(Self.init(rawValue:)) ?? .fallback
    }

    public init(from decoder: any Decoder) throws {
        self.init(try? decoder.singleValueContainer().decode(String.self))
    }
}

public enum AgentSessionStatus: String, FallbackEnum {
    case running
    case waiting
    case done
    case idle
    case error
    case closed
    case other

    public static let fallback = Self.other

    public var label: String {
        switch self {
        case .running: "Running"
        case .waiting: "Waiting for permission"
        case .done: "Done"
        case .idle: "Idle"
        case .error: "Error"
        case .closed: "Closed"
        case .other: "Unknown"
        }
    }
}

public enum ToolKind: String, FallbackEnum {
    case read
    case edit
    case delete
    case move
    case search
    case execute
    case think
    case fetch
    case switchMode = "switch_mode"
    case other

    public static let fallback = Self.other

    public var symbol: String {
        switch self {
        case .read: "doc.text"
        case .edit: "pencil"
        case .delete: "trash"
        case .move: "arrow.left.arrow.right"
        case .search: "magnifyingglass"
        case .execute: "terminal"
        case .think: "brain"
        case .fetch: "globe"
        case .switchMode: "arrow.triangle.swap"
        case .other: "wrench.and.screwdriver"
        }
    }

    public var touchesFiles: Bool {
        self == .edit || self == .delete || self == .move
    }
}

public enum ToolStatus: String, FallbackEnum {
    case pending
    case inProgress = "in_progress"
    case completed
    case failed
    case cancelled
    case other

    public static let fallback = Self.other

    public var isOpen: Bool {
        self == .pending || self == .inProgress
    }
}

public enum PlanStatus: String, FallbackEnum {
    case pending
    case inProgress = "in_progress"
    case completed

    public static let fallback = Self.pending
}

public enum MessageRole: String, FallbackEnum {
    case user
    case agent
    case thought

    public static let fallback = Self.agent
}

public enum PermissionKind: String, FallbackEnum {
    case allowOnce = "allow_once"
    case allowAlways = "allow_always"
    case rejectOnce = "reject_once"
    case rejectAlways = "reject_always"
    case other

    public static let fallback = Self.other

    public var allows: Bool {
        self == .allowOnce || self == .allowAlways
    }

    public var rejects: Bool {
        self == .rejectOnce || self == .rejectAlways
    }
}

public enum AgentFinish: String, FallbackEnum {
    case ask
    case merge

    public static let fallback = Self.ask
}

public enum PermissionOutcomeKind: String, FallbackEnum {
    case selected
    case cancelled
    case other

    public static let fallback = Self.other
}

public enum TranscriptEntryKind: String, FallbackEnum {
    case user
    case agent
    case thought
    case tool
    case plan
    case permission
    case notice
    case other

    public static let fallback = Self.other
}
