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
    public let projectId: String
    public let slot: String
    public let owner: String
    public let rev: Int64
    public let tree: SDUINode
}

public struct SectionInfo: Codable, Sendable, Equatable {
    public let id: String
    public let title: String
    public let icon: String
    public let order: Int
    public let plugin: String
}

public struct Project: Codable, Sendable, Equatable {
    public let id: String
    public let name: String
    public let repos: [String]
    public let createdAt: Date
}

public struct Command: Codable, Sendable, Equatable {
    public let id: String
    public let title: String
    public let plugin: String
}

public struct UIEvent: Codable, Sendable, Equatable {
    public let projectId: String
    public let slot: String
    public let nodeId: String
    public let action: String
    public let value: JSONValue

    public init(projectId: String, slot: String, nodeId: String, action: String, value: JSONValue = .null) {
        self.projectId = projectId
        self.slot = slot
        self.nodeId = nodeId
        self.action = action
        self.value = value
    }
}

public struct PingResult: Codable, Sendable, Equatable {
    public let version: String
    public let pid: Int
}
