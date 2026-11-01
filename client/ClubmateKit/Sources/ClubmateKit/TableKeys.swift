public enum TableKeys {
    public static func action(keyCode: UInt16, plain: Bool) -> TreeKeyAction? {
        guard plain else { return nil }
        switch keyCode {
        case 38, 125: return .move(1)
        case 40, 126: return .move(-1)
        case 37, 124: return .expand
        case 4, 123: return .collapse
        default: return nil
        }
    }

    public static func rows(_ rows: [SDUITableRow]) -> [TreeRow] {
        var chain: [SDUITableRow] = []
        var result: [TreeRow] = []
        for row in rows {
            while let last = chain.last, last.depth >= row.depth { chain.removeLast() }
            chain.append(row)
            guard !row.group else { continue }
            let parent = chain.dropLast().last
            result.append(TreeRow(id: row.id, depth: row.depth, parent: parent?.group == false ? parent?.id : nil, hasChildren: row.hasChildren, expanded: row.expanded))
        }
        return result
    }
}
