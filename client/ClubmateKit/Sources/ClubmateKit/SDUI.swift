import Foundation

public struct Workspace: Codable, Sendable, Equatable {
    public struct Folder: Codable, Sendable, Equatable {
        public let path: String
        public let worktree: String?
        public let missing: Bool
        public let addedBy: String?
        public let addedAt: Date?
        public let addedAfk: Bool
        public let seen: Bool

        public init(from decoder: any Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            path = try container.decode(String.self, forKey: .path)
            worktree = try container.decodeIfPresent(String.self, forKey: .worktree)
            missing = try container.decodeIfPresent(Bool.self, forKey: .missing) ?? false
            addedBy = try container.decodeIfPresent(String.self, forKey: .addedBy)
            addedAt = try container.decodeIfPresent(Date.self, forKey: .addedAt)
            addedAfk = try container.decodeIfPresent(Bool.self, forKey: .addedAfk) ?? false
            seen = try container.decodeIfPresent(Bool.self, forKey: .seen) ?? true
        }
    }

    public let id: String
    public let name: String
    public let folders: [Folder]
    public let createdAt: Date
    public let home: Bool
    public let lastOpenedAt: Date?

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        name = try container.decode(String.self, forKey: .name)
        folders = try container.decodeIfPresent([Folder].self, forKey: .folders) ?? []
        createdAt = try container.decode(Date.self, forKey: .createdAt)
        home = try container.decodeIfPresent(Bool.self, forKey: .home) ?? false
        lastOpenedAt = try container.decodeIfPresent(Date.self, forKey: .lastOpenedAt)
    }

    public static func recents(_ workspaces: [Workspace], hiddenBefore cutoff: Date? = nil) -> [Workspace] {
        workspaces
            .filter { workspace in
                guard !workspace.home else { return false }
                guard let cutoff else { return true }
                return workspace.lastOpenedAt.map { $0 > cutoff } ?? false
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
    public let running: Bool

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        title = try container.decode(String.self, forKey: .title)
        plugin = try container.decode(String.self, forKey: .plugin)
        hidden = try container.decode(Bool.self, forKey: .hidden)
        confirm = try container.decodeIfPresent(String.self, forKey: .confirm) ?? ""
        running = try container.decodeIfPresent(Bool.self, forKey: .running) ?? true
    }
}

public struct PingResult: Codable, Sendable, Equatable {
    public let version: String
    public let pid: Int
    public let sessionProtocol: Int?
    public let flavor: String?
}
