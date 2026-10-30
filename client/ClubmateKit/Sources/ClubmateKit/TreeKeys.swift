public struct TreeRow: Equatable, Sendable {
    public let id: String
    public let depth: Int
    public let parent: String?
    public let hasChildren: Bool
    public let expanded: Bool

    public init(id: String, depth: Int, parent: String?, hasChildren: Bool, expanded: Bool) {
        self.id = id
        self.depth = depth
        self.parent = parent
        self.hasChildren = hasChildren
        self.expanded = expanded
    }
}

public enum TreeCursor {
    public static func rows(_ items: [SDUITreeItem]) -> [TreeRow] {
        rows(items, depth: 0, parent: nil)
    }

    public static func move(_ id: String?, by delta: Int, in rows: [TreeRow]) -> String? {
        guard !rows.isEmpty else { return nil }
        guard let id, let index = rows.firstIndex(where: { $0.id == id }) else { return rows[0].id }
        return rows[min(max(index + delta, 0), rows.count - 1)].id
    }

    public static func resolve(_ action: TreeKeyAction, cursor: String?, in rows: [TreeRow]) -> TreeCommand? {
        let index = rows.firstIndex { $0.id == cursor }
        switch action {
        case .move(let delta):
            return move(cursor, by: delta, in: rows).map(TreeCommand.cursor)
        case .focusMain:
            return .focusMain
        case .activate:
            return index.map { .select(rows[$0].id) }
        case .open:
            guard let index else { return nil }
            let row = rows[index]
            guard row.hasChildren else { return .select(row.id) }
            return row.expanded ? .collapse(row.id) : .expand(row.id)
        case .expand:
            guard let index, rows[index].hasChildren else { return nil }
            let row = rows[index]
            guard row.expanded else { return .expand(row.id) }
            return rows.indices.contains(index + 1) && rows[index + 1].parent == row.id ? .cursor(rows[index + 1].id) : nil
        case .collapse:
            guard let index else { return nil }
            let row = rows[index]
            if row.hasChildren, row.expanded { return .collapse(row.id) }
            return row.parent.map(TreeCommand.cursor)
        }
    }

    public static func sticky(_ rows: [TreeRow], top: Int, limit: Int) -> [String] {
        var headers: [Int] = []
        while rows.indices.contains(top + headers.count) {
            let chain = Array(ancestors(of: top + headers.count, in: rows).prefix(limit))
            guard chain.count > headers.count else { break }
            headers = chain
        }
        return headers.enumerated()
            .prefix { contains($0.element, top + $0.offset, in: rows) }
            .map { rows[$0.element].id }
    }

    public static func stickyTarget(_ id: String, in rows: [TreeRow], limit: Int) -> String? {
        guard let index = rows.firstIndex(where: { $0.id == id }) else { return nil }
        return rows[max(0, index - min(rows[index].depth, limit))].id
    }

    public static func scrollTarget(_ id: String, from previous: String?, in rows: [TreeRow], limit: Int) -> String? {
        guard let index = rows.firstIndex(where: { $0.id == id }) else { return nil }
        guard let from = rows.firstIndex(where: { $0.id == previous }), index < from else { return id }
        return stickyTarget(id, in: rows, limit: limit)
    }

    private static func ancestors(of index: Int, in rows: [TreeRow]) -> [Int] {
        var chain: [Int] = []
        var depth = rows[index].depth
        var cursor = index
        while depth > 0, cursor > 0 {
            cursor -= 1
            if rows[cursor].depth < depth {
                chain.append(cursor)
                depth = rows[cursor].depth
            }
        }
        return chain.reversed()
    }

    private static func contains(_ ancestor: Int, _ index: Int, in rows: [TreeRow]) -> Bool {
        guard rows.indices.contains(index), index > ancestor else { return false }
        return rows[(ancestor + 1)...index].allSatisfy { $0.depth > rows[ancestor].depth }
    }

    private static func rows(_ items: [SDUITreeItem], depth: Int, parent: String?) -> [TreeRow] {
        items.flatMap { item in
            let row = TreeRow(id: item.id, depth: depth, parent: parent, hasChildren: item.hasChildren, expanded: item.expanded)
            return [row] + (item.expanded ? rows(item.children, depth: depth + 1, parent: item.id) : [])
        }
    }
}

public enum TreeKeyAction: Equatable, Sendable {
    case move(Int)
    case activate
    case open
    case expand
    case collapse
    case focusMain
}

public enum TreeCommand: Equatable, Sendable {
    case cursor(String)
    case select(String)
    case expand(String)
    case collapse(String)
    case focusMain
}

public enum TreeChord: Equatable, Sendable {
    case focusField

    public static let leader: UInt16 = 5

    public static func resolve(_ keyCode: UInt16) -> TreeChord? {
        keyCode == 8 ? .focusField : nil
    }
}

public enum TreeKeys {
    public static func action(keyCode: UInt16, control: Bool, command: Bool, side: SidebarSide) -> TreeKeyAction? {
        guard !command else { return nil }
        switch keyCode {
        case 38, 125: return .move(1)
        case 40, 126: return .move(-1)
        case 37: return control ? (side == .left ? .focusMain : nil) : .expand
        case 4: return control ? (side == .right ? .focusMain : nil) : .collapse
        case 124: return .expand
        case 123: return .collapse
        case 36: return .open
        case 31: return .activate
        case 53: return .focusMain
        default: return nil
        }
    }
}
