import Foundation

public protocol SyncedItem: Decodable, Identifiable, Sendable where ID: Codable & Sendable {
    static var field: String { get }
}

public struct SyncedList<Item: SyncedItem>: Codable, Sendable {
    public private(set) var items: [Item] = []
    public private(set) var rows: [JSONValue] = []
    public private(set) var cursor: String?

    public init() {}

    public init(rows: [JSONValue], cursor: String?) throws {
        try replace(rows)
        self.cursor = cursor
    }

    public var args: JSONValue {
        .object(["since": .string(cursor ?? "")])
    }

    public static func synced(_ reply: JSONValue) -> Bool {
        reply["cursor"]?.stringValue != nil
    }

    public func row(_ id: Item.ID) -> JSONValue? {
        items.firstIndex { $0.id == id }.map { rows[$0] }
    }

    public mutating func apply(_ reply: JSONValue) throws {
        guard let cursor = reply["cursor"]?.stringValue else { throw NotSynced() }
        let fresh = reply[Item.field]?.arrayValue ?? []
        let order = try reply["order"].flatMap { $0 == .null ? nil : try $0.decode([Item.ID].self) }
        if reply["delta"]?.boolValue == true {
            let removed = Set(try reply["removed"]?.decode([Item.ID].self) ?? [])
            var merged: [Item.ID: (Item, JSONValue)] = [:]
            for (item, row) in zip(items, rows) where !removed.contains(item.id) {
                merged[item.id] = (item, row)
            }
            var ids = items.map(\.id)
            for row in fresh {
                let item = try row.decode(Item.self)
                if merged[item.id] == nil { ids.append(item.id) }
                merged[item.id] = (item, row)
            }
            var placed: Set<Item.ID> = []
            let kept = (order ?? ids).compactMap { id in placed.insert(id).inserted ? merged[id] : nil }
            items = kept.map(\.0)
            rows = kept.map(\.1)
        } else {
            try replace(fresh)
            if let order { sort(order) }
        }
        self.cursor = cursor
    }

    public struct NotSynced: Error {}

    public mutating func replace(_ fresh: [JSONValue]) throws {
        items = try fresh.map { try $0.decode(Item.self) }
        rows = fresh
        cursor = nil
    }

    public mutating func replace(_ fetched: [JSONValue], for id: Item.ID) throws {
        let row = try fetched.first { try $0.decode(Item.self).id == id }
        let index = items.firstIndex { $0.id == id }
        switch (index, row) {
        case (let i?, let row?):
            items[i] = try row.decode(Item.self)
            rows[i] = row
        case (let i?, nil):
            items.remove(at: i)
            rows.remove(at: i)
        case (nil, let row?):
            items.append(try row.decode(Item.self))
            rows.append(row)
        case (nil, nil):
            break
        }
    }

    private mutating func sort(_ order: [Item.ID]) {
        let rank = Dictionary(order.enumerated().map { ($1, $0) }, uniquingKeysWith: { a, _ in a })
        let sorted = zip(items, rows).enumerated().sorted { (rank[$0.element.0.id] ?? order.count + $0.offset, $0.offset) < (rank[$1.element.0.id] ?? order.count + $1.offset, $1.offset) }
        items = sorted.map(\.element.0)
        rows = sorted.map(\.element.1)
    }

    private enum CodingKeys: String, CodingKey {
        case rows, cursor
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        try replace(c.decode([JSONValue].self, forKey: .rows))
        cursor = try c.decodeIfPresent(String.self, forKey: .cursor)
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(rows, forKey: .rows)
        try c.encodeIfPresent(cursor, forKey: .cursor)
    }
}

public enum SyncCursor {
    public static func newer(_ other: String?, current: String?) -> String? {
        guard let other, let current, let a = split(other), let b = split(current), a.epoch == b.epoch else { return current ?? other }
        return a.seq > b.seq ? other : current
    }

    private static func split(_ cursor: String) -> (epoch: Substring, seq: Int)? {
        guard let dot = cursor.lastIndex(of: "."), let seq = Int(cursor[cursor.index(after: dot)...]) else { return nil }
        return (cursor[..<dot], seq)
    }
}
