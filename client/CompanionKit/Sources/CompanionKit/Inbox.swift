import Foundation

import ClubmateProtocol

public struct InboxItem: Sendable, Equatable, Identifiable, Hashable {
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

    public var key: String {
        computerID + "|" + id
    }

    public init?(_ event: BusEvent, computerID: String, received: Date) {
        guard event.topic == Topic.notifyPosted, let posted = NotifyPosted(event.payload) else { return nil }
        self.init(
            id: posted.id,
            computerID: computerID,
            workspace: posted.workspace ?? event.workspace,
            session: posted.session,
            title: posted.title,
            message: posted.message,
            kind: posted.kind,
            viaAgent: posted.viaAgent,
            date: posted.date ?? received
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
        unread.insert(item.key)
        while items.count > Self.capacity {
            unread.remove(items.removeLast().key)
        }
        return true
    }

    public func isUnread(_ item: InboxItem) -> Bool {
        unread.contains(item.key)
    }

    public mutating func markRead(_ item: InboxItem) {
        unread.remove(item.key)
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
        unread.subtract(unread.filter { $0.hasPrefix(computer + "|") })
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
        id = item.key
        title = item.title.isEmpty ? item.kind.label : item.title
        subtitle = computerName
        body = item.message
        computerID = item.computerID
        workspace = item.workspace
        session = item.session
    }
}
