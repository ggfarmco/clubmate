import Foundation
import Observation

public struct WorktreeEntry: Equatable, Sendable, Identifiable {
    public let repo: String
    public let path: String
    public let branch: String
    public let head: String
    public let folder: String
    public let detail: String
    public let icon: String
    public let tone: String
    public let now: Bool
    public let main: Bool
    public let merged: Bool
    public let stale: Bool
    public let taskSession: String
    public let taskState: String
    let taskTitle: String
    let sessionTitle: String
    let haystack: String

    public var id: String { "worktree:\(path)" }

    public var name: String { branch.isEmpty ? "detached · \(head)" : branch }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let repo = fields["repo"]?.stringValue, let path = fields["path"]?.stringValue else { return nil }
        let task = fields["task"]?.objectValue
        self.repo = repo
        self.path = path
        branch = fields["branch"]?.stringValue ?? ""
        head = fields["head"]?.stringValue ?? ""
        folder = fields["folder"]?.stringValue ?? URL(fileURLWithPath: repo).lastPathComponent
        detail = fields["detail"]?.stringValue ?? ""
        icon = fields["icon"]?.stringValue ?? ""
        tone = fields["tone"]?.stringValue ?? ""
        now = fields["now"]?.boolValue ?? false
        main = fields["main"]?.boolValue ?? false
        merged = fields["merged"]?.boolValue ?? false
        stale = fields["stale"]?.boolValue ?? false
        taskSession = task?["session"]?.stringValue ?? ""
        taskState = task?["state"]?.stringValue ?? ""
        taskTitle = task?["title"]?.stringValue ?? ""
        sessionTitle = fields["session"]?.objectValue?["title"]?.stringValue ?? ""
        haystack = [folder, branch, taskTitle, sessionTitle].joined(separator: "\n").lowercased()
    }
}

public struct WorktreeTaskRow: Equatable, Sendable, Identifiable {
    public let session: String
    public let title: String
    public let state: String
    public let icon: String
    public let tone: String
    public let worktrees: [String]
    let haystack: String

    public var id: String { "task:\(session)" }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let session = fields["session"]?.stringValue else { return nil }
        let slug = fields["slug"]?.stringValue ?? ""
        self.session = session
        title = fields["title"]?.stringValue ?? slug
        state = fields["state"]?.stringValue ?? ""
        icon = fields["icon"]?.stringValue ?? ""
        tone = fields["tone"]?.stringValue ?? ""
        worktrees = fields["worktrees"]?.arrayValue?.compactMap(\.stringValue) ?? []
        haystack = "\(title)\n\(slug)".lowercased()
    }
}

public struct WorktreeSection: Equatable, Sendable, Identifiable {
    public let id: String
    public let title: String
    public let hint: String
    public let rows: [Int]
    public let shown: Int
    public let more: String
    public let fold: String

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue else { return nil }
        self.id = id
        title = fields["title"]?.stringValue ?? ""
        hint = fields["hint"]?.stringValue ?? ""
        rows = fields["rows"]?.arrayValue?.compactMap(\.numberValue).map(Int.init) ?? []
        shown = fields["shown"]?.numberValue.map(Int.init) ?? 0
        more = fields["more"]?.stringValue ?? ""
        fold = fields["fold"]?.stringValue ?? ""
    }
}

public struct WorktreeFolder: Equatable, Sendable, Identifiable {
    public let path: String
    public let name: String
    public let new: Bool
    public let session: String
    public let caption: String
    public let byline: String
    public let summary: String
    public let filter: String
    public let rows: [Int]
    public let sections: [WorktreeSection]

    public var id: String { "folder:\(path)" }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let path = fields["path"]?.stringValue else { return nil }
        self.path = path
        name = fields["name"]?.stringValue ?? URL(fileURLWithPath: path).lastPathComponent
        new = fields["new"]?.boolValue ?? false
        session = fields["session"]?.stringValue ?? ""
        caption = fields["caption"]?.stringValue ?? ""
        byline = fields["byline"]?.stringValue ?? ""
        summary = fields["summary"]?.stringValue ?? ""
        filter = fields["filter"]?.stringValue ?? ""
        rows = fields["rows"]?.arrayValue?.compactMap(\.numberValue).map(Int.init) ?? []
        sections = fields["sections"]?.arrayValue?.compactMap(WorktreeSection.init) ?? []
    }
}

