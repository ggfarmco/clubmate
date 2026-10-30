import Foundation

public struct InboxEntry: Sendable, Equatable, Codable, Identifiable {
    public let note: HubNotification
    public let date: Date
    public var read: Bool

    public var id: String { note.id }
}

public enum InboxPin: Sendable, Equatable, Identifiable {
    case permission(AgentAlert)
    case input(InboxEntry)

    public var id: String {
        switch self {
        case .permission(let alert): "\(alert.session)/\(alert.request)"
        case .input(let entry): entry.id
        }
    }
}

public struct NotificationInbox: Sendable, Equatable, Codable {
    public static let limit = 200
    public static let maxAge: TimeInterval = 7 * 24 * 60 * 60

    public private(set) var entries: [InboxEntry] = []
    public private(set) var lastSeq: Int64 = 0

    public init() {}

    @discardableResult
    public mutating func add(_ note: HubNotification, at date: Date, read: Bool, now: Date) -> Bool {
        guard !entries.contains(where: { $0.id == note.id }) else { return false }
        let index = entries.firstIndex { $0.date < date } ?? entries.endIndex
        entries.insert(InboxEntry(note: note, date: date, read: read), at: index)
        prune(now: now)
        return entries.contains { $0.id == note.id }
    }

    public mutating func advance(to seq: Int64) {
        lastSeq = max(lastSeq, seq)
    }

    public mutating func prune(now: Date) {
        entries = Array(entries.filter { now.timeIntervalSince($0.date) < Self.maxAge }.prefix(Self.limit))
    }

    public mutating func markRead(_ id: String) {
        for index in entries.indices where entries[index].id == id {
            entries[index].read = true
        }
    }

    public mutating func markRead(workspace: String) {
        for index in entries.indices where entries[index].note.workspace == workspace {
            entries[index].read = true
        }
    }

    public mutating func markAllRead() {
        for index in entries.indices {
            entries[index].read = true
        }
    }

    public mutating func keep(_ workspaces: Set<String>) {
        entries.removeAll { !workspaces.contains($0.note.workspace) }
    }

    public func unread(_ workspace: String) -> Int {
        entries.count { !$0.read && $0.note.workspace == workspace }
    }

    public var unreadTotal: Int {
        entries.count { !$0.read }
    }

    public func history(_ kind: NotificationKind?) -> [InboxEntry] {
        guard let kind else { return entries }
        return entries.filter { $0.note.kind == kind }
    }

    public func pinned(waiting: [AgentAlert]) -> [InboxPin] {
        waiting.map(InboxPin.permission) + entries.filter { !$0.read && $0.note.kind == .needsInput }.map(InboxPin.input)
    }

    public static func load(from url: URL) -> NotificationInbox {
        guard let data = try? Data(contentsOf: url), let inbox = try? decoder.decode(NotificationInbox.self, from: data) else { return NotificationInbox() }
        return inbox
    }

    public func save(to url: URL) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Self.encoder.encode(self).write(to: url, options: .atomic)
    }

    private static let encoder = {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    private static let decoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()
}

public enum SystemNotifications {
    public enum Status: Sendable {
        case notDetermined
        case denied
        case allowed
    }

    public enum Step: Sendable, Equatable {
        case request
        case deliver
        case off
    }

    public static func step(_ status: Status, asked: Bool) -> Step {
        switch status {
        case .allowed: .deliver
        case .denied: .off
        case .notDetermined: asked ? .off : .request
        }
    }
}
