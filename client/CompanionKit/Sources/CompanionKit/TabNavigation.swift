import Foundation

public enum CompanionTab: String, Codable, Sendable, CaseIterable {
    case agents
    case backlog
    case wiki
    case more

    public init(from decoder: any Decoder) throws {
        self = Self(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .agents
    }

    public static func shown(hasWiki: Bool) -> [CompanionTab] {
        allCases.filter { $0 != .wiki || hasWiki }
    }

    public var title: String {
        switch self {
        case .agents: "Agents"
        case .backlog: "Backlog"
        case .wiki: "Wiki"
        case .more: "More"
        }
    }
}

public enum Destination: Hashable, Codable, Sendable {
    case session(String)
    case run(String)
    case task(Int)
    case wikiPage(id: String, path: String, title: String)
    case doc(path: String, anchor: String?)
    case inbox
    case section(id: String, item: String?)
    case macDetails(computer: String)

    public var tab: CompanionTab? {
        switch self {
        case .session, .run: .agents
        case .task: .backlog
        case .wikiPage: .wiki
        case .section, .macDetails: .more
        case .doc, .inbox: nil
        }
    }

    public func sectionLine(workspace: String) -> String? {
        switch self {
        case .doc(let path, _): "File · " + path
        case .session, .run, .task, .wikiPage: tab.map { $0.title + " · " + workspace }
        default: nil
        }
    }
}

public struct Origin: Hashable, Codable, Sendable {
    public enum Kind: String, Codable, Sendable {
        case page
        case inbox
        case notification
    }

    public let tab: CompanionTab?
    public let title: String
    public let kind: Kind

    public init(tab: CompanionTab?, title: String, kind: Kind = .page) {
        self.tab = tab
        self.title = title
        self.kind = kind
    }
}

public struct StackEntry: Hashable, Codable, Sendable {
    public let destination: Destination
    public let origin: Origin?
    public let workspace: WorkspaceKey?

    public init(_ destination: Destination, origin: Origin? = nil, workspace: WorkspaceKey? = nil) {
        self.destination = destination
        self.origin = origin
        self.workspace = workspace
    }
}

public enum BackControl: Hashable, Sendable {
    case chevron
    case chip(String)

    public static let maxLabel = 12

    public init(_ entry: StackEntry) {
        guard let origin = entry.origin, origin.kind != .page || origin.tab != entry.destination.tab else {
            self = .chevron
            return
        }
        self = .chip(origin.title.count > Self.maxLabel ? String(origin.title.prefix(Self.maxLabel - 1)) + "…" : origin.title)
    }
}

public struct WorkspaceKey: Hashable, Codable, Sendable {
    public let computer: String
    public let workspace: String

    public init(computer: String, workspace: String) {
        self.computer = computer
        self.workspace = workspace
    }
}

public struct StackKey: Hashable, Codable, Sendable {
    public let computer: String
    public let workspace: String
    public let tab: CompanionTab

    public init(computer: String, workspace: String, tab: CompanionTab) {
        self.computer = computer
        self.workspace = workspace
        self.tab = tab
    }

    public init(_ workspace: WorkspaceKey, tab: CompanionTab) {
        self.init(computer: workspace.computer, workspace: workspace.workspace, tab: tab)
    }

    public var workspaceKey: WorkspaceKey {
        WorkspaceKey(computer: computer, workspace: workspace)
    }
}

public struct TabStacks: Equatable, Sendable {
    public static let version = 1
    public static let defaultsKey = "tabStacks"

    public private(set) var workspace: WorkspaceKey?
    private var tabs: [WorkspaceKey: CompanionTab] = [:]
    private var stacks: [StackKey: [StackEntry]] = [:]
    private var columns: [CompanionTab: ColumnStep] = [:]

    public init() {}

    public var tab: CompanionTab {
        get { workspace.flatMap { tabs[$0] } ?? .agents }
        set {
            guard let workspace else { return }
            tabs[workspace] = newValue
        }
    }

    public var current: StackKey? {
        workspace.map { StackKey($0, tab: tab) }
    }

    public var path: [StackEntry] {
        current.map(path(for:)) ?? []
    }

    public mutating func select(computer: String, workspace: String) {
        self.workspace = WorkspaceKey(computer: computer, workspace: workspace)
    }

    public func path(for key: StackKey) -> [StackEntry] {
        stacks[key] ?? []
    }

    public mutating func setPath(_ entries: [StackEntry], for key: StackKey) {
        stacks[key] = entries.isEmpty ? nil : entries
    }

    public mutating func push(_ destination: Destination, from origin: Origin? = nil, in workspace: WorkspaceKey? = nil) {
        guard let current else { return }
        setPath(path(for: current) + [StackEntry(destination, origin: origin, workspace: own(workspace))], for: current)
    }

    public mutating func pop() {
        guard let current else { return }
        setPath(path(for: current).dropLast(), for: current)
    }

    public mutating func popToRoot() {
        guard let current else { return }
        setPath([], for: current)
    }