public struct WorktreeIndex: Equatable, Sendable {
    public let folders: [WorktreeFolder]
    public let tasks: [WorktreeTaskRow]
    public let entries: [WorktreeEntry]
    public let sections: [WorktreeSection]
    public let placeholder: String
    public let count: String
    public let byFolder: Bool
    public let addFolder: String
    public let remove: String
    public let searchHint: String
    public let searchStale: Int

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let picker = fields["picker"]?.objectValue else { return nil }
        folders = fields["folders"]?.arrayValue?.compactMap(WorktreeFolder.init) ?? []
        tasks = fields["tasks"]?.arrayValue?.compactMap(WorktreeTaskRow.init) ?? []
        entries = fields["worktrees"]?.arrayValue?.compactMap(WorktreeEntry.init) ?? []
        sections = picker["sections"]?.arrayValue?.compactMap(WorktreeSection.init) ?? []
        placeholder = picker["placeholder"]?.stringValue ?? ""
        count = picker["count"]?.stringValue ?? ""
        byFolder = picker["byFolder"]?.boolValue ?? true
        addFolder = picker["addFolder"]?.stringValue ?? ""
        remove = picker["remove"]?.stringValue ?? ""
        searchHint = picker["searchHint"]?.stringValue ?? ""
        searchStale = picker["searchStale"]?.numberValue.map(Int.init) ?? 0
    }
}

public enum WorktreePickerRow: Equatable, Sendable, Identifiable {
    case section(id: String, title: String, hint: String)
    case folderHeader(WorktreeFolder)
    case worktree(WorktreeEntry, Style)
    case task(WorktreeTaskRow)
    case folder(WorktreeFolder)
    case note(id: String, text: String)
    case fold(id: String, text: String)
    case branch(Branch, folder: String)

    public struct Style: Equatable, Sendable {
        public let folder: Bool
        public let indented: Bool
        public let plain: Bool
        public let state: Bool
    }

    public var id: String {
        switch self {
        case .section(let id, _, _): "section:\(id)"
        case .folderHeader(let folder): "header:\(folder.path)"
        case .worktree(let entry, _): entry.id
        case .task(let task): task.id
        case .folder(let folder): folder.id
        case .note(let id, _): "note:\(id)"
        case .fold(let id, _): "fold:\(id)"
        case .branch(let branch, _): branch.id
        }
    }

    public var selectable: Bool {
        switch self {
        case .section, .folderHeader, .note: false
        default: true
        }
    }
}

public enum WorktreePickerChoice: Equatable, Sendable {
    case worktree(WorktreeEntry)
    case branch(Branch)
}

@MainActor
@Observable
public final class WorktreePickerModel {
    public static let command = "agents.worktrees"
    public static let moreStale = "search-stale"
    static let maxBranchRows = 200

    public enum Scope: Equatable, Sendable {
        case all
        case folder(String)
        case task(String)
    }

    public private(set) var index: WorktreeIndex?
    public private(set) var unavailable = false
    public private(set) var query = ""
    public private(set) var scope = Scope.all
    public private(set) var rows: [WorktreePickerRow] = []
    public private(set) var selection = 0
    public private(set) var matches = 0
    public var actionsFolder: WorktreeFolder?

    @ObservationIgnored private let run: WorktreeModel.Run
    @ObservationIgnored private let branches: @MainActor () -> [Branch]
    @ObservationIgnored private var expanded: Set<String> = []

    public init(run: @escaping WorktreeModel.Run, branches: @escaping @MainActor () -> [Branch] = { [] }) {
        self.run = run
        self.branches = branches
    }

    public var terms: [String] {
        query.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
    }

    public var searching: Bool { !terms.isEmpty }

    public var inside: Bool { scope != root }

    public var placeholder: String {
        if case .folder(let path) = scope, let folder = folder(path) { return folder.filter }
        return index?.placeholder ?? ""
    }

    public var caption: String {
        if searching { return matchCaption }
        if case .folder(let path) = scope, let folder = folder(path) { return "\(folder.rowsTotal)" }
        return index?.count ?? ""
    }

    public var selectedRow: WorktreePickerRow? {
        rows.indices.contains(selection) ? rows[selection] : nil
    }

    public var scopedFolder: WorktreeFolder? {
        if case .folder(let path) = scope { return folder(path) }
        return nil
    }

    public var multipleFolders: Bool { (index?.folders.count ?? 0) > 1 }

    public func load() async {
        guard case .success(let value) = await run(CommandInvocation(id: Self.command, args: .object([:]))),
              let fresh = WorktreeIndex(value) else {
            unavailable = true
            return
        }
        unavailable = false
        let chosen = selectedRow?.id
        let first = index == nil
        index = fresh
        if first { scope = root }
        rebuild(keeping: chosen)
    }

    public func reset() {
        query = ""
        expanded = []
        actionsFolder = nil
        scope = root
        rebuild(keeping: nil)
    }

