import Foundation

public enum NotificationKind: String, Sendable, Equatable, CaseIterable {
    case needsInput = "needs-input"
    case permission
    case done
    case stuck
    case info

    public var label: String {
        switch self {
        case .needsInput: "Needs input"
        case .permission: "Permission needed"
        case .done: "Done"
        case .stuck: "Stuck"
        case .info: "Info"
        }
    }

    public var symbol: String {
        switch self {
        case .needsInput: "questionmark.bubble.fill"
        case .permission: "hand.raised.fill"
        case .done: "checkmark.circle.fill"
        case .stuck: "exclamationmark.triangle.fill"
        case .info: "info.circle.fill"
        }
    }
}

public enum NotificationPolicy: String, Sendable, Equatable {
    case all
    case needsInput = "needs-input"
    case muted

    public static let sessionsCommand = "agents.sessions"

    public init(session: String, in sessions: JSONValue) {
        self = sessions.arrayValue?.first { $0["id"]?.stringValue == session }?["notifyEffective"]?.stringValue.flatMap(Self.init(rawValue:)) ?? .all
    }

    public func allows(_ kind: NotificationKind) -> Bool {
        switch self {
        case .all: true
        case .needsInput: kind == .needsInput || kind == .permission
        case .muted: false
        }
    }
}

public struct InboxItem: Sendable, Equatable, Identifiable, Hashable {
    public static let topic = "notify.posted"

    public let id: String
    public let computerID: String
    public let workspace: String
    public let session: String?
    public let title: String
    public let message: String
    public let kind: NotificationKind
    public let viaAgent: Bool
    public let date: Date

    public init(id: String, computerID: String, workspace: String, session: String? = nil, title: String = "", message: String, kind: NotificationKind = .info, viaAgent: Bool = false, date: Date) {
        self.id = id
        self.computerID = computerID
        self.workspace = workspace
        self.session = session
        self.title = title
        self.message = message
        self.kind = kind
        self.viaAgent = viaAgent
        self.date = date
    }

    public init?(_ event: BusEvent, computerID: String, received: Date) {
        guard event.topic == Self.topic, let id = event.payload["id"]?.stringValue, let message = event.payload["message"]?.stringValue else { return nil }
        let nonEmpty = { (key: String) in event.payload[key]?.stringValue.flatMap { $0.isEmpty ? nil : $0 } }
        self.init(
            id: id,
            computerID: computerID,
            workspace: nonEmpty("workspace") ?? event.workspace,
            session: nonEmpty("session"),
            title: nonEmpty("title") ?? "",
            message: message,
            kind: nonEmpty("kind").flatMap(NotificationKind.init(rawValue:)) ?? .info,
            viaAgent: event.payload["agent"] == .bool(true),
            date: nonEmpty("ts").flatMap(Date.parseRFC3339) ?? received
        )
    }
}

public struct Inbox: Sendable, Equatable {
    public static let capacity = 200

    public private(set) var items: [InboxItem] = []
    public private(set) var unread: Set<String> = []

    public init() {}

    @discardableResult
    public mutating func add(_ item: InboxItem) -> Bool {
        guard !items.contains(where: { $0.id == item.id && $0.computerID == item.computerID }) else { return false }
        let index = items.firstIndex { $0.date < item.date } ?? items.endIndex
        items.insert(item, at: index)
        unread.insert(item.id)
        while items.count > Self.capacity {
            unread.remove(items.removeLast().id)
        }
        return true
    }

    public mutating func markRead(_ id: String) {
        unread.remove(id)
    }

    public mutating func markAllRead() {
        unread.removeAll()
    }

    public mutating func clear() {
        items.removeAll()
        unread.removeAll()
    }

    public mutating func remove(computer: String) {
        items.removeAll { $0.computerID == computer }
        unread = unread.filter { id in items.contains { $0.id == id } }
    }
}

public struct LocalAlert: Sendable, Equatable {
    public static let window: TimeInterval = 3600
    public static let kinds: Set<NotificationKind> = [.needsInput, .permission, .stuck, .done]

    public let id: String
    public let title: String
    public let subtitle: String
    public let body: String
    public let computerID: String
    public let workspace: String
    public let session: String?

    public init?(_ item: InboxItem, computerName: String, now: Date) {
        guard Self.kinds.contains(item.kind), now.timeIntervalSince(item.date) < Self.window else { return nil }
        id = item.computerID + "|" + item.id
        title = item.title.isEmpty ? item.kind.label : item.title
        subtitle = computerName
        body = item.message
        computerID = item.computerID
        workspace = item.workspace
        session = item.session
    }
}
