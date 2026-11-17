import Foundation

public actor TranscriptCache {
    public struct Key: Hashable, Codable, Sendable {
        public let computer: String
        public let workspace: String
        public let session: String

        public init(computer: String, workspace: String, session: String) {
            self.computer = computer
            self.workspace = workspace
            self.session = session
        }

        var list: ListCache.Key {
            ListCache.Key(computer: computer, workspace: workspace, collection: TranscriptCache.collection, scope: session)
        }
    }

    struct Record: Codable {
        let key: Key
        var bytes: Int
        var used: Date
    }

    struct Stored: Codable {
        var version = TranscriptCache.version
        let transcript: AgentTranscript
    }

    static let version = 1
    static let collection = "transcript"
    static let indexKey = ListCache.Key(computer: "", workspace: "", collection: "transcripts")

    public let maxBytes: Int
    public let maxCount: Int
    private let lists: ListCache
    private let now: @Sendable () -> Date
    private var records: [Record]?

    public init(lists: ListCache, maxBytes: Int = 64 << 20, maxCount: Int = 40, now: @escaping @Sendable () -> Date = { .now }) {
        self.lists = lists
        self.maxBytes = maxBytes
        self.maxCount = maxCount
        self.now = now
    }

    public func load(_ key: Key) -> AgentTranscript? {
        var index = loadIndex()
        guard let i = index.firstIndex(where: { $0.key == key }) else { return nil }
        guard let stored = lists.load(key.list, as: Stored.self), stored.version == Self.version else {
            lists.remove(key.list)
            index.remove(at: i)
            keep(index)
            return nil
        }
        index[i].used = now()
        keep(index)
        return stored.transcript
    }

    public func save(_ transcript: AgentTranscript, _ key: Key) {
        guard let bytes = lists.save(Stored(transcript: transcript), key.list) else { return }
        var index = loadIndex().filter { $0.key != key }
        index.append(Record(key: key, bytes: bytes, used: now()))
        index.sort { $0.used < $1.used }
        while index.count > maxCount || index.reduce(0, { $0 + $1.bytes }) > maxBytes, index.count > 1 {
            lists.remove(index.removeFirst().key.list)
        }
        keep(index)
    }

    public func remove(_ keys: [Key]) {
        guard !keys.isEmpty else { return }
        for key in keys {
            lists.remove(key.list)
        }
        let gone = Set(keys)
        keep(loadIndex().filter { !gone.contains($0.key) })
    }

    public func forget(computer: String) {
        keep(loadIndex().filter { $0.key.computer != computer })
    }

    public func cached() -> [Key] {
        loadIndex().map(\.key)
    }

    private func loadIndex() -> [Record] {
        if let records { return records }
        let loaded = lists.load(Self.indexKey, as: [Record].self) ?? []
        records = loaded
        return loaded
    }

    private func keep(_ index: [Record]) {
        records = index
        lists.save(index, Self.indexKey)
    }
}
