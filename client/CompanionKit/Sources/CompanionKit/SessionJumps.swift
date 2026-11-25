import Foundation

import ClubmateProtocol

public enum SessionJumps {
    public static let limit = 9

    public static func shown(_ sections: [ShownSection]) -> [AgentSession] {
        var seen: Set<String> = []
        return sections.filter { !$0.section.mirrors }.flatMap(\.rows).map(\.item.node.session).filter { seen.insert($0.id).inserted }
    }

    public static func numbered(_ sections: [ShownSection]) -> [String] {
        Array(shown(sections).filter(\.numbered).map(\.id).prefix(limit))
    }

    public static func needs(_ sessions: [AgentSession]) -> [String] {
        sessions.compactMap { session in session.needsRank.map { ($0, session.id) } }.sorted { $0 < $1 }.map(\.1)
    }

    public static func step(_ ids: [String], from current: String?, by: Int) -> String? {
        guard !ids.isEmpty, by != 0 else { return nil }
        guard let current, let at = ids.firstIndex(of: current) else { return by > 0 ? ids.first : ids.last }
        return ids[((at + by) % ids.count + ids.count) % ids.count]
    }
}
