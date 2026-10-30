import Foundation

public enum TranscriptItem: Sendable, Equatable, Identifiable {
    case node(SDUIChild)
    case action(SDUIChild)
    case actions(id: String, cards: [SDUIChild])

    public var id: String {
        switch self {
        case let .node(child), let .action(child): child.id
        case let .actions(id, _): id
        }
    }
}

public struct ToolActionsSummary: Sendable, Equatable {
    public let text: String
    public let running: String?

    private static let order = ["read", "search", "fetch", "execute", "edit", "delete", "move", "think", "switch_mode", "other"]

    public init(_ cards: [SDUINode]) {
        var counts: [String: Int] = [:]
        for card in cards {
            let kind = card.string("kind").flatMap { Self.order.contains($0) ? $0 : nil } ?? "other"
            counts[kind, default: 0] += 1
        }
        let parts = counts
            .sorted { $0.value != $1.value ? $0.value > $1.value : Self.rank($0.key) < Self.rank($1.key) }
            .map { Self.phrase($0.key, $0.value) }
        text = "\(Self.plural(cards.count, "action", "actions")): " + parts.joined(separator: ", ")
        running = cards.last { ["pending", "in_progress"].contains($0.string("status")) }.map { ToolLineTitle($0).text }
    }

    private static func rank(_ kind: String) -> Int {
        order.firstIndex(of: kind) ?? order.count
    }

    private static func phrase(_ kind: String, _ count: Int) -> String {
        switch kind {
        case "read": "read " + plural(count, "file", "files")
        case "search": plural(count, "search", "searches")
        case "fetch": plural(count, "fetch", "fetches")
        case "execute": plural(count, "command", "commands")
        case "edit": "edited " + plural(count, "file", "files")
        case "delete": "deleted " + plural(count, "file", "files")
        case "move": "moved " + plural(count, "file", "files")
        case "think": plural(count, "thought", "thoughts")
        case "switch_mode": plural(count, "mode switch", "mode switches")
        default: plural(count, "other tool", "other tools")
        }
    }

    private static func plural(_ count: Int, _ one: String, _ many: String) -> String {
        "\(count) \(count == 1 ? one : many)"
    }
}

public struct ToolLineTitle: Sendable, Equatable {
    public let text: String
    public let help: String?

    public init(_ card: SDUINode) {
        let title = card.string("title") ?? ""
        let paths = ["edit", "delete", "move"].contains(card.string("kind")) ? Self.paths(in: title) : []
        let shown = Array(paths.prefix(card.string("kind") == "move" ? 2 : 1))
        guard !shown.isEmpty else {
            text = title
            help = nil
            return
        }
        text = shown.map { $0.split(separator: "/").last.map(String.init) ?? $0 }.joined(separator: " → ")
        help = shown.joined(separator: " → ")
    }

    private static func paths(in title: String) -> [String] {
        let quoted = title.split(separator: "`", omittingEmptySubsequences: false).enumerated().filter { $0.offset % 2 == 1 }.map { String($0.element) }
        let candidates = quoted.isEmpty ? title.split(separator: " ").map(String.init).filter { $0.contains("/") } : quoted
        return candidates.map(clean).filter { !$0.isEmpty }
    }

    private static func clean(_ token: String) -> String {
        var token = token.trimmingCharacters(in: CharacterSet(charactersIn: "'\" "))
        while let last = token.last, ":,.'\"".contains(last) {
            token.removeLast()
        }
        return token
    }
}

public struct ToolExpansionState: Sendable, Equatable {
    private var groups: Set<String> = []
    private var cards: Set<String> = []

    public init() {}

    public func isExpanded(card id: String) -> Bool {
        cards.contains(id)
    }

    public func isExpanded(group ids: [String]) -> Bool {
        ids.contains { groups.contains($0) || cards.contains($0) }
    }

    @discardableResult public mutating func toggle(card id: String) -> Bool {
        cards.remove(id) == nil && cards.insert(id).inserted
    }

    @discardableResult public mutating func toggle(group ids: [String]) -> Bool {
        if isExpanded(group: ids) {
            groups.subtract(ids)
            cards.subtract(ids)
            return false
        }
        groups.formUnion(ids)
        return true
    }

    public mutating func prune(_ transcript: SDUINode) {
        guard let id = transcript.id, let slash = id.lastIndex(of: "/") else { return }
        let scope = id[...slash]
        let present = Set(transcript.children.filter { $0.type == "toolCard" }.compactMap(\.id))
        groups = groups.filter { !$0.hasPrefix(scope) || present.contains($0) }
        cards = cards.filter { !$0.hasPrefix(scope) || present.contains($0) }
    }
}

extension SDUIToolBody {
    @MainActor public var changes: (added: Int, removed: Int)? {
        guard format == .diff else { return nil }
        let kinds = lines.map(\.kind)
        return (kinds.filter { $0 == .added }.count, kinds.filter { $0 == .removed }.count)
    }
}

extension SDUINode {
    public func transcriptItems() -> [TranscriptItem] {
        var items: [TranscriptItem] = []
        var run: [SDUIChild] = []
        func flush() {
            if run.count == 1 {
                items.append(.action(run[0]))
            } else if let first = run.first {
                items.append(.actions(id: first.id + "/actions", cards: run))
            }
            run = []
        }
        for child in identifiedChildren {
            if child.node.type == "toolCard", child.node.string("status") != "failed" {
                run.append(child)
                continue
            }
            flush()
            items.append(.node(child))
        }
        flush()
        return items
    }
}