    public func update(_ query: String) {
        self.query = query
        rebuild(keeping: nil)
    }

    public func branchesChanged() {
        if searching { rebuild(keeping: selectedRow?.id) }
    }

    public func move(_ delta: Int) {
        let selectable = rows.indices.filter { rows[$0].selectable }
        guard !selectable.isEmpty else { return }
        let position = selectable.firstIndex(of: selection) ?? 0
        selection = selectable[(position + delta + selectable.count) % selectable.count]
        if case .fold(Self.moreStale, _) = rows[selection] { expand(Self.moreStale) }
    }

    public func select(_ row: Int) {
        if rows.indices.contains(row), rows[row].selectable { selection = row }
    }

    public func activate() -> WorktreePickerChoice? {
        switch selectedRow {
        case .worktree(let entry, _): return .worktree(entry)
        case .branch(let branch, _): return .branch(branch)
        case .folder(let folder): enter(.folder(folder.path))
        case .task(let task):
            let trees = worktrees(of: task)
            if trees.count == 1 { return .worktree(trees[0]) }
            enter(.task(task.session))
        case .fold(let id, _): expand(id)
        default: break
        }
        return nil
    }

    public var opensForward: Bool {
        switch selectedRow {
        case .folder, .task: true
        default: false
        }
    }

    public func back() -> Bool {
        guard inside else { return false }
        enter(root)
        return true
    }

    public func actionsTarget() -> WorktreeFolder? {
        guard let index else { return nil }
        if index.byFolder, index.folders.count < 2 { return nil }
        switch selectedRow {
        case .worktree(let entry, _): return folder(entry.repo)
        case .folder(let folder): return folder
        case .branch(let branch, _): return folder(branch.repo)
        default: return scopedFolder
        }
    }

    private var root: Scope {
        guard let index, !index.byFolder, index.folders.count == 1 else { return .all }
        return .folder(index.folders[0].path)
    }

    private var matchCaption: String {
        let folders = Set(rows.compactMap { row -> String? in
            if case .worktree(let entry, _) = row { return entry.repo }
            return nil
        }).count
        let noun = folders == 1 ? "folder" : "folders"
        return folders > 0 ? "\(matches) in \(folders) \(noun)" : "\(matches)"
    }

    private func folder(_ path: String) -> WorktreeFolder? {
        index?.folders.first { $0.path == path }
    }

    private func worktrees(of task: WorktreeTaskRow) -> [WorktreeEntry] {
        task.worktrees.compactMap { path in index?.entries.first { $0.path == path } }
    }

    private func enter(_ scope: Scope) {
        self.scope = scope
        query = ""
        rebuild(keeping: nil)
    }

    private func expand(_ id: String) {
        expanded.insert(id)
        let row = selection
        rebuild(keeping: nil)
        selection = rows.indices.contains(row) && rows[row].selectable ? row : selection
    }

    private func rebuild(keeping id: String?) {
        guard let index else {
            rows = []
            return
        }
        rows = searching ? searchRows(index) : browseRows(index)
        if let id, let kept = rows.firstIndex(where: { $0.id == id }) {
            selection = kept
        } else {
            selection = rows.firstIndex(where: \.isNow) ?? rows.firstIndex(where: \.selectable) ?? 0
        }
    }

    private func browseRows(_ index: WorktreeIndex) -> [WorktreePickerRow] {
        matches = 0
        switch scope {
        case .all:
            return index.byFolder ? byFolderRows(index) : homeRows(index)
        case .folder(let path):
            guard let folder = folder(path) else { return [] }
            return folder.sections.flatMap { sectionRows($0, in: index, style: Self.inFolder) }
        case .task(let session):
            guard let task = index.tasks.first(where: { $0.session == session }) else { return [] }
            return [.section(id: task.id, title: task.title, hint: task.state)] + worktrees(of: task).map { .worktree($0, Self.acrossFolders) }
        }
    }

    private func byFolderRows(_ index: WorktreeIndex) -> [WorktreePickerRow] {
        let many = index.folders.count > 1
        let style = WorktreePickerRow.Style(folder: false, indented: many, plain: true, state: false)
        return index.folders.flatMap { folder -> [WorktreePickerRow] in
            let rows = folder.rows.filter(index.entries.indices.contains).map { WorktreePickerRow.worktree(index.entries[$0], style) }
            return (many ? [.folderHeader(folder)] : []) + rows
        }
    }

