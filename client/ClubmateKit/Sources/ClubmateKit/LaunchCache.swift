import Foundation

public enum PaneKind: String, Codable, Sendable {
    case list
    case transcript
    case detail
    case table
    case terminal
    case unknown

    public static func of(_ tree: SDUINode) -> PaneKind {
        var level = [tree]
        for _ in 0..<4 {
            for node in level {
                if let kind = decisive[node.type] { return kind }
            }
            level = level.flatMap { containers.contains($0.type) ? $0.children : [] }
            if level.isEmpty { break }
        }
        return tree.type == "empty" ? .unknown : .detail
    }

    public static func caches(_ slot: String) -> Bool {
        slot.hasSuffix(".main") || slot.hasSuffix(".sidebar")
    }

    private static let decisive: [String: PaneKind] = [
        "list": .list, "tree": .list, "transcript": .transcript, "table": .table, "surface": .terminal, "webview": .terminal,
    ]
    private static let containers: Set<String> = ["stack", "split", "scroll", "tabs", "card"]
}

public struct LaunchSection: Codable, Equatable, Sendable {
    public let id: String
    public let title: String
    public let icon: String

    public init(id: String, title: String, icon: String) {
        self.id = id
        self.title = title
        self.icon = icon
    }
}

public struct LaunchRecord: Codable, Equatable, Sendable {
    public var name: String?
    public var sections: [LaunchSection] = []
    public var selected: String?
    public var kinds: [String: PaneKind] = [:]
    public var sidebarHidden: [String: Bool] = [:]
    public var sidebarWidth: Double?
    public var chatWidth: Double?

    public init() {}

    public func sidebarShown(_ section: String) -> Bool {
        section != CodeSection.id && !(sidebarHidden[section] ?? false)
    }

    public func chatShown(in section: String) -> Bool {
        section == CodeSection.id && kinds[CodeSection.chatSlot] != nil && sidebarHidden[CodeSection.id] == false
    }

    public func kind(_ slot: String) -> PaneKind {
        kinds[slot] ?? .unknown
    }
}

@MainActor
public final class LaunchCache {
    private struct File: Codable {
        var version: String
        var plugins: [String] = []
        var recent = 0
        var workspaces: [String: LaunchRecord] = [:]
    }

    private let url: URL
    private var file: File

    public init(url: URL, version: String) {
        self.url = url
        if let data = try? Data(contentsOf: url), let saved = try? JSONDecoder().decode(File.self, from: data), saved.version == version {
            file = saved
        } else {
            file = File(version: version)
        }
    }

    public func record(_ workspace: String) -> LaunchRecord? {
        file.workspaces[workspace]
    }

    public var recentCount: Int {
        get { file.recent }
        set {
            guard file.recent != newValue else { return }
            file.recent = newValue
            save()
        }
    }

    public func plugins(_ plugins: Set<String>) {
        let sorted = plugins.sorted()
        guard file.plugins != sorted else { return }
        if !file.plugins.isEmpty { file.workspaces = [:] }
        file.plugins = sorted
        save()
    }

    public func update(_ workspace: String, _ change: (inout LaunchRecord) -> Void) {
        var record = file.workspaces[workspace] ?? LaunchRecord()
        change(&record)
        guard file.workspaces[workspace] != record else { return }
        file.workspaces[workspace] = record
        save()
    }

    private func save() {
        guard let data = try? JSONEncoder().encode(file) else { return }
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? data.write(to: url, options: .atomic)
    }
}
