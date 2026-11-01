import Foundation

public struct SDUINode: Codable, Sendable, Equatable {
    public var type: String
    public var id: String?
    public var props: [String: JSONValue]
    public var children: [SDUINode]

    public init(type: String, id: String? = nil, props: [String: JSONValue] = [:], children: [SDUINode] = []) {
        self.type = type
        self.id = id
        self.props = props
        self.children = children
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        type = try container.decode(String.self, forKey: .type)
        id = try container.decodeIfPresent(String.self, forKey: .id)
        props = try container.decodeIfPresent([String: JSONValue].self, forKey: .props) ?? [:]
        children = try container.decodeIfPresent([SDUINode].self, forKey: .children) ?? []
    }
}

public struct SlotView: Codable, Sendable, Equatable {
    public let workspaceId: String
    public let slot: String
    public let owner: String
    public let rev: Int64
    public let tree: SDUINode

    public init(workspaceId: String, slot: String, owner: String, rev: Int64, tree: SDUINode) {
        self.workspaceId = workspaceId
        self.slot = slot
        self.owner = owner
        self.rev = rev
        self.tree = tree
    }
}

public struct SectionInfo: Codable, Sendable, Equatable {
    public let id: String
    public let title: String
    public let icon: String
    public let order: Int
    public let scope: String
    public let autoNew: Bool
    public let plugin: String
    public let host: String?
    public let slots: [String]
    public let rail: Bool

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        title = try container.decode(String.self, forKey: .title)
        icon = try container.decode(String.self, forKey: .icon)
        order = try container.decode(Int.self, forKey: .order)
        scope = try container.decode(String.self, forKey: .scope)
        autoNew = try container.decodeIfPresent(Bool.self, forKey: .autoNew) ?? false
        plugin = try container.decode(String.self, forKey: .plugin)
        host = try container.decodeIfPresent(String.self, forKey: .host)
        slots = try container.decodeIfPresent([String].self, forKey: .slots) ?? []
        rail = try container.decodeIfPresent(Bool.self, forKey: .rail) ?? true
    }
}

public struct UIEvent: Codable, Sendable, Equatable {
    public let workspaceId: String
    public let slot: String
    public let nodeId: String
    public let action: String
    public let value: JSONValue

    public init(workspaceId: String, slot: String, nodeId: String, action: String, value: JSONValue = .null) {
        self.workspaceId = workspaceId
        self.slot = slot
        self.nodeId = nodeId
        self.action = action
        self.value = value
    }
}
