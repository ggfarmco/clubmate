public struct TreeRow: Equatable, Sendable {
    public let id: String
    public let depth: Int
    public let parent: String?
    public let hasChildren: Bool
    public let expanded: Bool
    public let opens: Bool
    public let docked: Bool
    public let selects: Bool
    public let heading: Bool

    public init(id: String, depth: Int, parent: String?, hasChildren: Bool, expanded: Bool, opens: Bool = false, docked: Bool = false, selects: Bool = false, heading: Bool = false) {
        self.id = id
        self.depth = depth
        self.parent = parent
        self.hasChildren = hasChildren
        self.expanded = expanded
        self.opens = opens
        self.docked = docked
        self.selects = selects
        self.heading = heading
    }

    var skipped: Bool { heading && expanded }
}

public enum TreeCursor {
    public static func rows(_ items: [SDUITreeItem], dock: [SDUITreeDock] = []) -> [TreeRow] {
        rows(items, depth: 0, parent: nil, top: true) + dock.flatMap { section in
            [TreeRow(id: section.id, depth: 0, parent: nil, hasChildren: true, expanded: section.expanded, docked: true)]
                + (section.expanded ? rows(section.rows, depth: 1, parent: section.id) : [])
        }
    }

    public static func move(_ id: String?, by delta: Int, in rows: [TreeRow]) -> String? {
        guard !rows.isEmpty else { return nil }
        guard let id, let index = rows.firstIndex(where: { $0.id == id }) else { return (rows.first { !$0.skipped } ?? rows[0]).id }
        let step = delta < 0 ? -1 : 1
        var at = index, found = index, left = abs(delta)
        while left > 0 {
            at += step
            guard rows.indices.contains(at) else { break }
            if !rows[at].skipped {
                found = at
                left -= 1
            }
        }
        return rows[found].id
    }

    public static func settle(_ cursor: String?, activated: Bool, from old: [String], to new: [String]) -> String? {
        guard let cursor, let index = old.firstIndex(of: cursor), !new.isEmpty else { return nil }
        let target = new[min(index, new.count - 1)]
        guard let moved = new.firstIndex(of: cursor) else { return target }
        return activated && moved != index ? target : nil
    }

    public static func resolve(_ action: TreeKeyAction, cursor: String?, in rows: [TreeRow]) -> TreeCommand? {
        let index = rows.firstIndex { $0.id == cursor }
        switch action {
        case .move(let delta):
            return move(cursor, by: delta, in: rows).map(TreeCommand.cursor)
        case .focusMain:
            return .focusMain
        case .cycleTabs(let delta):
            return .cycleTabs(delta)
        case .activate:
            return index.map { .select(rows[$0].id) }
        case .open:
            guard let index else { return nil }
            let row = rows[index]
            if row.opens { return .openPage(row.id) }
            guard row.hasChildren, !row.selects else { return .select(row.id) }
            return row.expanded ? .collapse(row.id) : .expand(row.id)
        case .remove:
            return index.map { .remove(rows[$0].id) }
        case .escape:
            guard let index else { return .focusMain }
            let row = rows[index]
            if row.docked { return row.expanded ? .collapse(row.id) : .focusMain }
            if let parent = row.parent, rows.contains(where: { $0.id == parent && $0.docked }) { return .collapse(parent) }
            return .focusMain
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

    private static func rows(_ items: [SDUITreeItem], depth: Int, parent: String?, top: Bool = true) -> [TreeRow] {
        items.flatMap { item in
            let heading = top && item.heading
            let row = TreeRow(id: item.id, depth: depth, parent: parent, hasChildren: item.hasChildren, expanded: item.expanded, opens: item.open != nil, selects: item.mirrorOf != nil, heading: heading)
            return [row] + rows(item.shownChildren, depth: depth + 1, parent: item.id, top: heading)
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
    case escape
    case remove
    case cycleTabs(Int)
}

public enum TreeCommand: Equatable, Sendable {
    case cursor(String)
    case select(String)
    case expand(String)
    case collapse(String)
    case openPage(String)
    case remove(String)
    case focusMain
    case cycleTabs(Int)
}

public enum TabCycle {
    public static func next(_ ids: [String], from selected: String?, by delta: Int) -> String? {
        guard !ids.isEmpty else { return nil }
        guard let selected, let index = ids.firstIndex(of: selected) else { return ids[0] }
        return ids[((index + delta) % ids.count + ids.count) % ids.count]
    }
}

public enum TreeChord: Equatable, Sendable {
    case focusField
    case jump(Int)
    case last
    case needs(Int)
    case tree
    case composer

    public static let leader: UInt16 = 5

    public static let jumpAction = "jump"
    public static let lastAction = "last"
    public static let needsAction = "needs"

    static let digits: [UInt16: Int] = [18: 1, 19: 2, 20: 3, 21: 4, 23: 5, 22: 6, 26: 7, 28: 8, 25: 9]

    public static func passes(command: Bool, control: Bool, option: Bool, shift: Bool, sessions: Bool) -> Bool {
        command || control || option || shift && !sessions
    }

    public static func resolve(_ keyCode: UInt16, shift: Bool = false, sessions: Bool = false) -> TreeChord? {
        if shift { return sessions && keyCode == 45 ? .needs(-1) : nil }
        if keyCode == 8 { return .focusField }
        guard sessions else { return nil }
        if let n = digits[keyCode] { return .jump(n) }
        switch keyCode {
        case 37: return .last
        case 45: return .needs(1)
        case 17: return .tree
        case 34: return .composer
        default: return nil
        }
    }
}

public enum SessionKeys {
    public static func isLast(keyCode: UInt16, control: Bool, command: Bool, option: Bool, shift: Bool) -> Bool {
        keyCode == 48 && control && !command && !option && !shift
    }

    public static func holdsControl(control: Bool, command: Bool, option: Bool, shift: Bool) -> Bool {
        control && !command && !option && !shift
    }
}

public struct ControlHoldKeys: Equatable, Sendable {
    public enum Effect: Equatable, Sendable {
        case land
        case start
        case cancel
    }

    public private(set) var down = false

    public init() {}

    public mutating func flags(control: Bool, command: Bool, option: Bool, shift: Bool) -> [Effect] {
        guard control else {
            down = false
            return [.cancel]
        }
        let effects: [Effect] = down ? [] : [.land]
        down = true
        return effects + [SessionKeys.holdsControl(control: control, command: command, option: option, shift: shift) ? .start : .cancel]
    }
}

public enum TreeKeys {
    public static func action(keyCode: UInt16, control: Bool, command: Bool, side: SidebarSide, shift: Bool = false, option: Bool = false) -> TreeKeyAction? {
        if command { return keyCode == 51 && !control && !shift && !option ? .remove : nil }
        switch keyCode {
        case 43: return shift ? .cycleTabs(-1) : nil
        case 47: return shift ? .cycleTabs(1) : nil
        case 38, 125: return .move(1)
        case 40, 126: return .move(-1)
        case 37: return control ? (side == .left ? .focusMain : nil) : .expand
        case 4: return control ? (side == .right ? .focusMain : nil) : .collapse
        case 124: return .expand
        case 123: return .collapse
        case 36: return .open
        case 31: return .activate
        case 53: return .escape
        case 33: return control ? .escape : nil
        default: return nil
        }
    }
}

public enum MenuEntryKey: Equatable, Sendable {
    case plain(Character)
    case commandDelete

    public init?(_ key: String?) {
        guard let key else { return nil }
        if key == "cmd+delete" {
            self = .commandDelete
        } else if key.count == 1, let first = key.first {
            self = .plain(first)
        } else {
            return nil
        }
    }
}
