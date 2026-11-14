import Foundation

public enum TaskCommand {
    public static let connectAnswer = "agents.connect-answer"
    public static let landAnswer = "agents.land-answer"
    public static let landCommits = "agents.land-commits"
    public static let repoFix = "agents.repo-fix"
    public static let repoLeave = "agents.repo-leave"
    public static let archivePlan = "agents.archive-plan"
    public static let archive = "agents.archive"
    public static let folderRemove = "agents.folder-remove"
}

public enum RequestCard: Sendable, Equatable {
    case connect(ConnectCard)
    case land(LandCard)

    public var request: String {
        switch self {
        case .connect(let card): card.request
        case .land(let card): card.request
        }
    }

    init?(node: JSONValue) {
        guard let props = node["props"] else { return nil }
        switch node["type"]?.stringValue {
        case "request.connectRepo":
            guard let card = try? props.decode(ConnectCard.self) else { return nil }
            self = .connect(card)
        case "request.land":
            guard let card = try? props.decode(LandCard.self) else { return nil }
            self = .land(card)
        default:
            return nil
        }
    }
}

public struct ConnectCard: Decodable, Sendable, Equatable {
    public struct Repo: Decodable, Sendable, Equatable, Identifiable {
        public let key: String
        public let name: String
        public var path: String?
        public var remote: String?
        public var state: String?
        public var warn: Bool?
        public var clone: Bool?
        public var reason: String?
        public var landsBy: String?
        public var source: String?
        public var brings: String?
        public var trust: String?
        public var folded: Bool?

        public var id: String { key }

        public var facts: String {
            [warn == true ? remote : nil, warn == true ? nil : remote, warn == true ? nil : state].compactMap { $0 }.joined(separator: " · ")
        }
    }

    public struct Effect: Decodable, Sendable, Equatable, Identifiable {
        public let text: String
        public var note: String?

        public var id: String { text }
    }

    public let request: String
    public let title: String
    public var caption: String?
    public let connect: String
    public var effect: String?
    public var effects: [Effect]?
    public var hint: String?
    public var declined: String?
    public var more: String?
    public var seen: String?
    public let repos: [Repo]

    public static let footnote = "No time limit · another folder: on the Mac"

    public var single: Bool { repos.count == 1 }

    public func visible(_ draft: ConnectDraft) -> [Repo] {
        draft.unfolded ? repos : repos.filter { $0.folded != true }
    }

    public func answers(_ draft: ConnectDraft) -> [ConnectAnswer] {
        repos.map { ConnectAnswer(path: $0.key, action: draft.unticked.contains($0.key) ? .skip : .connect) }
    }

    public var declineAll: [ConnectAnswer] {
        repos.map { ConnectAnswer(path: $0.key, action: .skip) }
    }

    public var subject: String {
        single ? repos[0].name : "\(repos.count) repos"
    }
}

public struct ConnectDraft: Sendable, Equatable {
    public var unticked: Set<String> = []
    public var unfolded = false

    public init() {}

    public mutating func toggle(_ repo: ConnectCard.Repo) {
        if unticked.remove(repo.key) == nil { unticked.insert(repo.key) }
    }

    public func connects(_ card: ConnectCard) -> Bool {
        card.repos.contains { !unticked.contains($0.key) }
    }
}

public struct ConnectAnswer: Codable, Sendable, Equatable {
    public enum Action: String, Codable, Sendable {
        case connect
        case skip
    }

    public let path: String
    public let action: Action

    public init(path: String, action: Action) {
        self.path = path
        self.action = action
    }

    var json: JSONValue {
        .object(["path": .string(path), "action": .string(action.rawValue)])
    }
}

public struct LandCard: Decodable, Sendable, Equatable, Identifiable {
    public struct Option: Decodable, Sendable, Equatable, Identifiable {
        public let id: String
        public let label: String
    }

    public static let merge = "merge"
    public static let leave = "leave"

    public let request: String
    public let title: String
    public var caption: String?
    public let name: String
    public var detail: String?
    public var rule: String?
    public var confirm: String?
    public let options: [Option]

    public var id: String { request }

    public var mergeLabel: String {
        options.first { $0.id == Self.merge }?.label ?? "Merge"
    }

    public var leaveLabel: String? {
        options.first { $0.id == Self.leave }?.label
    }

    public var reviewLabel: String {
        "Review and merge \(name)…"
    }

    public func canMerge(commits: LandCommits?, blocked: String?) -> Bool {
        commits != nil && blocked == nil
    }
}

public struct LandCommits: Decodable, Sendable, Equatable {
    public struct Commit: Decodable, Sendable, Equatable, Identifiable {
        public let sha: String
        public let title: String

        public var id: String { sha }
    }

    public let commits: [Commit]
    public var more: Int?
    public var tip: String?
    public var detail: String?
}

public struct ReposCard: Sendable, Equatable {
    public struct Segment: Decodable, Sendable, Equatable {
        public let text: String
        public var tone: String?
    }

    public struct Action: Sendable, Equatable, Identifiable {
        public enum Kind: String, Sendable {
            case fix = "repoFix"
            case session = "repoSession"
            case leave = "repoLeave"
        }

        public let kind: Kind
        public let label: String
        public var confirm: String?

        public var id: String { kind.rawValue }
    }

