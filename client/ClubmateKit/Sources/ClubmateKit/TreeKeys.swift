import Foundation

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
    case expand
    case collapse
    case focusMain
}

public enum TreeKeys {
    public static func action(keyCode: UInt16, control: Bool, command: Bool) -> TreeKeyAction? {
        guard !command else { return nil }
        switch keyCode {
        case 38, 125: return .move(1)
        case 40, 126: return .move(-1)
        case 37: return control ? .focusMain : .expand
        case 124: return .expand
        case 4, 123: return .collapse
        case 36, 31: return .activate
        case 53: return .focusMain
        default: return nil
        }
    }
}
