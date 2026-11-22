import Observation
import SwiftUI

import CompanionKit

struct SidebarItem: Hashable {
    let computer: String
    let plugin: String
}

struct VoiceDockFrame: Equatable {
    let id: UUID
    var frame: CGRect
}

struct ReportSheet: Identifiable, Hashable {
    let computer: String
    let about: String?

    var id: String { computer + "|" + (about ?? "") }
}

struct LiveTabs: Equatable {
    let workspace: WorkspaceKey
    let tabs: [CompanionTab]
}

enum RootTab: Hashable {
    case section(CompanionTab)
}

@MainActor
@Observable
final class AppNavigation {
    @ObservationIgnored weak var model: CompanionModel?
    @ObservationIgnored let menu = MenuBoard()
    @ObservationIgnored private var stacks: TabStacks {
        didSet {
            guard stacks != oldValue else { return }
            publish()
            if persists { stacks.save(to: .standard) }
        }
    }
    private(set) var workspaceKey: WorkspaceKey?
    private(set) var shownTab: CompanionTab = .agents
    private let paths = Dictionary(uniqueKeysWithValues: CompanionTab.allCases.map { ($0, TabPath()) })
    private(set) var knownTabs: KnownTabs {
        didSet { if persists, knownTabs != oldValue { knownTabs.save(to: .standard) } }
    }
    var connectionSheet: ConnectionSheet?
    var reportSheet: ReportSheet?
    var usageSheet: UsageSheet?
    var workspaces: [String: String] = [:] {
        didSet { if workspaces != oldValue { follow() } }
    }
    var newTaskWorkspaces: [String: String] = [:]
    var showingAddComputer = false
    @ObservationIgnored private(set) var pairingSheetUp = false
    @ObservationIgnored private var pairedUnderSheet: String?
    @ObservationIgnored private var pending: (computer: String, workspace: String?, tab: CompanionTab, destination: Destination?)?
    @ObservationIgnored private let persists: Bool
    var sectionItem: String?
    private(set) var voiceDocks: [VoiceDockFrame] = []
    private(set) var searchRequests = 0
    var typing = false
    var layout: CompanionLayout = .compact
    var room: ColumnStep.Room = .beside {
        didSet {
            guard room != oldValue else { return }
            listOver = false
        }
    }
    var readingView = false
    var listOver = false
    private(set) var chatShown = false
    @ObservationIgnored private var chatOpenPending = false
    var lastWiki: [WorkspaceKey: StackEntry] = [:]
    private var readingEntry: StackEntry?

    var voiceDock: CGRect? { voiceDocks.last?.frame }

    init(restore: Bool = AppNavigation.restores) {
        persists = restore
        UserDefaults.standard.removeObject(forKey: "sidebarHidden")
        stacks = restore ? TabStacks.restore(from: .standard) : TabStacks()
        knownTabs = restore ? KnownTabs.restore(from: .standard) : KnownTabs()
        if let key = stacks.workspace { workspaces[key.computer] = key.workspace }
        publish()
    }

    private func publish() {
        if workspaceKey != stacks.workspace { workspaceKey = stacks.workspace }
        if shownTab != stacks.tab { shownTab = stacks.tab }
        for (tab, box) in paths {
            box.update(stacks.workspace.map { stacks.path(for: StackKey($0, tab: tab)) } ?? [], column: stacks.column(for: tab))
        }
    }

    private func box(_ tab: CompanionTab) -> TabPath {
        paths[tab] ?? TabPath()
    }

    private static var restores: Bool {
        #if DEBUG
        let defaults = UserDefaults.standard
        return defaults.string(forKey: "fixtures") == nil || defaults.bool(forKey: "restoreStacks")
        #else
        true
        #endif
    }

    var computer: String? { workspaceKey?.computer }

    var tab: CompanionTab {
        get { shownTab }
        set { stacks.tab = newValue }
    }

    var root: RootTab {
        get { .section(tab) }
        set {
            guard case .section(let tab) = newValue else { return }
            switchTab(tab)
        }
    }

    func pick(_ tab: CompanionTab) {
        #if DEBUG
        FrameMonitor.mark("tab")
        #endif
        guard tab == self.tab else { return switchTab(tab) }
        tapped(tab)
    }

