import CryptoKit
import Foundation
import Synchronization

public struct ListCache: Sendable {
    public struct Key: Hashable, Sendable {
        public let computer: String
        public let workspace: String
        public let collection: String
        public let scope: String

        public init(computer: String, workspace: String, collection: String, scope: String = "") {
            self.computer = computer
            self.workspace = workspace
            self.collection = collection
            self.scope = scope
        }
    }

    public static let staleAfter: TimeInterval = 600

    private final class Forgotten: Sendable {
        let computers = Mutex<Set<String>>([])
    }

    private let dir: URL?
    private let forgotten = Forgotten()

    public init(dir: URL?) {
        self.dir = dir
    }

    public func load<T: Codable>(_ key: Key, as type: T.Type) -> T? {
        guard let file = file(key), let data = try? Data(contentsOf: file) else { return nil }
        return try? JSONDecoder.clubmate.decode(T.self, from: data)
    }

    public func save<T: Codable>(_ value: T, _ key: Key) {
        guard let file = file(key), !forgotten.computers.withLock({ $0.contains(key.computer) }) else { return }
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        guard let data = try? encoder.encode(value) else { return }
        try? FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? data.write(to: file, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }

    public func forget(computer: String) {
        forgotten.computers.withLock { _ = $0.insert(computer) }
        guard let dir else { return }
        try? FileManager.default.removeItem(at: dir.appending(path: Self.hash(computer), directoryHint: .isDirectory))
    }

    public func allow(computer: String) {
        forgotten.computers.withLock { _ = $0.remove(computer) }
    }

    public static func ageLine(syncedAt: Date?, error: String?, empty: Bool, now: Date) -> String? {
        guard error != nil, !empty, let syncedAt, now.timeIntervalSince(syncedAt) > staleAfter else { return nil }
        return "as of " + ComputerConnection.clock(syncedAt)
    }

    private func file(_ key: Key) -> URL? {
        dir?.appending(path: Self.hash(key.computer), directoryHint: .isDirectory)
            .appending(path: Self.hash([key.workspace, key.collection, key.scope].joined(separator: "\u{1F}")) + ".json")
    }

    private static func hash(_ text: String) -> String {
        SHA256.hash(data: Data(text.utf8)).prefix(16).map { String(format: "%02x", $0) }.joined()
    }
}

@MainActor
final class SaveThrottle {
    private let interval: Duration
    private var last: ContinuousClock.Instant?
    private var pending: Task<Void, Never>?

    init(interval: Duration = .seconds(5)) {
        self.interval = interval
    }

    func request(_ save: @escaping @MainActor () -> Void) {
        guard let last, ContinuousClock.now - last < interval else {
            pending?.cancel()
            pending = nil
            last = .now
            save()
            return
        }
        guard pending == nil else { return }
        pending = Task { [weak self, interval] in
            try? await Task.sleep(until: last + interval)
            guard let self, !Task.isCancelled else { return }
            self.pending = nil
            self.last = .now
            save()
        }
    }
}
