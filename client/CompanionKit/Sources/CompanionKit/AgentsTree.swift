import Foundation

import ClubmateProtocol

public enum ListFilter: String, CaseIterable, Sendable, Identifiable {
    case all
    case needs
    case open
    case running

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .all: "All"
        case .needs: "Needs you"
        case .open: "Open"
        case .running: "Running"
        }
    }
}

public struct ListSection: Sendable, Equatable, Identifiable {
    public enum Kind: Sendable, Equatable {
        case needsYou
        case open
        case paused(Date?)
        case worktree(String)
        case closed
        case service
    }

    public let kind: Kind
    public let title: String?
    public let nodes: [SessionNode]

    public var id: String {
        switch kind {
        case .needsYou: "inbox"
        case .open: "inbox-open"
        case .paused(let until): "paused-\(until.map { Int($0.timeIntervalSince1970) } ?? 0)"
        case .worktree(let key): "wt:" + key
        case .closed: "closed"
        case .service: "service"
        }
    }

    public var mirrors: Bool {
        kind == .needsYou || kind == .open
    }

    public var folds: Bool {
        kind == .closed || kind == .service
    }

    public func title(now: Date, calendar: Calendar = .autoupdatingCurrent) -> String? {
        guard case .paused(let until) = kind, let until else { return title }
        return "Paused until \(LimitWindow.clock(until, now: now, calendar: calendar))"
    }

    public func rows(expanded: Set<String>, folded: Set<String>, unfolded: Set<String>) -> [ListRow] {
        if folds, !unfolded.contains(id) { return [] }
        if mirrors {
            return nodes.map { ListRow(section: id, item: SessionRowItem(node: $0, depth: 0, expanded: false, hidden: 0), place: $0.place) }
        }
        return nodes.flatMap { $0.rows(expanded: expanded, folded: folded) }.map { ListRow(section: id, item: $0, place: nil) }
    }
}

public struct ListRow: Sendable, Equatable, Identifiable {
    public let section: String
    public var item: SessionRowItem
    public let place: String?

    public init(section: String, item: SessionRowItem, place: String?) {
        self.section = section
        self.item = item
        self.place = place
    }

    public var id: String { section + "|" + item.id }
    public var session: String { item.node.id }
}

public struct LayoutEntry: Sendable, Hashable {
    public let id: String
    public let session: String?
}

public enum AgentsTree {
    public static func supported(_ sessions: [AgentSession]) -> Bool {
        guard sessions.contains(where: { $0.listFilters != nil }) else { return false }
        return sessions.contains { $0.inboxKey != nil } || !sessions.contains { $0.parentSession == nil && ($0.group == .needsInput || $0.group == .open) }
    }

    public static func counts(_ sessions: [AgentSession]) -> [ListFilter: Int] {
        var out: [ListFilter: Int] = [:]
        for filter in ListFilter.allCases {
            out[filter] = filter == .open ? sessions.reduce(0) { $0 + $1.listOpen } : sessions.count { $0.listFilters?.contains(filter.rawValue) == true }
        }
        return out
    }

    public static func sections(_ sessions: [AgentSession], filter: ListFilter = .all) -> [ListSection] {
        let roots = AgentSession.trees(sessions).filter { filter == .all || $0.matches(filter) }
        var out: [ListSection] = []
        if filter == .all {
            let inbox = roots.flatMap(\.inboxed).sorted(by: SessionNode.inboxFirst)
            let needs = inbox.filter { $0.session.inboxKey?.hasPrefix(Self.needsSection) == true }
            let open = inbox.filter { $0.session.inboxKey?.hasPrefix(Self.needsSection) != true }
            if !needs.isEmpty { out.append(ListSection(kind: .needsYou, title: "Needs you", nodes: needs)) }
            if !open.isEmpty { out.append(ListSection(kind: .open, title: "Open", nodes: open)) }
        }
        var closed: [SessionNode] = []
        var service: [SessionNode] = []
        var paused: [Date?: [SessionNode]] = [:]
        var groups: [String: [SessionNode]] = [:]
        for root in roots {
            if root.session.isClosed {
                closed.append(root)
            } else if root.session.service {
                service.append(root)
            } else if root.session.group == .paused {
                paused[root.session.pausedUntil, default: []].append(root)
            } else {
                groups[root.groupKey, default: []].append(root)
            }
        }
        let rowOrder = filter == .needs ? SessionNode.inboxFirst : SessionNode.listedFirst
        for until in paused.keys.sorted(by: { ($0 ?? .distantFuture) < ($1 ?? .distantFuture) }) {
            out.append(ListSection(kind: .paused(until), title: "Paused", nodes: (paused[until] ?? []).sorted(by: SessionNode.listedFirst)))
        }
        var worktrees = groups.map { key, nodes in (key: key, nodes: nodes.sorted(by: rowOrder)) }.sorted { $0.key < $1.key }
        if filter == .needs {
            worktrees.sort { rowOrder($0.nodes[0], $1.nodes[0]) }
        }
        for (key, nodes) in worktrees {
            let lone = worktrees.count == 1 && key == homeGroup
            out.append(ListSection(kind: .worktree(key), title: lone ? nil : nodes[0].groupLabel, nodes: nodes))
        }
        if !closed.isEmpty { out.append(ListSection(kind: .closed, title: SessionGroup.closed.title, nodes: closed.sorted(by: SessionNode.newerFirst))) }
        if !service.isEmpty { out.append(ListSection(kind: .service, title: SessionGroup.service.title, nodes: service.sorted(by: SessionNode.newerFirst))) }
        return out
    }