    var path: [StackEntry] { path(listTab) }

    func tabs(_ model: CompanionModel) -> [CompanionTab] {
        if let known = knownTabs.tabs(for: workspaceKey, live: liveTabs(model)?.tabs) { return known }
        let partial = CompanionPlugins.tabs(computer.flatMap { model.connection($0) }, sections: [])
        return CompanionTab.allCases.filter { partial.contains($0) || $0 == tab }
    }

    func liveTabs(_ model: CompanionModel) -> LiveTabs? {
        guard let key = workspaceKey, let connection = model.connection(key.computer) else { return nil }
        return CompanionPlugins.liveTabs(connection, ui: connection.workspaceUI(key.workspace)).map { LiveTabs(workspace: key, tabs: $0) }
    }

    func remember(_ live: LiveTabs) {
        knownTabs.remember(live.tabs, for: live.workspace)
    }

    func hidesTabBar(_ tab: CompanionTab) -> Bool {
        let path = path(tab)
        return path.contains { $0.destination.isSession } || path.last.map { $0 == readingEntry } == true
    }

    func reading(_ active: Bool, _ entry: StackEntry) {
        if active {
            readingEntry = entry
        } else if readingEntry == entry {
            readingEntry = nil
        }
    }

    func path(_ tab: CompanionTab) -> [StackEntry] {
        box(tab).entries
    }

    func setPath(_ entries: [StackEntry], tab: CompanionTab) {
        guard let workspace = stacks.workspace else { return }
        stacks.setPath(entries, for: StackKey(workspace, tab: tab))
    }

    func workspace(_ computer: String) -> String? {
        workspaces[computer]
    }

    func push(_ destination: Destination, from origin: Origin? = nil, in workspace: WorkspaceKey? = nil) {
        adoptListTab()
        stacks.push(destination, from: origin, in: workspace)
    }

    func replaceTop(_ destination: Destination) {
        adoptListTab()
        guard !path.isEmpty else { return push(destination) }
        if path.last?.destination != destination { stacks.replaceTop(destination) }
    }

    func reach(_ destination: Destination, from origin: Origin? = nil) {
        guard path.dropLast().last?.destination == destination else { return push(destination, from: origin) }
        pop()
    }

    func origin(kind: Origin.Kind = .page) -> Origin {
        guard let top = path.last else { return Origin(tab: tab, title: rootTitle, kind: kind) }
        return Origin(tab: top.destination.tab, title: title(top), kind: top.destination == .inbox && kind == .page ? .inbox : kind)
    }

    func title(_ entry: StackEntry) -> String {
        let key = entry.workspace ?? workspaceKey
        switch entry.destination {
        case .session(let id):
            return key.flatMap { model?.agents(computer: $0.computer, workspace: $0.workspace, settled: false)?.session(id)?.displayTitle } ?? "Session"
        case .run:
            return "Run"
        case .task(let number):
            return "#\(number)"
        case .wikiPage(_, _, let title):
            return title
        case .doc(let path, _):
            return URL(filePath: path).deletingPathExtension().lastPathComponent
        case .inbox:
            return "Inbox"
        case .section(let id, _):
            return key.flatMap { model?.connection($0.computer)?.workspaceUI($0.workspace).sections.first { $0.id == id }?.title } ?? id
        case .macDetails(let computer):
            return model?.connection(computer)?.computer.name ?? "Mac"
        }
    }

    var rootTitle: String {
        guard let key = workspaceKey, let connection = model?.connection(key.computer) else { return tab.title }
        return connection.workspaces.first { $0.id == key.workspace }?.name ?? tab.title
    }

    func workspaceName(_ key: WorkspaceKey?) -> String {
        guard let key = key ?? workspaceKey else { return "" }
        return model?.connection(key.computer)?.workspaces.first { $0.id == key.workspace }?.name ?? ""
    }

