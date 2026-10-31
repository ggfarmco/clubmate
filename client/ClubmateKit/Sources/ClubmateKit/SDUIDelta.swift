import Foundation

public indirect enum SDUIDelta: Decodable, Sendable, Equatable {
    case ref([Int])
    case node(type: String, id: String?, props: [String: JSONValue], children: [SDUIDelta])

    private enum CodingKeys: String, CodingKey {
        case ref, type, id, props, children
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        if let path = try container.decodeIfPresent([Int].self, forKey: .ref) {
            self = .ref(path)
            return
        }
        self = .node(
            type: try container.decode(String.self, forKey: .type),
            id: try container.decodeIfPresent(String.self, forKey: .id),
            props: try container.decodeIfPresent([String: JSONValue].self, forKey: .props) ?? [:],
            children: try container.decodeIfPresent([SDUIDelta].self, forKey: .children) ?? []
        )
    }

    public func resolve(against base: SDUINode) -> SDUINode? {
        switch self {
        case .ref(let path):
            var node = base
            for index in path {
                guard node.children.indices.contains(index) else { return nil }
                node = node.children[index]
            }
            return node
        case .node(let type, let id, let props, let children):
            var resolved: [SDUINode] = []
            resolved.reserveCapacity(children.count)
            for child in children {
                guard let node = child.resolve(against: base) else { return nil }
                resolved.append(node)
            }
            return SDUINode(type: type, id: id, props: props, children: resolved)
        }
    }
}

struct SlotUpdate: Decodable, Sendable {
    let workspaceId: String
    let slot: String
    let owner: String
    let rev: Int64
    let tree: SDUINode?
    let base: Int64?
    let delta: SDUIDelta?

    func view(over current: SlotView?) -> SlotView? {
        if let tree { return SlotView(workspaceId: workspaceId, slot: slot, owner: owner, rev: rev, tree: tree) }
        guard let delta, let current, current.rev == base, let tree = delta.resolve(against: current.tree) else { return nil }
        return SlotView(workspaceId: workspaceId, slot: slot, owner: owner, rev: rev, tree: tree)
    }
}