    public static func layout(_ sections: [ListSection], expanded: Set<String>, folded: Set<String>, unfolded: Set<String>) -> [LayoutEntry] {
        sections.flatMap { section in
            [LayoutEntry(id: "#" + section.id, session: nil)] + section.rows(expanded: expanded, folded: folded, unfolded: unfolded).map { LayoutEntry(id: $0.id, session: $0.session) }
        }
    }

    static let homeGroup = "0"
    static let needsSection = "0"
}

extension SessionNode {
    var groupKey: String {
        groupKeyIfSent ?? "~"
    }

    private var groupKeyIfSent: String? {
        session.listGroupKey ?? children.compactMap(\.groupKeyIfSent).min()
    }

    var groupLabel: String {
        session.listGroupLabel ?? children.lazy.compactMap(\.groupLabelIfSent).first ?? session.worktreeName
    }

    private var groupLabelIfSent: String? {
        session.listGroupLabel ?? children.lazy.compactMap(\.groupLabelIfSent).first
    }

    var place: String? {
        session.listGroupLabel ?? children.lazy.compactMap(\.groupLabelIfSent).first
    }

    var rowKey: String {
        session.listKey ?? children.compactMap(\.session.listKey).min() ?? ""
    }

    var inboxed: [SessionNode] {
        (session.inboxKey == nil ? [] : [self]) + children.flatMap(\.inboxed)
    }

    func matches(_ filter: ListFilter) -> Bool {
        session.listFilters?.contains(filter.rawValue) == true || children.contains { $0.matches(filter) }
    }

    func restyled(_ latest: [String: AgentSession]) -> SessionNode {
        SessionNode(session: latest[id] ?? session, children: children.map { $0.restyled(latest) }, group: group, updatedAt: updatedAt, needsYouAt: needsYouAt)
    }

    static func listedFirst(_ a: SessionNode, _ b: SessionNode) -> Bool {
        let (x, y) = (a.rowKey, b.rowKey)
        if x != y { return x < y }
        return a.session.createdAt != b.session.createdAt ? a.session.createdAt < b.session.createdAt : a.id < b.id
    }

    static func inboxFirst(_ a: SessionNode, _ b: SessionNode) -> Bool {
        switch (a.session.inboxKey, b.session.inboxKey) {
        case let (x?, y?) where x != y: x < y
        case (.some, nil): true
        case (nil, .some): false
        default: listedFirst(a, b)
        }
    }
}

public struct ListHold: Sendable, Equatable {
    public private(set) var shown: [AgentSession]?
    public private(set) var pill: String?

    public init() {}

    public var held: Bool { pill != nil }

    public mutating func offer(_ latest: [AgentSession], layout: ([AgentSession]) -> (sections: [ListSection], entries: [LayoutEntry]), visible: Set<String>, onScreen: Bool) {
        guard let shown, onScreen else { return land(latest) }
        let before = layout(shown)
        let after = layout(latest)
        if before.entries == after.entries { return land(latest) }
        let first = before.entries.indices.first { $0 >= after.entries.count || before.entries[$0] != after.entries[$0] } ?? before.entries.count
        guard let last = before.entries.lastIndex(where: { visible.contains($0.id) }), first <= last else { return land(latest) }
        pill = Self.pill(before: before, after: after, known: Set(shown.map(\.id)))
    }

    public mutating func land(_ latest: [AgentSession]) {
        shown = latest
        pill = nil
    }

    static func pill(before: (sections: [ListSection], entries: [LayoutEntry]), after: (sections: [ListSection], entries: [LayoutEntry]), known: Set<String>) -> String {
        let inbox = { (sections: [ListSection]) in Set(sections.filter { $0.kind == .needsYou }.flatMap { $0.nodes.map(\.id) }) }
        let needs = inbox(after.sections).subtracting(inbox(before.sections))
        let fresh = Set(after.entries.compactMap(\.session)).subtracting(known)
        let moved = Set(Set(before.entries).symmetricDifference(after.entries).compactMap(\.session)).subtracting(needs).subtracting(fresh)
        let parts = [(needs.count, "needs you"), (fresh.count, "new"), (moved.count, "moved")].filter { $0.0 > 0 }.map { "\($0.0) \($0.1)" }
        return parts.isEmpty ? "Show changes" : parts.joined(separator: " · ")
    }
}
