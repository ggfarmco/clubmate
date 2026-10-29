import Foundation
import Observation

public struct Worktree: Equatable, Sendable, Identifiable {
    public let repo: String
    public let path: String
    public let branch: String
    public let head: String
    public let main: Bool
    public let current: Bool
    public let defaultBase: String

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
        defaultBase = fields["defaultBase"]?.stringValue ?? ""
    }
}

public struct WorktreeGroup: Equatable, Sendable, Identifiable {
    public let repo: String
    public let trees: [Worktree]

    public var id: String { repo }

    public var name: String { URL(fileURLWithPath: repo).lastPathComponent }
}

@MainActor
@Observable
public final class WorktreeModel {
    public static let listCommand = "git.worktrees"
    public static let switchCommand = "git.worktree-switch"
    public static let newCommand = "git.worktree-new"
    public static let removeCommand = "git.worktree-remove"
    public static let statusCommand = "git.worktree-op-status"
    public static let sessionsCommand = "git.worktree-sessions"
    static let statusRetries = 3
    public static let defaultRetryDelays: [Duration] = [.milliseconds(250), .milliseconds(500), .seconds(1), .seconds(2), .seconds(4)]

    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public private(set) var worktrees: [Worktree] = []

    @ObservationIgnored private let run: Run
    @ObservationIgnored private let project: @MainActor () -> Project?
    @ObservationIgnored private let confirm: @MainActor (String, String) async -> Bool
    @ObservationIgnored private let report: @MainActor (String) -> Void
    @ObservationIgnored private let retryDelays: [Duration]
    @ObservationIgnored private var announced: Set<String> = []
    @ObservationIgnored private var generation = 0

    public init(
        run: @escaping Run,
        project: @escaping @MainActor () -> Project?,
        confirm: @escaping @MainActor (String, String) async -> Bool,
        report: @escaping @MainActor (String) -> Void,
        retryDelays: [Duration] = WorktreeModel.defaultRetryDelays
    ) {
        self.run = run
        self.project = project
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

    public var reviewLabel: String {
        guard let base = current?.defaultBase, !base.isEmpty else { return "Review against default base" }
        return "Review against \(base)"
    }

    public var pickerItems: [PickerItem] {
        worktrees.map { PickerItem(id: $0.id, label: label($0), checked: $0.current) }
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
        guard mine == generation, let project = project() else { return }
        var switched = false
        for path in project.missing where !announced.contains(path) {
            announced.insert(path)
            let owner = worktrees.filter { Self.within(path, $0.path) }.max { $0.path.count < $1.path.count }
            let lost = owner.flatMap { $0.main ? nil : $0 }
            report("Worktree \(lost?.name ?? Self.folderName(path)) no longer exists — back on main")
            guard let repo = owner?.repo ?? soleRepo, let home = checkout(of: repo) else { continue }
            await send(Self.switchCommand, ["repo": .string(home.repo), "path": .string(home.path)])
            switched = true
        }
        if switched { await fetch() }
    }

    public func menuOpened() async {
        await load()
    }

    public func switchTo(repo: String, path: String) async {
        guard let tree = tree(repo: repo, path: path), !tree.current else { return }
        await send(Self.switchCommand, ["repo": .string(tree.repo), "path": .string(tree.path)])
        await load()
    }

    public func create(_ branch: String, repo: String? = nil) async {
        let branch = branch.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !branch.isEmpty else { return }
        var args: [String: JSONValue] = ["branch": .string(branch)]
        if let repo { args["repo"] = .string(repo) }
        if let failure = await perform(CommandInvocation(id: Self.newCommand, args: .object(args))) { report(failure) }
        await load()
    }

    public func remove(repo: String, path: String) async {
        guard let tree = tree(repo: repo, path: path), !tree.main else { return }
        let titles: [String]
        switch await run(CommandInvocation(id: Self.sessionsCommand, args: .object(["path": .string(tree.path)]))) {
        case .success(let value):
            titles = (value.arrayValue ?? []).compactMap { $0.objectValue?["title"]?.stringValue }
        case .failure(let failure):
            return report(NvimConfigModel.text(failure))
        }
        let detail = titles.isEmpty ? "" : "Terminals still in it: \(titles.joined(separator: ", "))"
        guard await confirm("Remove worktree \(tree.name)? Its branch is kept.", detail) else { return }
        var args: [String: JSONValue] = ["repo": .string(tree.repo), "path": .string(tree.path)]
        var failure = await perform(CommandInvocation(id: Self.removeCommand, args: .object(args)))
        if let message = failure, let marker = message.range(of: NvimRestart.unsavedMarker) {
            guard await confirm("Worktree \(tree.name) has \(message[marker.lowerBound...]). Remove it and discard the changes?", "") else { return }
            args["force"] = .bool(true)
            failure = await perform(CommandInvocation(id: Self.removeCommand, args: .object(args)))
        }
        if let failure { report(failure) }
        await load()
    }

    private func tree(repo: String, path: String) -> Worktree? {
        worktrees.first { $0.repo == repo && $0.path == path }
    }

    private func checkout(of repo: String) -> Worktree? {
        worktrees.filter { $0.repo == repo && Self.within(repo, $0.path) }.max { $0.path.count < $1.path.count }
            ?? worktrees.first { $0.repo == repo && $0.current }
    }

    private var soleRepo: String? {
        let repos = Set(worktrees.map(\.repo))
        return repos.count == 1 ? repos.first : nil
    }

    @discardableResult
    private func fetch() async -> Bool {
        guard case .success(let value) = await run(CommandInvocation(id: Self.listCommand, args: .object([:]))) else { return false }
        worktrees = (value.arrayValue ?? []).compactMap(Worktree.init)
        return true
    }

    private func send(_ id: String, _ args: [String: JSONValue]) async {
        if case .failure(let failure) = await run(CommandInvocation(id: id, args: .object(args))) {
            report(NvimConfigModel.text(failure))
        }
    }

    private func perform(_ invocation: CommandInvocation) async -> String? {
        let id: String
        switch await run(invocation) {
        case .success(let value):
            guard let started = value.objectValue?["id"]?.stringValue else { return "unexpected reply from \(invocation.id)" }
            id = started
        case .failure(let failure):
            return NvimConfigModel.text(failure)
        }
        var retries = 0
        while !Task.isCancelled {
            switch await run(CommandInvocation(id: Self.statusCommand, args: .object(["id": .string(id)]))) {
            case .success(let value):
                retries = 0
                let fields = value.objectValue ?? [:]
                guard fields["done"]?.boolValue == true else { continue }
                return fields["error"]?.stringValue.map(NvimConfigModel.text)
            case .failure(let failure) where failure.isTransient && retries < Self.statusRetries:
                retries += 1
                try? await Task.sleep(for: .milliseconds(200 * retries))
            case .failure(let failure):
                return NvimConfigModel.text(failure)
            }
        }
        return nil
    }

    private static func within(_ path: String, _ dir: String) -> Bool {
        path == dir || path.hasPrefix(dir.hasSuffix("/") ? dir : dir + "/")
    }

    private static func folderName(_ path: String) -> String {
        URL(fileURLWithPath: path).lastPathComponent
    }
}