    func openLink(_ path: String, anchor: String?, from origin: Origin) {
        guard let key = workspaceKey, let connection = model?.connection(key.computer) else { return }
        let doc = Destination.doc(path: path, anchor: anchor)
        guard connection.supports(WikiCommand.locate) else { return push(doc, from: origin) }
        Task {
            let page = try? await connection.command(WikiCommand.locate, workspace: key.workspace, args: .object(["path": .string(anchor.map { path + "#" + $0 } ?? path)]), as: WikiLocation.self)
            guard workspaceKey == key else { return }
            if let page, let id = page.id, let file = page.path {
                push(.wikiPage(id: id, path: file, title: page.title ?? URL(filePath: file).deletingPathExtension().lastPathComponent), from: origin)
            } else {
                push(doc, from: origin)
            }
        }
    }

    var tabHasSearch: Bool {
        tab != .more
    }

    func focusSearch() {
        guard tabHasSearch else { return }
        if layout == .compact { popToRoot(tab) }
        searchRequests += 1
    }

    func notified(computer: String, workspace: String?, session: String?, active: Bool) {
        let workspace = workspace.flatMap { $0.isEmpty ? nil : $0 }
        let destination = session.map(Destination.session) ?? .inbox
        guard active, let current = workspaceKey, model?.connection(computer) != nil else {
            jump(computer: computer, workspace: workspace, tab: .agents, to: destination)
            return
        }
        let key = workspace.map { WorkspaceKey(computer: computer, workspace: $0) } ?? (computer == current.computer ? current : nil)
        guard let key else {
            jump(computer: computer, workspace: workspace, tab: .agents, to: destination)
            return
        }
        guard path.last?.destination != destination || path.last?.workspace ?? current != key else { return }
        push(destination, from: origin(kind: .notification), in: key)
    }

    func jump(computer: String, workspace: String? = nil, tab: CompanionTab = .agents, to destination: Destination? = nil) {
        self.pending = nil
        guard let model, let connection = model.connection(computer), let target = workspace.flatMap({ id in connection.workspaces.first { $0.id == id } }) ?? self.workspace(for: connection) else {
            pending = (computer, workspace, tab, destination)
            return
        }
        if workspaces[computer] != target.id { workspaces[computer] = target.id }
        select(computer: computer, workspace: target.id)
        reset(tab, destination)
    }

    func selected(_ tab: CompanionTab) -> String? {
        box(tab).first.flatMap { Self.tag($0.destination) }
    }

    func selection(_ tab: CompanionTab) -> Binding<String?> {
        Binding(get: { self.selected(tab) }, set: { self.select($0, tab: tab) })
    }

    func listSelection(_ tab: CompanionTab) -> Binding<String?> {
        guard layout == .compact else { return selection(tab) }
        return Binding(get: { nil }, set: { tag in
            if let tag { self.select(tag, tab: tab) }
        })
    }

    func select(_ tag: String?, tab: CompanionTab) {
        guard let tag else { return popToRoot(tab) }
        if tab == .agents, GateEntry.matches(tag) {
            sectionItem = tag
            return setPath([StackEntry(.section(id: GateEntry.section, item: nil))], tab: tab)
        }
        setPath([StackEntry(Self.destination(tag, tab: tab))], tab: tab)
    }

    func tapped(_ tab: CompanionTab) {
        if !path(tab).isEmpty { popToRoot(tab) }
    }

    func pop() {
        adoptListTab()
        stacks.pop()
    }

    private func adoptListTab() {
        let shown = listTab
        if shown != stacks.tab { switchTab(shown) }
    }

    func popToRoot(_ tab: CompanionTab) {
        setPath([], tab: tab)
    }

    func switchTab(_ tab: CompanionTab) {
        if shownTab != tab {
            readingView = false
            listOver = false
        }
        stacks.tab = tab
    }

    func showIn(_ tab: CompanionTab, _ destination: Destination) {
        switchTab(tab)
        if path.last?.destination != destination { push(destination) }
    }

    func show(in tab: CompanionTab, _ destination: Destination) {
        if path.last?.destination == destination { pop() }
        showIn(tab, destination)
    }

    func switchWorkspace(computer: String, workspace: String) {
        select(computer: computer, workspace: workspace)
        workspaces[computer] = workspace
    }