    public struct Row: Decodable, Sendable, Equatable, Identifiable {
        public let id: String
        public let order: Int
        public let name: String
        public var path: String?
        public var session: String?
        public var landsBy: String?
        public let state: String
        public var tone: String?
        public var opens: Bool?
        public var open: Bool?
        public var tail: String?
        public var note: String?
        public var actions: String?
        public var foldKey: String?
        public var buttons: [Action] = []

        enum CodingKeys: String, CodingKey {
            case id, order, name, path, session, landsBy, state, tone, opens, open, tail, note, actions, foldKey
        }
    }

    public let title: String
    public let summary: [Segment]
    public let footer: String?
    public let archive: String?
    public let collapsed: Bool
    public let foldKey: String
    public let rows: [Row]

    public var ordered: Bool {
        Set(rows.map(\.order)).count > 1
    }

    public func row(named name: String) -> Row? {
        rows.first { $0.name == name }
    }

    init?(node: JSONValue) {
        guard node["type"]?.stringValue == "repos", let props = node["props"],
              let title = props["title"]?.stringValue, var rows = try? props["rows"]?.decode([Row].self) else { return nil }
        let bars = node["children"]?.arrayValue ?? []
        func buttons(_ id: String?) -> [JSONValue] {
            bars.first { $0["id"]?.stringValue == id && id != nil }?["children"]?.arrayValue ?? []
        }
        for index in rows.indices {
            rows[index].buttons = buttons(rows[index].actions).compactMap { button in
                guard let action = button["props"]?["action"]?.stringValue.flatMap(Action.Kind.init), let label = button["props"]?["label"]?.stringValue else { return nil }
                return Action(kind: action, label: label, confirm: button["props"]?["confirm"]?.stringValue)
            }
        }
        self.title = title
        self.rows = rows
        summary = (try? props["summary"]?.decode([Segment].self)) ?? []
        footer = props["footer"]?.stringValue
        archive = buttons(props["footerAction"]?.stringValue).first?["props"]?["label"]?.stringValue
        collapsed = props["collapsed"]?.boolValue ?? false
        foldKey = props["foldKey"]?.stringValue ?? ""
    }
}

public struct ReposFold: Sendable, Equatable {
    private var rows: [String: (open: Bool, key: String?)] = [:]
    private var card: (collapsed: Bool, key: String)?

    public init() {}

    public func collapsed(_ repos: ReposCard) -> Bool {
        if let card, card.key == repos.foldKey { return card.collapsed }
        return repos.collapsed
    }

    public mutating func toggle(_ repos: ReposCard) {
        card = (!collapsed(repos), repos.foldKey)
    }

    public func open(_ row: ReposCard.Row) -> Bool {
        guard row.opens == true else { return false }
        if let choice = rows[row.id], choice.key == row.foldKey { return choice.open }
        return row.open == true
    }

    public mutating func toggle(_ row: ReposCard.Row) {
        rows[row.id] = (!open(row), row.foldKey)
    }

    public static func == (a: ReposFold, b: ReposFold) -> Bool {
        a.rows.mapValues { "\($0.open)\($0.key)" } == b.rows.mapValues { "\($0.open)\($0.key)" } && a.card?.collapsed == b.card?.collapsed && a.card?.key == b.card?.key
    }
}

public struct ArchiveChoice: Codable, Sendable, Equatable {
    public let path: String
    public let seen: String

    var json: JSONValue {
        .object(["path": .string(path), "seen": .string(seen)])
    }
}

public struct ArchivePlan: Decodable, Sendable, Equatable {
    public struct Step: Decodable, Sendable, Equatable {
        public let action: String
    }

    public struct State: Decodable, Sendable, Equatable {
        public var path: String?
    }

    public struct Repo: Decodable, Sendable, Equatable, Identifiable {
        public let name: String
        public let path: String
        public let worktree: Step
        public let summary: String
        public var why: String?
        public var whyTone: String?
        public var seen: String?
        public var state: State?

        public var id: String { path }
    }

    public static let removes = "remove"

    public let session: String
    public let title: String
    public let workspace: String
    public let sessions: Int
    public let repos: [Repo]
    public var held: String?
    public var cronLines: [String]?
    public var cronWarn: String?
    public var seen: String?

    public var choices: [ArchiveChoice] {
        repos.compactMap { repo in
            guard repo.worktree.action == Self.removes, let path = repo.state?.path, let seen = repo.seen else { return nil }
            return ArchiveChoice(path: path, seen: seen)
        }
    }

    public var lead: String {
        "Cleans up what it left in each repo. Its \(sessions) \(sessions == 1 ? "session moves" : "sessions move") to Closed."
    }

    public func rule(mac: String) -> String {
        "Unpushed commits, uncommitted files, remote branches and transcripts are never deleted. Folders stay in \(workspace). Runs on \(mac); if it is offline, it waits and runs when the Mac is back."
    }
}

public enum ArchiveMode: Sendable {
    case archive
    case closeOnly
}

public enum Delivery: Sendable, Equatable {
    case sent
    case queued
}

public struct OfflineRefused: Error, Equatable, LocalizedError {
    public let action: String

    public init(_ action: String = "Merge") {
        self.action = action
    }

    public var errorDescription: String? {
        "\(action) needs the Mac online"
    }
}

public enum PlainMarkdown {
    public static func attributed(_ text: String) -> AttributedString {
        var out = (try? AttributedString(markdown: text, options: AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(text)
        for run in out.runs where run.link != nil {
            out[run.range].link = nil
        }
        return out
    }
}
