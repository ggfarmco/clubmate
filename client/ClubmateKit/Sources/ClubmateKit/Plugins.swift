import Foundation

public struct PluginStatus: Decodable, Sendable, Equatable, Identifiable {
    public let id: String
    public let state: String
    public let pid: Int
    public let restarts: Int
    public let lastError: String
    public let manifest: Manifest

    public struct Manifest: Decodable, Sendable, Equatable {
        public let name: String
        public let version: String
        public let contributes: Contributes
    }

    public struct Contributes: Decodable, Sendable, Equatable {
        public let sections: [Item]
        public let commands: [Item]
    }

    public struct Item: Decodable, Sendable, Equatable {
        public let id: String
        public let title: String
    }

    public var name: String {
        manifest.name.isEmpty ? id : manifest.name
    }

    public var stateLabel: String {
        switch state {
        case "backoff": "crashed, restarting"
        case "failed": "crashed"
        default: state
        }
    }

    public var healthy: Bool {
        state == "running"
    }
}