    func pushFromInbox(_ destination: Destination, in key: WorkspaceKey) {
        if stacks.workspace != key {
            switchWorkspace(computer: key.computer, workspace: key.workspace)
            if !isTop(.inbox) { push(.inbox) }
        }
        push(destination, from: Origin(tab: nil, title: "Inbox", kind: .inbox))
    }

    func columnState(_ tab: CompanionTab) -> ColumnStep {
        box(tab).column
    }

    var listTab: CompanionTab {
        guard let model else { return tab }
        let tabs = tabs(model)
        return tabs.contains(tab) ? tab : tabs.first ?? tab
    }

    var overlays: Bool { overlays(listTab) }

    func overlays(_ tab: CompanionTab) -> Bool {
        layout != .compact && room == .over && !path(tab).isEmpty
    }

    var listShown: Bool { listShown(listTab, shown: true) }

    func listShown(_ tab: CompanionTab, shown: Bool) -> Bool {
        guard layout != .compact, !(shown && readingView), !(tab == .wiki && chatDocked) else { return false }
        return overlays(tab) ? shown && listOver : columnState(tab) == .listDetail
    }

    func toggleList() {
        toggleList(listTab)
    }

    func toggleList(_ tab: CompanionTab) {
        guard layout != .compact else { return }
        if tab == .wiki, chatDocked, !readingView {
            chatShown = false
            guard !listShown(tab, shown: true) else { return }
        }
        if readingView {
            readingView = false
            return
        }
        if overlays(tab) {
            listOver.toggle()
        } else {
            stacks.toggleColumns(for: tab)
        }
    }

    var chatDocked: Bool { chatShown && layout == .wide }

    var canChat: Bool {
        guard UIDevice.current.userInterfaceIdiom == .pad, tab == .wiki, case .wikiPage = path.last?.destination else { return false }
        return true
    }

    func toggleChat() {
        if chatShown {
            chatShown = false
            chatOpenPending = false
        } else if canChat {
            chatShown = true
            chatOpenPending = true
        }
    }

    func takeChatOpen() -> Bool {
        defer { chatOpenPending = false }
        return chatOpenPending
    }

    var canRead: Bool {
        guard layout != .compact, let top = path.last else { return false }
        switch top.destination {
        case .wikiPage, .doc, .session: return true
        default: return false
        }
    }

    func toggleReading() {
        guard readingView || canRead else { return }
        listOver = false
        readingView.toggle()
    }

    var canEscape: Bool {
        layout != .compact && (readingView || overlays && listOver)
    }

    func escape() {
        if readingView {
            readingView = false
        } else if overlays {
            listOver = false
        }
    }

    var canGoBack: Bool {
        path.count > (layout == .compact ? 0 : 1)
    }

    func back() {
        guard canGoBack else { return }
        pop()
    }

    func prune() {
        guard let model else { return }
        var next = stacks
        next.drop(where: model.gone)
        if next != stacks { stacks = next }
    }

    func follow(_ model: CompanionModel) {
        if let pending, let connection = model.connection(pending.computer),
           let workspace = pending.workspace.flatMap({ id in connection.workspaces.first { $0.id == id } }) ?? workspace(for: connection) {
            if workspaces[pending.computer] != workspace.id { workspaces[pending.computer] = workspace.id }
            select(computer: pending.computer, workspace: workspace.id)
            return
        }
        let computer = computer.flatMap { model.connection($0) == nil ? nil : $0 } ?? model.computers.first?.id
        guard let computer, let connection = model.connection(computer), let workspace = workspace(for: connection) else { return }
        select(computer: computer, workspace: workspace.id)
    }

    private func follow() {
        guard let computer, let model, let connection = model.connection(computer), let workspace = workspace(for: connection) else { return }
        select(computer: computer, workspace: workspace.id)
    }

    private func select(computer: String, workspace: String) {
        if stacks.workspace != WorkspaceKey(computer: computer, workspace: workspace) {
            stacks.select(computer: computer, workspace: workspace)
            prune()
        }
        guard let pending, pending.computer == computer, pending.workspace == nil || pending.workspace == workspace else { return }
        self.pending = nil
        reset(pending.tab, pending.destination)
    }

