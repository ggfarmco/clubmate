import Foundation

public struct LandQueueAction: Codable, Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let enabled: Bool
    public let to: Int?
    public let confirm: String?
    public let divider: Bool?

    public var command: String? {
        switch id {
        case "up", "down": "agents.land-queue-move"
        case "skip": "agents.land-queue-skip"
        case "remove": "agents.land-queue-remove"
        default: nil
        }
    }
}

public struct LandQueueMark: Codable, Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let help: String
}

public struct LandQueueFiles: Codable, Sendable, Equatable, Identifiable {
    public let title: String
    public let shared: Bool?
    public let files: [String]

    public var id: String { title }
}

public struct LandQueueEntry: Codable, Sendable, Equatable, Identifiable {
    public let id: String
    public let session: String?
    public let title: String
    public let mode: String
    public let word: String
    public let glyph: String
    public let tone: String?
    public let line: String
    public let why: String?
    public let marks: [LandQueueMark]?
    public let needsYou: Bool
    public let actions: [LandQueueAction]
    public let fileGroups: [LandQueueFiles]
    public let more: Int?

    public var subtitle: String {
        ((marks ?? []).map(\.label) + [why].compactMap { $0 }).joined(separator: " · ")
    }

    public func action(_ id: String) -> LandQueueAction? {
        actions.first { $0.id == id }
    }
}

public struct LandQueue: Codable, Sendable, Equatable, Identifiable {
    public let id: String
    public let into: String
    public let title: String
    public let glyph: String
    public let detail: String
    public let detailTone: String?
    public let grouped: Bool
    public let owner: String?
    public let help: String?
    public let needsYou: Bool
    public let entries: [LandQueueEntry]
}

extension [LandQueue] {
    public var inLine: Set<String> {
        Set(filter(\.grouped).flatMap { $0.entries.filter { $0.mode != "dirty" }.compactMap(\.session) })
    }
}