    public mutating func replaceTop(_ destination: Destination, in workspace: WorkspaceKey? = nil) {
        guard let current, let top = path(for: current).last else { return }
        setPath(path(for: current).dropLast() + [StackEntry(destination, origin: top.origin, workspace: own(workspace))], for: current)
    }

    private func own(_ workspace: WorkspaceKey?) -> WorkspaceKey? {
        workspace == self.workspace ? nil : workspace
    }

    public func backControl(at index: Int) -> BackControl? {
        guard let current, path(for: current).indices.contains(index) else { return nil }
        return BackControl(path(for: current)[index])
    }

    public func sectionLine(at index: Int, workspace name: String) -> String? {
        guard let current, path(for: current).indices.contains(index) else { return nil }
        let destination = path(for: current)[index].destination
        guard destination.tab != current.tab else { return nil }
        return destination.sectionLine(workspace: name)
    }

    public mutating func drop(where gone: (WorkspaceKey, Destination) -> Bool) {
        for (key, entries) in stacks {
            setPath(Array(entries.prefix { !gone($0.workspace ?? key.workspaceKey, $0.destination) }), for: key)
        }
    }

    public func column(for tab: CompanionTab) -> ColumnStep {
        columns[tab] ?? .listDetail
    }

    public mutating func toggleColumns(for tab: CompanionTab) {
        columns[tab] = column(for: tab).toggled
    }

    public func save(to defaults: UserDefaults, key: String = defaultsKey) {
        guard let data = try? JSONEncoder().encode(Saved(self)) else { return }
        defaults.set(data, forKey: key)
    }

    public static func restore(from defaults: UserDefaults, key: String = defaultsKey, gone: (WorkspaceKey, Destination) -> Bool = { _, _ in false }) -> TabStacks {
        guard let data = defaults.data(forKey: key), let saved = try? JSONDecoder().decode(Saved.self, from: data) else { return TabStacks() }
        var stacks = saved.restored
        stacks.drop(where: gone)
        return stacks
    }
}

extension TabStacks {
    private struct Saved: Codable {
        struct Tab: Codable {
            let workspace: WorkspaceKey
            let tab: CompanionTab
        }

        struct Stack: Codable {
            enum CodingKeys: String, CodingKey {
                case key
                case entries
            }

            enum KeyKeys: String, CodingKey {
                case tab
            }

            let key: StackKey
            let entries: [Lossy<StackEntry>]
            var known = true

            init(key: StackKey, entries: [Lossy<StackEntry>]) {
                self.key = key
                self.entries = entries
            }

            init(from decoder: any Decoder) throws {
                let container = try decoder.container(keyedBy: CodingKeys.self)
                key = try container.decode(StackKey.self, forKey: .key)
                entries = try container.decode([Lossy<StackEntry>].self, forKey: .entries)
                let raw = try container.nestedContainer(keyedBy: KeyKeys.self, forKey: .key).decode(String.self, forKey: .tab)
                known = CompanionTab(rawValue: raw) != nil
            }

            func encode(to encoder: any Encoder) throws {
                var container = encoder.container(keyedBy: CodingKeys.self)
                try container.encode(key, forKey: .key)
                try container.encode(entries, forKey: .entries)
            }
        }

        struct Column: Codable {
            let tab: CompanionTab
            let step: ColumnStep
        }

        let version: Int
        let workspace: WorkspaceKey?
        let tabs: [Tab]
        let stacks: [Stack]
        let columns: [Column]

        init(_ stacks: TabStacks) {
            version = TabStacks.version
            workspace = stacks.workspace
            tabs = stacks.tabs.map { Tab(workspace: $0.key, tab: $0.value) }
            self.stacks = stacks.stacks.map { Stack(key: $0.key, entries: $0.value.map { Lossy(value: $0) }) }
            columns = stacks.columns.map { Column(tab: $0.key, step: $0.value) }
        }

        init(from decoder: any Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            version = try container.decode(Int.self, forKey: .version)
            guard version == TabStacks.version else {
                workspace = nil
                tabs = []
                stacks = []
                columns = []
                return
            }
            workspace = try container.decodeIfPresent(WorkspaceKey.self, forKey: .workspace)
            tabs = try container.decode([Tab].self, forKey: .tabs)
            stacks = try container.decode([Stack].self, forKey: .stacks)
            columns = try container.decode([Column].self, forKey: .columns)
        }

        var restored: TabStacks {
            var result = TabStacks()
            result.workspace = workspace
            result.tabs = Dictionary(tabs.map { ($0.workspace, $0.tab) }, uniquingKeysWith: { first, _ in first })
            for stack in stacks.filter(\.known) + stacks.filter({ !$0.known }) where result.stacks[stack.key] == nil {
                result.setPath(stack.entries.compactMap(\.value), for: stack.key)
            }
            result.columns = Dictionary(columns.map { ($0.tab, $0.step) }, uniquingKeysWith: { first, _ in first })
            return result
        }
    }

    private struct Lossy<Value: Codable>: Codable {
        let value: Value?

        init(value: Value) {
            self.value = value
        }

        init(from decoder: any Decoder) throws {
            value = try? Value(from: decoder)
        }

        func encode(to encoder: any Encoder) throws {
            try value.encode(to: encoder)
        }
    }
}
