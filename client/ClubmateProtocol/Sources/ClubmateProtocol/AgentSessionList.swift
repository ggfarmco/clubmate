import Foundation

public struct AgentSessionList: Codable, Sendable, Equatable {
    public private(set) var sessions: [AgentSession] = []
    public private(set) var cursor: String?

    public init() {}

    public var args: JSONValue {
        .object(["since": .string(cursor ?? "")])
    }

    public mutating func apply(_ reply: JSONValue) throws {
        guard reply.arrayValue == nil else {
            sessions = try reply.decode([AgentSession].self)
            cursor = nil
            return
        }
        let page = try reply.decode(Page.self)
        cursor = page.cursor
        guard page.delta == true else {
            sessions = page.sessions
            return
        }
        let removed = Set(page.removed)
        let changed = Dictionary(page.sessions.map { ($0.id, $0) }, uniquingKeysWith: { $1 })
        let known = Set(sessions.map(\.id))
        sessions = sessions.compactMap { removed.contains($0.id) ? nil : changed[$0.id] ?? $0 } + page.sessions.filter { !known.contains($0.id) }
    }

    public mutating func replace(_ fetched: [AgentSession], for id: String) {
        let row = fetched.first { $0.id == id }
        if let i = sessions.firstIndex(where: { $0.id == id }) {
            if let row { sessions[i] = row } else { sessions.remove(at: i) }
        } else if let row {
            sessions.append(row)
        }
    }

    private struct Page: Decodable {
        let sessions: [AgentSession]
        let removed: [String]
        let cursor: String
        let delta: Bool?
    }
}