    private func homeRows(_ index: WorktreeIndex) -> [WorktreePickerRow] {
        index.sections.flatMap { section -> [WorktreePickerRow] in
            let rows = sectionRows(section, in: index, style: Self.acrossFolders)
            return section.rows.isEmpty ? rows + index.folders.map { .folder($0) } : rows
        }
    }

    private func sectionRows(_ section: WorktreeSection, in index: WorktreeIndex, style: WorktreePickerRow.Style) -> [WorktreePickerRow] {
        var out: [WorktreePickerRow] = [.section(id: section.id, title: section.title, hint: section.hint)]
        let open = expanded.contains(section.id)
        let shown = open ? section.rows.count : section.shown
        out += section.rows.prefix(shown).filter(index.entries.indices.contains).map { .worktree(index.entries[$0], style) }
        if !open, !section.fold.isEmpty { out.append(.fold(id: section.id, text: section.fold)) }
        if !section.more.isEmpty { out.append(.note(id: section.id, text: section.more)) }
        return out
    }

    private func searchRows(_ index: WorktreeIndex) -> [WorktreePickerRow] {
        let terms = terms
        var out: [WorktreePickerRow] = []
        let tasks = taskMatches(index, terms)
        if !tasks.isEmpty {
            out.append(.section(id: "tasks", title: "Tasks · \(tasks.count)", hint: "one row, every folder"))
            out += tasks.map { .task($0) }
        }
        let found = worktreeMatches(index, terms)
        matches = tasks.count + found.count
        if !found.isEmpty {
            out.append(.section(id: "worktrees", title: "Worktrees · \(found.count)", hint: ""))
            out += capped(found, stale: index.searchStale, tasks: Set(tasks.map(\.session)))
        }
        let names = branchMatches(index)
        if !names.isEmpty {
            out.append(.section(id: "branches", title: "Branches · \(names.count)", hint: ""))
            out += names.map { .branch($0, folder: multipleFolders ? URL(fileURLWithPath: $0.repo).lastPathComponent : "") }
        }
        return out
    }

    private func taskMatches(_ index: WorktreeIndex, _ terms: [String]) -> [WorktreeTaskRow] {
        guard scope == .all else { return [] }
        return index.tasks.filter { task in terms.allSatisfy(task.haystack.contains) }
    }

    private func scoped(_ index: WorktreeIndex) -> [WorktreeEntry] {
        switch scope {
        case .all: index.entries
        case .folder(let path): index.entries.filter { $0.repo == path }
        case .task(let session): index.entries.filter { $0.taskSession == session }
        }
    }

    private func worktreeMatches(_ index: WorktreeIndex, _ terms: [String]) -> [WorktreeEntry] {
        let pool = scoped(index)
        let exact = pool.filter { entry in terms.allSatisfy(entry.haystack.contains) }
        let found = Set(exact.map(\.path))
        let loose = FuzzyRank.rank(pool.filter { !found.contains($0.path) }, query: query, text: \.name)
        let all = exact + loose
        return all.filter { !$0.stale } + all.filter(\.stale)
    }

    private func capped(_ found: [WorktreeEntry], stale cap: Int, tasks: Set<String>) -> [WorktreePickerRow] {
        let style = scope == .all ? Self.acrossFolders : Self.inFolder
        let row = { (entry: WorktreeEntry) -> WorktreePickerRow in
            let short = !entry.taskSession.isEmpty && tasks.contains(entry.taskSession)
            return .worktree(entry, .init(folder: style.folder, indented: style.indented, plain: style.plain, state: short))
        }
        let live = found.filter { !$0.stale }
        let stale = found.filter(\.stale)
        guard stale.count > cap, !expanded.contains(Self.moreStale) else { return found.map(row) }
        let shown = live + stale.prefix(cap)
        return shown.map(row) + [.fold(id: Self.moreStale, text: "\(stale.count - cap) more stale · ↓ to show")]
    }

    private func branchMatches(_ index: WorktreeIndex) -> [Branch] {
        var pool = branches()
        if case .folder(let path) = scope { pool = pool.filter { $0.repo == path } }
        if case .task = scope { return [] }
        return Array(FuzzyRank.rank(pool, query: query, text: \.name).prefix(Self.maxBranchRows))
    }

    private static let acrossFolders = WorktreePickerRow.Style(folder: true, indented: false, plain: false, state: false)
    private static let inFolder = WorktreePickerRow.Style(folder: false, indented: false, plain: false, state: false)
}

extension WorktreeFolder {
    fileprivate var rowsTotal: Int {
        sections.reduce(0) { $0 + $1.rows.count }
    }
}

extension WorktreePickerRow {
    fileprivate var isNow: Bool {
        if case .worktree(let entry, _) = self { return entry.now }
        return false
    }
}
