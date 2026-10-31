import Foundation
import Synchronization

public struct InboxEntry: Sendable, Equatable, Codable, Identifiable {
    public let note: HubNotification
    public let date: Date
    public var seen: Bool
    public var handled: Bool

    public var id: String { note.id }
}

public enum InboxPin: Sendable, Equatable, Identifiable {
    case permission(AgentAlert)
    case input(InboxEntry)

    public var id: String {
        switch self {
        case .permission(let alert): alert.id
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

    private enum CodingKeys: String, CodingKey {
        case entries
        case lastSeq
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        entries = (try? container.decode([Lossy<InboxEntry>].self, forKey: .entries))?.compactMap(\.value) ?? []
        lastSeq = (try? container.decode(Int64.self, forKey: .lastSeq)) ?? 0
    }

    @discardableResult
    public mutating func add(_ note: HubNotification, at date: Date, handled: Bool, now: Date) -> Bool {
        guard !entries.contains(where: { $0.id == note.id }) else { return false }
        let index = entries.firstIndex { $0.date <= date } ?? entries.endIndex
        entries.insert(InboxEntry(note: note, date: date, seen: handled, handled: handled), at: index)
        prune(now: now)
        return entries.contains { $0.id == note.id }
    }

    public mutating func record(seq: Int64) {
        lastSeq = seq
    }

    public mutating func forgetSeq() {
        lastSeq = 0
    }

    public mutating func prune(now: Date) {
        entries = Array(entries.filter { now.timeIntervalSince($0.date) < Self.maxAge }.prefix(Self.limit))
    }

    public mutating func handle(_ id: String) {
        update { $0.id == id }
    }

    public mutating func handleAll() {
        update { _ in true }
    }

    public mutating func handleShown(_ focus: HubFocus) {
        update { focus.shows(workspace: $0.note.workspace, session: $0.note.session) }
    }

    public mutating func markSeen(workspace: String) {
        for index in entries.indices where entries[index].note.workspace == workspace {
            entries[index].seen = true
        }
    }

    public mutating func keep(_ workspaces: Set<String>) {
        entries.removeAll { !workspaces.contains($0.note.workspace) }
    }

    public func unseen(_ workspace: String) -> Int {
        entries.count { !$0.seen && $0.note.workspace == workspace }
    }

    public var unseenTotal: Int {
        entries.count { !$0.seen }
    }

    public var unhandled: Set<String> {
        Set(entries.filter { !$0.handled }.map(\.id))
    }

    public func history(_ kind: NotificationKind?) -> [InboxEntry] {
        guard let kind else { return entries }
        return entries.filter { $0.note.kind == kind }
    }

    public func pinned(waiting: [AgentAlert]) -> [InboxPin] {
        waiting.map(InboxPin.permission) + entries.filter { !$0.handled && $0.note.kind == .needsInput }.map(InboxPin.input)
    }

    private mutating func update(_ matches: (InboxEntry) -> Bool) {
        for index in entries.indices where !entries[index].handled && matches(entries[index]) {
            entries[index].seen = true
            entries[index].handled = true
        }
    }

    public static func load(from url: URL) -> NotificationInbox {
        guard let data = try? Data(contentsOf: url), let inbox = try? JSONDecoder().decode(NotificationInbox.self, from: data) else { return NotificationInbox() }
        return inbox
    }

    public func save(to url: URL) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try JSONEncoder().encode(self).write(to: url, options: .atomic)
    }

    public static func resumePoint(lastSeq: Int64, read: Result<JSONValue, any Error>) -> Int64 {
        guard lastSeq > 0 else { return .max }
        guard case .success(let events) = read else { return lastSeq }
        guard case .number(let seq)? = events.arrayValue?.first?.objectValue?["seq"], Int64(seq) == lastSeq else { return .max }
        return lastSeq
    }
}

public final class InboxWriter: Sendable {
    private let url: URL
    private let written = Mutex<UInt64>(0)

    public init(url: URL) {
        self.url = url
    }

    public func save(_ inbox: NotificationInbox, generation: UInt64) {
        written.withLock { last in
            guard generation > last else { return }
            last = generation
            try? inbox.save(to: url)
        }
    }
}

public struct AgentWaiting: Sendable, Equatable {
    public private(set) var alerts: [AgentAlert] = []

    public init() {}

    public func has(workspace: String, session: String?) -> Bool {
        alerts.contains { Self.scope($0, workspace, session) }
    }

    public mutating func update(workspace: String, session: String?, sessions: [AgentSession]) {
        alerts.removeAll { Self.scope($0, workspace, session) }
        alerts += AgentNotifier.requests(sessions.filter { $0.workspace == workspace && (session == nil || $0.id == session) })
    }

    public mutating func remove(workspace: String, session: String) {
        alerts.removeAll { Self.scope($0, workspace, session) }
    }

    public mutating func keep(_ workspaces: Set<String>) {
        alerts.removeAll { !workspaces.contains($0.workspace) }
    }

    private static func scope(_ alert: AgentAlert, _ workspace: String, _ session: String?) -> Bool {
        alert.workspace == workspace && (session == nil || alert.session == session)
    }
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

private struct Lossy<Value: Decodable>: Decodable {
    let value: Value?

    init(from decoder: any Decoder) throws {
        value = try? decoder.singleValueContainer().decode(Value.self)
    }
}
