public struct TreeHeld: Equatable, Sendable {
    public let node: SDUINode
    public let pending: Int
}

public enum TreeHoldMerge {
    private static let structure = ["children", "expanded", "hasChildren", "pinned"]

    public static func merge(shown: SDUINode, incoming: SDUINode) -> TreeHeld {
        var fresh: [String: [String: JSONValue]] = [:]
        index(incoming.props["items"]?.arrayValue ?? [], into: &fresh)
        let kept = (shown.props["items"]?.arrayValue ?? []).map { keep($0, fresh: fresh) }
        var node = incoming
        node.props["items"] = .array(kept)
        return TreeHeld(node: node, pending: pending(from: node, to: incoming))
    }

    public static func shows(_ id: String, in node: SDUINode) -> Bool {
        (node.treeItems() + node.treeDock().filter(\.expanded).flatMap(\.rows)).flatMap(rows).contains { $0.id == id || $0.mirrorOf == id }
    }

    private static func index(_ items: [JSONValue], into out: inout [String: [String: JSONValue]]) {
        for item in items {
            guard let fields = item.objectValue, let id = fields["id"]?.stringValue else { continue }
            out[id] = fields
            index(fields["children"]?.arrayValue ?? [], into: &out)
        }
    }

    private static func keep(_ item: JSONValue, fresh: [String: [String: JSONValue]]) -> JSONValue {
        guard let old = item.objectValue, let id = old["id"]?.stringValue else { return item }
        var out = fresh[id] ?? old
        for key in structure { out[key] = old[key] }
        if old["action"]?.boolValue == true, old["hasChildren"]?.boolValue == true { out["detail"] = old["detail"] }
        if let children = old["children"]?.arrayValue { out["children"] = .array(children.map { keep($0, fresh: fresh) }) }
        return .object(out)
    }

    private static func rows(_ item: SDUITreeItem) -> [SDUITreeItem] {
        [item] + item.shownChildren.flatMap(rows)
    }

    private static func pending(from shown: SDUINode, to incoming: SDUINode) -> Int {
        let before = shown.treeItems().flatMap(rows).filter { !TreeHoldMerge.group($0) }.map(\.id)
        let after = incoming.treeItems().flatMap(rows).filter { !TreeHoldMerge.group($0) }.map(\.id)
        let changed = Set(after).symmetricDifference(before).count
        return changed == 0 && before != after ? 1 : changed
    }

    private static func group(_ item: SDUITreeItem) -> Bool {
        item.action && item.hasChildren
    }
}

public struct TreeGate: Sendable, Equatable {
    public private(set) var holding = false
    public private(set) var base: SDUINode?
    public private(set) var pending = 0
    private var latest: SDUINode?

    public init() {}

    public func display(_ node: SDUINode) -> SDUINode {
        guard holding, let base, Self.same(base, node) else { return node }
        return TreeHoldMerge.merge(shown: base, incoming: node).node
    }

    public mutating func receive(_ node: SDUINode) -> Bool {
        let before = latest?.string("selected")
        latest = node
        guard holding, let base, Self.same(base, node) else {
            self.base = node
            pending = 0
            return false
        }
        let held = TreeHoldMerge.merge(shown: base, incoming: node)
        if let selected = node.string("selected"), !selected.isEmpty, selected != before, !TreeHoldMerge.shows(selected, in: held.node) {
            return true
        }
        pending = held.pending
        return false
    }

    public mutating func arm() {
        base = latest
        pending = 0
        holding = true
    }

    public mutating func release() {
        holding = false
        base = latest
        pending = 0
    }

    private static func same(_ base: SDUINode, _ node: SDUINode) -> Bool {
        guard let key = base.string("hold"), !key.isEmpty else { return false }
        return base.id == node.id && node.string("hold") == key
    }

    public mutating func reset() {
        latest = nil
        base = nil
        pending = 0
    }
}
