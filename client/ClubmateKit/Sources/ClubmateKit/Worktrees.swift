import Foundation
import Observation

public struct Worktree: Equatable, Sendable, Identifiable {
    public let repo: String
    public let path: String
    public let branch: String
    public let head: String
    public let main: Bool
    public let current: Bool

    public var id: String { "\(repo)\u{0}\(path)" }

    public var name: String { branch.isEmpty ? head : branch }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let repo = fields["repo"]?.stringValue, let path = fields["path"]?.stringValue else { return nil }
        self.repo = repo
        self.path = path
        branch = fields["branch"]?.stringValue ?? ""
        head = fields["head"]?.stringValue ?? ""
        main = fields["main"]?.boolValue ?? false
        current = fields["current"]?.boolValue ?? false
    }
}

public struct Branch: Equatable, Sendable, Identifiable {
    public let repo: String
    public let name: String
    public let remote: Bool

    public var id: String { "branch:\(repo)\u{0}\(name)" }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let repo = fields["repo"]?.stringValue, let name = fields["name"]?.stringValue else { return nil }
        self.repo = repo
        self.name = name
        remote = fields["remote"]?.boolValue ?? false
    }
}

public struct WorktreeGroup: Equatable, Sendable, Identifiable {
    public let repo: String
    public let trees: [Worktree]

    public var id: String { repo }

    public var name: String { URL(fileURLWithPath: repo).lastPathComponent }
}

public struct WorktreePrompt: Equatable, Sendable {
    public let title: String
    public let detail: String
    public let command: String
    public let args: JSONValue

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let title = fields["title"]?.stringValue, let command = fields["command"]?.stringValue else { return nil }
        self.title = title
        self.command = command
        detail = fields["detail"]?.stringValue ?? ""
        args = fields["args"] ?? .object([:])
    }
}

@MainActor
@Observable
public final class WorktreeModel {
    public static let listCommand = "git.worktrees"
    public static let switchCommand = "git.worktree-switch"
    public static let newCommand = "git.worktree-new"
    public static let removeCommand = "git.worktree-remove"
    public static let removePromptCommand = "git.worktree-remove-prompt"
    public static let branchesCommand = "git.branches"
    public static let branchSwitchCommand = "git.branch-switch"
    public static let changedTopic = "git.changed"
    public static let outcomeTopic = "git.outcome"
    public static let worktreesSection = "Worktrees"
    public static let branchesSection = "Branches"
    static let topics = "git.*"
    static let source = "plugin:git"
    static let maxBranchRows = 200
    public static let defaultRetryDelays: [Duration] = [.milliseconds(250), .milliseconds(500), .seconds(1), .seconds(2), .seconds(4)]

    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public private(set) var worktrees: [Worktree] = []
    public private(set) var branches: [Branch] = []

    @ObservationIgnored private let run: Run
    @ObservationIgnored private let confirm: @MainActor (String, String) async -> Bool
    @ObservationIgnored private let report: @MainActor (String) -> Void
    @ObservationIgnored private let retryDelays: [Duration]
    @ObservationIgnored private var generation = 0

    public init(
        run: @escaping Run,
        confirm: @escaping @MainActor (String, String) async -> Bool,
        report: @escaping @MainActor (String) -> Void,
        retryDelays: [Duration] = WorktreeModel.defaultRetryDelays
    ) {
        self.run = run
        self.confirm = confirm
        self.report = report
        self.retryDelays = retryDelays
    }

    public var groups: [WorktreeGroup] {
        var order: [String] = []
        var trees: [String: [Worktree]] = [:]
        for tree in worktrees {
            if trees[tree.repo] == nil { order.append(tree.repo) }
            trees[tree.repo, default: []].append(tree)
        }
        return order.map { WorktreeGroup(repo: $0, trees: trees[$0] ?? []) }
    }

    public var linked: [Worktree] {
        worktrees.filter { !$0.main }
    }

    public func label(_ tree: Worktree) -> String {
        let groups = self.groups
        guard groups.count > 1, let group = groups.first(where: { $0.repo == tree.repo }) else { return tree.name }
        return "\(group.name) ▸ \(tree.name)"
    }

    public var current: Worktree? {
        guard let first = groups.first else { return nil }
        return first.trees.first { $0.current } ?? first.trees.first { $0.main }
    }

