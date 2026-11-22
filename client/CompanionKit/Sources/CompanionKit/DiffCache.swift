import Foundation
import Synchronization

import ClubmateProtocol

public enum DiffCache {
    static let limit = 128

    public final class Parsed: Sendable {
        public let files: [DiffFile]
        public let added: Int
        public let removed: Int

        init(files: [DiffFile]) {
            self.files = files
            added = files.reduce(0) { $0 + $1.added }
            removed = files.reduce(0) { $0 + $1.removed }
        }

        public var changes: (added: Int, removed: Int) { (added, removed) }
    }

    struct Key: Hashable {
        let text: String

        static func == (a: Key, b: Key) -> Bool { a.text == b.text }

        func hash(into hasher: inout Hasher) {
            let utf8 = text.utf8
            hasher.combine(utf8.count)
            for byte in utf8.prefix(256) { hasher.combine(byte) }
            for byte in utf8.suffix(256) { hasher.combine(byte) }
        }
    }

    private struct Generations {
        var recent: [Key: Parsed] = [:]
        var older: [Key: Parsed] = [:]

        mutating func insert(_ key: Key, _ value: Parsed) {
            if recent.count >= DiffCache.limit {
                older = recent
                recent = [:]
            }
            recent[key] = value
        }
    }

    private static let parsed = Mutex(Generations())

    public static func parse(_ text: String) -> Parsed {
        let key = Key(text: text)
        let hit = parsed.withLock { cache -> Parsed? in
            if let hit = cache.recent[key] { return hit }
            guard let hit = cache.older[key] else { return nil }
            cache.insert(key, hit)
            return hit
        }
        if let hit { return hit }
        let result = Parsed(files: DiffFile.parse(text))
        parsed.withLock { $0.insert(key, result) }
        return result
    }
}
