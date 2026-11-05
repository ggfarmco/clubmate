import Foundation

import ClubmateProtocol

public struct InboxItem: Sendable, Equatable, Identifiable, Hashable {
    public let notificationID: String
    public let computerID: String
    public let workspace: String
    public let session: String?
    public let collapse: String?
    public let title: String
    public let message: String
    public let kind: NotificationKind
    public let viaAgent: Bool
    public let pushed: Bool
    public let date: Date

    public init(notificationID: String, computerID: String, workspace: String, session: String? = nil, collapse: String? = nil, title: String = "", message: String, kind: NotificationKind = .info, viaAgent: Bool = false, pushed: Bool = false, date: Date) {
        self.notificationID = notificationID
        self.computerID = computerID
        self.workspace = workspace
        self.session = session
        self.collapse = collapse
        self.title = title
        self.message = message
        self.kind = kind
        self.viaAgent = viaAgent
        self.pushed = pushed
        self.date = date
    }

    public var id: String {
        computerID + "|" + notificationID
    }

    public init?(_ event: BusEvent, computerID: String, received: Date) {
        guard event.topic == Topic.notifyPosted, let posted = NotifyPosted(event.payload) else { return nil }
        self.init(
            notificationID: posted.id,
            computerID: computerID,
            workspace: posted.workspace ?? event.workspace,
            session: posted.session,
            collapse: posted.collapse,
            title: posted.title,
            message: posted.message,
            kind: posted.kind,
            viaAgent: posted.viaAgent,
            pushed: posted.pushed,
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
        guard !items.contains(where: { $0.id == item.id }) else { return false }
        let index = items.firstIndex { $0.date < item.date } ?? items.endIndex
        items.insert(item, at: index)
        unread.insert(item.id)
        while items.count > Self.capacity {
            unread.remove(items.removeLast().id)
        }
        return true
    }

    public func isUnread(_ item: InboxItem) -> Bool {
        unread.contains(item.id)
    }

    public mutating func markRead(_ item: InboxItem) {
        unread.remove(item.id)
    }

    public mutating func markAllRead() {
        unread.removeAll()
    }

    public mutating func clear() {
        items.removeAll()
        unread.removeAll()
    }

    public mutating func resolve(computer: String, session: String, kind: NotificationKind) -> [String] {
        let resolved = items.filter { $0.computerID == computer && $0.session == session && $0.kind == kind }.map(\.id)
        unread.subtract(resolved)
        return resolved
    }

    public mutating func remove(computer: String) {
        items.removeAll { $0.computerID == computer }
        unread.subtract(unread.filter { $0.hasPrefix(computer + "|") })
    }
}

public struct LocalAlert: Sendable, Equatable {
    public static let window: TimeInterval = 3600
    public static let kinds: Set<NotificationKind> = [.needsInput, .permission, .question, .stuck, .done]

    public let id: String
    public let identifier: String
    public let title: String
    public let subtitle: String
    public let body: String
    public let computerID: String
    public let workspace: String
    public let session: String?
    public let kind: NotificationKind

    public init?(_ item: InboxItem, computerName: String, workspaceName: String?, now: Date) {
        guard Self.kinds.contains(item.kind), now.timeIntervalSince(item.date) < Self.window else { return nil }
        id = item.id
        identifier = item.collapse ?? item.id
        title = [computerName, workspaceName ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
        subtitle = item.title.isEmpty ? item.kind.label : item.title
        body = item.message
        computerID = item.computerID
        workspace = item.workspace
        session = item.session
        kind = item.kind
    }
}

public struct InboxSections: Sendable, Equatable {
    public let waiting: [InboxItem]
    public let earlier: [InboxItem]
}

extension InboxItem {
    public var needsAction: Bool {
        kind == .needsInput || kind == .permission || kind == .question
    }

    public var displayTitle: String {
        title.isEmpty ? kind.label : title
    }

    public var heading: String {
        switch kind {
        case .needsInput, .question: "Question"
        case .permission: "Permission"
        default: displayTitle
        }
    }

    public func age(now: Date) -> String {
        let seconds = max(0, Int(now.timeIntervalSince(date)))
        switch seconds {
        case ..<60: return "\(seconds) s"
        case ..<3600: return "\(seconds / 60) min"
        case ..<86_400: return "\(seconds / 3600) h"
        default: return "\(seconds / 86_400) d"
        }
    }

    public static func pendingCommand(_ entries: [TranscriptEntry]) -> String? {
        for entry in entries.reversed() {
            if case .permission(let request) = entry.content, request.outcome == nil {
                return request.toolCall.command ?? (request.toolCall.title.isEmpty ? nil : request.toolCall.title)
            }
        }
        return nil
    }
}

extension Inbox {
    public func sections(computer: String, sessionWaiting: (InboxItem) -> Bool?) -> InboxSections {
        var waiting: [InboxItem] = []
        var earlier: [InboxItem] = []
        var claimed: Set<String> = []
        for item in items where item.computerID == computer {
            let candidate = item.needsAction && (sessionWaiting(item) ?? isUnread(item))
            if candidate, item.session.map({ claimed.insert($0).inserted }) ?? true {
                waiting.append(item)
            } else {
                earlier.append(item)
            }
        }
        return InboxSections(waiting: waiting, earlier: earlier)
    }

    public func showsDot(_ item: InboxItem, in sections: InboxSections) -> Bool {
        isUnread(item) && sections.waiting.contains(item)
    }
}
