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
    public let scope: String
    public let autoNew: Bool
    public let plugin: String

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        title = try container.decode(String.self, forKey: .title)
        icon = try container.decode(String.self, forKey: .icon)
        order = try container.decode(Int.self, forKey: .order)
        scope = try container.decode(String.self, forKey: .scope)
        autoNew = try container.decodeIfPresent(Bool.self, forKey: .autoNew) ?? false
        plugin = try container.decode(String.self, forKey: .plugin)
    }
}

public struct Project: Codable, Sendable, Equatable {
    public let id: String
    public let name: String
    public let repos: [String]
    public let roots: [String]
    public let missing: [String]
    public let createdAt: Date
    public let home: Bool
    public let lastOpenedAt: Date?

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        name = try container.decode(String.self, forKey: .name)
        repos = try container.decode([String].self, forKey: .repos)
        roots = try container.decodeIfPresent([String].self, forKey: .roots) ?? repos
        missing = try container.decodeIfPresent([String].self, forKey: .missing) ?? []
        createdAt = try container.decode(Date.self, forKey: .createdAt)
        home = try container.decodeIfPresent(Bool.self, forKey: .home) ?? false
        lastOpenedAt = try container.decodeIfPresent(Date.self, forKey: .lastOpenedAt)
    }

    public static func recents(_ projects: [Project], hiddenBefore cutoff: Date? = nil) -> [Project] {
        projects
            .filter { project in
                guard !project.home else { return false }
                guard let cutoff else { return true }
                return project.lastOpenedAt.map { $0 > cutoff } ?? false
            }
            .sorted { ($0.lastOpenedAt ?? .distantPast, $0.createdAt) > ($1.lastOpenedAt ?? .distantPast, $1.createdAt) }
    }
}

public struct Command: Codable, Sendable, Equatable {
    public let id: String
    public let title: String
    public let plugin: String
    public let hidden: Bool
    public let confirm: String

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        title = try container.decode(String.self, forKey: .title)
        plugin = try container.decode(String.self, forKey: .plugin)
        hidden = try container.decode(Bool.self, forKey: .hidden)
        confirm = try container.decodeIfPresent(String.self, forKey: .confirm) ?? ""
    }
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
    public let sessionProtocol: Int?
}