    public func switcherItems(_ query: String) -> [PickerItem] {
        let trees = worktrees.map { PickerItem(id: $0.id, label: label($0), checked: $0.current, section: Self.worktreesSection) }
        let multi = groups.count > 1
        let names = branches.map { branch in
            let name = multi ? "\(URL(fileURLWithPath: branch.repo).lastPathComponent) ▸ \(branch.name)" : branch.name
            return PickerItem(id: branch.id, label: name, detail: branch.remote ? "remote" : "", section: Self.branchesSection)
        }
        return FuzzyRank.rank(trees, query: query, text: \.label) + FuzzyRank.rank(names, query: query, text: \.label).prefix(Self.maxBranchRows)
    }

    public var switcherSource: FuzzyPickerModel.Source {
        { [weak self] query in .success(self?.switcherItems(query) ?? []) }
    }

    public func pick(_ item: PickerItem) async {
        if let tree = worktree(id: item.id) {
            return await switchTo(repo: tree.repo, path: tree.path)
        }
        guard let branch = branches.first(where: { $0.id == item.id }) else { return }
        await send(Self.branchSwitchCommand, .object(["repo": .string(branch.repo), "branch": .string(branch.name)]))
    }

    public func worktree(id: String) -> Worktree? {
        worktrees.first { $0.id == id }
    }

    public func title(_ workspace: String) -> String {
        current.map { "\(workspace) ▸ \($0.name) ▾" } ?? workspace
    }

    public func load(retrying: Bool = false) async {
        generation += 1
        let mine = generation
        var delays = (retrying ? retryDelays : [])[...]
        while !(await fetch()) {
            guard let delay = delays.popFirst() else { return }
            try? await Task.sleep(for: delay)
            guard !Task.isCancelled, mine == generation else { return }
        }
    }

    public func menuOpened() async {
        await load()
        await loadBranches()
    }

    public func loadBranches() async {
        if case .success(let value) = await run(CommandInvocation(id: Self.branchesCommand, args: .object([:]))) {
            branches = (value.arrayValue ?? []).compactMap(Branch.init)
        }
    }

    public func switchTo(repo: String, path: String) async {
        guard let tree = tree(repo: repo, path: path), !tree.current else { return }
        await send(Self.switchCommand, .object(["repo": .string(tree.repo), "path": .string(tree.path)]))
        await load()
    }

    public func create(_ branch: String, repo: String? = nil) async {
        let branch = branch.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !branch.isEmpty else { return }
        var args: [String: JSONValue] = ["branch": .string(branch)]
        if let repo { args["repo"] = .string(repo) }
        await send(Self.newCommand, .object(args))
    }

    public func remove(repo: String, path: String) async {
        switch await run(CommandInvocation(id: Self.removePromptCommand, args: .object(["repo": .string(repo), "path": .string(path)]))) {
        case .success(let value):
            guard let prompt = WorktreePrompt(value) else { return }
            await ask(prompt)
        case .failure(let failure):
            report(failure.commandText)
        }
    }

    public func receive(_ event: BusEvent) async {
        if event.topic == Self.outcomeTopic, event.source == Self.source {
            let fields = event.payload.objectValue ?? [:]
            if let message = fields["message"]?.stringValue, !message.isEmpty { report(message) }
            if let prompt = fields["confirm"].flatMap(WorktreePrompt.init) { await ask(prompt) }
        }
        await load()
    }

    private func ask(_ prompt: WorktreePrompt) async {
        guard prompt.command == Self.removeCommand, await confirm(prompt.title, prompt.detail) else { return }
        await send(prompt.command, prompt.args)
    }

    private func tree(repo: String, path: String) -> Worktree? {
        worktrees.first { $0.repo == repo && $0.path == path }
    }

    @discardableResult
    private func fetch() async -> Bool {
        guard case .success(let value) = await run(CommandInvocation(id: Self.listCommand, args: .object([:]))) else { return false }
        worktrees = (value.arrayValue ?? []).compactMap(Worktree.init)
        return true
    }

    private func send(_ id: String, _ args: JSONValue) async {
        if case .failure(let failure) = await run(CommandInvocation(id: id, args: args)) {
            report(failure.commandText)
        }
    }

    public static func follow(_ client: CoreClient, received: @escaping @MainActor (BusEvent) async -> Void) async {
        guard let events = try? await client.busEvents(pattern: topics) else { return }
        for await event in events where !event.workspace.isEmpty && [changedTopic, outcomeTopic].contains(event.topic) {
            await received(event)
        }
    }
}