    private func reset(_ tab: CompanionTab, _ destination: Destination?) {
        switchTab(tab)
        setPath(destination.map { [StackEntry($0)] } ?? [], tab: tab)
    }

    func land(_ session: String) {
        guard tab != .agents else { return replaceTop(.session(session)) }
        setPath([StackEntry(.session(session))], tab: .agents)
    }

    func isTop(_ destination: Destination) -> Bool {
        path.last?.destination == destination
    }

    static func destination(_ detail: String, tab: CompanionTab) -> Destination {
        switch tab {
        case .agents where AgentSessionID.matches(detail):
            .session(detail)
        case .agents where GraphRun.isRun(detail):
            .run(detail)
        case .backlog:
            Int(detail).map(Destination.task) ?? .section(id: detail, item: nil)
        default:
            .section(id: detail, item: nil)
        }
    }

    static func tag(_ destination: Destination) -> String? {
        switch destination {
        case .session(let id), .run(let id): id
        case .task(let id): String(id)
        case .section(let id, _): id
        default: nil
        }
    }

    func dock(_ id: UUID, at frame: CGRect) {
        if let index = voiceDocks.firstIndex(where: { $0.id == id }) {
            voiceDocks[index].frame = frame
        } else {
            voiceDocks.append(VoiceDockFrame(id: id, frame: frame))
        }
    }

    func undock(_ id: UUID) {
        voiceDocks.removeAll { $0.id == id }
    }

    func openPaired(_ computer: String) {
        showingAddComputer = false
        guard pairingSheetUp else {
            jump(computer: computer)
            return
        }
        pairedUnderSheet = computer
    }

    func pairingSheetAppeared() {
        pairingSheetUp = true
    }

    func pairingSheetDismissed() {
        pairingSheetUp = false
        guard let computer = pairedUnderSheet else { return }
        pairedUnderSheet = nil
        jump(computer: computer)
    }

    func workspace(for connection: ComputerConnection) -> Workspace? {
        let list = connection.workspaces
        return list.first { $0.id == workspaces[connection.computer.id] } ?? list.first
    }
}

@MainActor
@Observable
final class TabPath {
    private(set) var entries: [StackEntry] = []
    private(set) var first: StackEntry?
    private(set) var column: ColumnStep = .listDetail

    func update(_ entries: [StackEntry], column: ColumnStep) {
        if self.entries != entries { self.entries = entries }
        if first != entries.first { first = entries.first }
        if self.column != column { self.column = column }
    }
}

@MainActor
@Observable
final class PairingFlow {
    enum Phase: Equatable {
        case idle
        case pairing(String)
        case failed(String)
    }

    enum Source {
        case link
        case paste
        case scan
    }

    var phase: Phase = .idle
    var review: PairingReview?
    var paired: Computer?

    var failureMessage: String? {
        if case .failed(let message) = phase { return message }
        return nil
    }

    func received(_ text: String, from source: Source, model: CompanionModel, navigation: AppNavigation) {
        let link: PairingLink
        do {
            link = try PairingLink.parse(text, policy: model.policy)
        } catch {
            phase = .failed(error.message)
            return
        }
        let review = PairingReview(link: link, existing: model.computers)
        if Self.autoConfirm || !review.needsConfirmation(scanned: source == .scan) {
            pair(link, model: model, navigation: navigation)
            return
        }
        self.review = review
    }

    func confirm(model: CompanionModel, navigation: AppNavigation) {
        guard let review else { return }
        self.review = nil
        pair(review.link, model: model, navigation: navigation)
    }

    func pair(_ link: PairingLink, model: CompanionModel, navigation: AppNavigation) {
        phase = .pairing(link.computerName)
        Task {
            do {
                let computer = try await model.pair(link)
                phase = .idle
                paired = computer
                navigation.openPaired(computer.id)
            } catch {
                phase = .failed((error as? RemoteError)?.message ?? error.localizedDescription)
            }
        }
    }

    func dismissFailure() {
        if case .failed = phase { phase = .idle }
    }

    private static var autoConfirm: Bool {
        #if DEBUG
        UserDefaults.standard.bool(forKey: "autoConfirmPairing")
        #else
        false
        #endif
    }
}
