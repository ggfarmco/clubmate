import AppKit
import Foundation
import Observation
import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class AppModel {
    let supervisor: ConnectionSupervisor
    let updates: Updates
    let settings = AppSettings(defaults: TestMode.defaults)
    let claude = ClaudeIntegration()
    private(set) var projects: ProjectsStore?
    private(set) var commands: [Command] = []
    private(set) var switcherWindow: String?
    private(set) var inboxWindow: String?
    private let layout = WindowLayout(defaults: TestMode.defaults)

    @ObservationIgnored private var sessions: [String: ProjectSession] = [:]
    @ObservationIgnored private var openRequests: Task<Void, Never>?
    @ObservationIgnored private var openWindow: OpenWindowAction?
    private var launchProject = launchProjectID(CommandLine.arguments)
    @ObservationIgnored private var keyWindowID: String?
    @ObservationIgnored private let hosts = NSMapTable<NSString, NSWindow>.strongToWeakObjects()
    @ObservationIgnored private var captured: [String: ContinuousClock.Instant] = [:]
    @ObservationIgnored private var shownWindows: Set<String> = []
    @ObservationIgnored private var presence: [String: ProjectPresence] = [:]
    @ObservationIgnored private var restored = false
    @ObservationIgnored private var publishedTheme: (client: ObjectIdentifier, event: ThemeEvent)?

    @ObservationIgnored private(set) lazy var agentFinish = AgentFinishModel { [weak self] workspace, invocation in
        await self?.runCommand(invocation, in: workspace) ?? .failure(.notRunning)
    }

    @ObservationIgnored private(set) lazy var remoteDevices = RemoteDevicesModel(
        run: { [weak self] invocation in
            await self?.runCommand(invocation, in: self?.defaultWorkspace ?? "") ?? .failure(.notRunning)
        },
        confirm: { message in await Confirmation.ask(message, action: "Revoke", destructive: true) }
    )

    @ObservationIgnored private(set) lazy var notifications = Notifications(
        store: ClubmatePaths.resolve().state.appending(path: "notifications.json"),
        run: { [weak self] workspace, invocation in
            await self?.runCommand(invocation, in: workspace) ?? .failure(.notRunning)
        },
        focus: { [weak self] in self?.hubFocus ?? HubFocus(appActive: false, workspace: nil) },
        workspaces: { [weak self] in self?.openWorkspaces ?? [] },
        workspaceName: { [weak self] workspace in self?.project(workspace)?.name },
        reveal: { [weak self] workspace, session, owner in self?.reveal(workspace, session: session, owner: owner) }
    )

    var phase: ConnectionSupervisor.Phase { supervisor.phase }

    init() {
        let link = ModelLink()
        supervisor = ConnectionSupervisor { try await link.connect() }
        updates = Updates(supervisor: supervisor)
        link.model = self
        trackClient()
        trackTheme()
        trackProjects()
        syncSessions()
        trackTerminalFontSize()
        _ = notifications
        AppDelegate.willQuit = { [weak self] in self?.terminating() }
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.terminating() }
        }
    }

    func terminating() {
        layout.terminating()
        notifications.flush()
    }

    static func launchProjectID(_ arguments: [String]) -> String? {
        guard let index = arguments.firstIndex(of: "--open-project"), arguments.indices.contains(index + 1) else { return nil }
        return arguments[index + 1]
    }

    func launch(_ openWindow: OpenWindowAction) {
        self.openWindow = openWindow
        supervisor.start()
    }

    func retry() {
        supervisor.retry()
    }

    func window(_ id: String) -> LayoutWindow? {
        layout.window(id)
    }

    func toggleSidebar(_ store: ProjectUIStore?, in window: String?) {
        guard let store else { return }
        guard store.togglesChat else { return store.toggleSidebar() }
        if let window { layout.toggleChat(in: window) }
    }

    func showChat(in window: String) {
        layout.showChat(in: window)
    }

    func setChatWidth(_ width: Double, in window: String) {
        layout.setChatWidth(width, in: window)
    }

    var openWorkspaces: Set<String> {
        Set(layout.windows.flatMap(\.workspaces))
    }

    func claimWindow() -> String {
        let id = layout.windows.first { !shownWindows.contains($0.id) }?.id ?? layout.newWindow()
        shownWindows.insert(id)
        return id
    }

    func windowShown(_ window: String) {
        shownWindows.insert(window)
    }

    func open(_ projectID: String, in target: String? = nil) {
        let preferred = target ?? keyWindowID ?? layout.windows.last?.id
        snapshot(layout.windows.first { $0.workspaces.contains(projectID) }?.id ?? preferred, leaving: projectID)
        let window = layout.open(projectID, preferring: preferred)
        syncSessions()
        touch(projectID)
        openWindow?(value: window)
        if !TestMode.current { NSApp.activate() }
    }

    func activateFromSwitcher(_ projectID: String, in window: String) {
        hideSwitcher()
        hosts.object(forKey: window as NSString)?.contentView?.layoutSubtreeIfNeeded()
        activate(projectID, in: window)
    }

    func activate(_ projectID: String, in window: String) {
        snapshot(window, leaving: projectID)
        layout.activate(projectID, in: window)
        touchActive(in: window)
    }

    func next(in window: String) {
        guard (layout.window(window)?.workspaces.count ?? 0) > 1 else { return }
        snapshot(window)
        layout.next(in: window)
        touchActive(in: window)
    }

    func previous(in window: String) {
        guard (layout.window(window)?.workspaces.count ?? 0) > 1 else { return }
        snapshot(window)
        layout.previous(in: window)
        touchActive(in: window)
    }

    func closeWorkspace(_ projectID: String, in window: String) {
        layout.close(projectID, in: window)
        syncSessions()
    }

    func moveToNewWindow(_ projectID: String) {
        let window = layout.move(projectID, toNewWindow: true)
        syncSessions()
        openWindow?(value: window)
    }

    func windowBecameKey(_ window: String) {
        keyWindowID = window
        notifications.focusChanged()
    }

    var keyWorkspace: (projectID: String?, window: NSWindow?) {
        (keyWindowID.flatMap { layout.window($0)?.active }, keyWindowID.flatMap { hosts.object(forKey: $0 as NSString) })
    }

    private var hubFocus: HubFocus {
        guard let window = keyWindowID, hosts.object(forKey: window as NSString)?.isKeyWindow == true,
              let active = layout.window(window)?.active else { return HubFocus(appActive: NSApp.isActive, workspace: nil) }
        let store = sessions[active]?.store
        let section = store?.selectedSection
        let code = section == CodeSection.id
        return HubFocus(
            appActive: NSApp.isActive,
            workspace: active,
            sidebar: code ? nil : section.flatMap { store?.slots["\($0).sidebar"]?.tree },
            main: section.flatMap { store?.mainTree($0) },
            panel: code && layout.window(window)?.chatShown == true ? store?.slots[CodeSection.chatSlot]?.tree : nil
        )
    }

    func reveal(_ workspace: String, session: String?, owner: String?) {
        open(workspace)
        guard let session, let target = NotificationHub.reveal(workspace: workspace, session: session, owner: owner) else { return }
        Task {
            for _ in 0..<Self.revealAttempts {
                if let store = existingSession(for: workspace)?.store, store.connected {
                    store.show(section: target.section)
                    if let event = target.event, case .failure(let error) = await store.send(event) {
                        existingSession(for: workspace)?.banner.show(error.message)
                    }
                    if target.focusMain { store.requestFocus(section: target.section, slot: "\(target.section).main") }
                    return
                }
                try? await Task.sleep(for: .milliseconds(100))
            }
        }
    }

    private static let revealAttempts = 100

    func windowAttached(_ window: String, _ host: NSWindow) {
        hosts.setObject(host, forKey: window as NSString)
    }

    func toggleSwitcher(in window: String) {
        guard switcherWindow != window else { return hideSwitcher() }
        snapshot(window, throttled: false)
        switcherWindow = window
    }

    func hideSwitcher() {
        switcherWindow = nil
    }

    func toggleInbox(in window: String) {
        inboxWindow = inboxWindow == window ? nil : window
    }

    func hideInbox() {
        inboxWindow = nil
    }

    private func snapshot(_ window: String?, leaving target: String? = nil, throttled: Bool = true) {
        guard let window, switcherWindow != window, let active = layout.window(window)?.active, active != target,
              let session = sessions[active], session.overlay == nil,
              let host = hosts.object(forKey: window as NSString), !PaletteTextField.isShown(in: host) else { return }
        let now = ContinuousClock.now
        if throttled, let last = captured[window], now - last < .milliseconds(200) { return }
        captured[window] = now
        session.thumbnail = WorkspaceThumbnail.capture(host)
    }

    func windowClosed(_ window: String) {
        if keyWindowID == window { keyWindowID = nil }
        if switcherWindow == window { switcherWindow = nil }
        if inboxWindow == window { inboxWindow = nil }
        captured[window] = nil
        shownWindows.remove(window)
        layout.closeWindow(window)
        syncSessions()
    }

    func existingSession(for projectID: String) -> ProjectSession? {
        sessions[projectID]
    }

    private func session(for projectID: String) -> ProjectSession {
        if let existing = sessions[projectID] { return existing }
        let session = ProjectSession(projectID: projectID) { [weak self] in self?.project(projectID) }
        session.deck.fontSize = settings.terminalFontSize
        sessions[projectID] = session
        if let client = supervisor.client { session.adopt(client, supervisor, settings) }
        return session
    }

    private func syncSessions() {
        notifications.keepOpen(openWorkspaces)
        for workspace in Set(layout.windows.flatMap(\.workspaces)) where sessions[workspace] == nil {
            _ = session(for: workspace)
        }
        for (workspace, session) in sessions where SurfaceDeckPolicy.shouldCloseDeck(workspace: workspace, layout: layout) {
            sessions[workspace] = nil
            session.deck.closeAll()
            Task { await session.close(supervisor) }
        }
    }

    private func touchActive(in window: String) {
        if let active = layout.window(window)?.active { touch(active) }
    }

    private func touch(_ projectID: String) {
        notifications.focusChanged()
        guard let client = supervisor.client else { return }
        Task {
            try? await client.touchWorkspace(projectID)
            try? await projects?.load()
        }
    }

    var terminalFontSize: Double {
        settings.terminalFontSize ?? GhosttyApp.shared.configFontSize
    }

    func stepTerminalFont(_ delta: Double) {
        settings.terminalFontSize = AppSettings.fontSteps(current: terminalFontSize, delta: delta, range: AppSettings.terminalFontRange)
    }

    private func trackTerminalFontSize() {
        withObservationTracking { _ = settings.terminalFontSize } onChange: { [weak self] in
            Task { @MainActor in
                guard let self else { return }
                for session in self.sessions.values { session.deck.fontSize = self.settings.terminalFontSize }
                self.trackTerminalFontSize()
            }
        }
    }

    private func runCommand(_ invocation: CommandInvocation, in workspace: String?) async -> Result<JSONValue, ClubmateError> {
        guard let client = supervisor.client else { return .failure(.notRunning) }
        guard let projectID = workspace ?? defaultWorkspace else { return .failure(.noWorkspace) }
        do {
            return .success(try await client.commandRun(id: invocation.id, projectID: projectID, args: invocation.args))
        } catch let error as ClubmateError {
            return .failure(error)
        } catch {
            return .failure(.protocolError("\(error)"))
        }
    }

    private var defaultWorkspace: String? {
        keyWindowID.flatMap { layout.window($0)?.active } ?? layout.windows.lazy.compactMap(\.active).first ?? projects?.projects.first?.id
    }

    func project(_ id: String) -> Project? {
        projects?.projects.first { $0.id == id }
    }

    fileprivate func connect() async throws -> CoreClient {
        guard let cli = Bundle.main.resourceURL?.appending(path: "bin/clubmate") else {
            throw ClubmateError.coreStartFailed("bundled clubmate CLI not found")
        }
        let paths = ClubmatePaths.resolve()
        try await CoreLauncher(cliPath: cli, bundledVersion: Bundle.main.clubmateVersion).ensureRunning(paths: paths)
        let client = try await CoreClient.connect(paths: paths)
        client.confirmHandler = { [weak self] request, expiry in await self?.approve(request, expiry) ?? false }
        if TestMode.current {
            client.snapshotHandler = { try await TestHarness.shared.snapshot($0) }
            client.presentHandler = { try TestHarness.shared.present($0) }
        }
        do {
            let commands = try await client.commandList()
            let projects = ProjectsStore(client: client)
            try await projects.start()
            await self.projects?.stop()
            self.projects = projects
            self.commands = commands
            listen(client)
            restoreWindows(projects.projects)
            if let id = launchProject {
                launchProject = nil
                open(id)
            }
            return client
        } catch {
            await client.close()
            throw error
        }
    }

    private func approve(_ request: ConfirmRequest, _ expiry: ConfirmExpiry) async -> Bool {
        if TestMode.current { return false }
        let window = keyWindowID.flatMap { hosts.object(forKey: $0 as NSString) } ?? layout.windows.lazy.compactMap { self.hosts.object(forKey: $0.id as NSString) }.first
        let attention = NSApp.requestUserAttention(.criticalRequest)
        defer { NSApp.cancelUserAttentionRequest(attention) }
        let font = GhosttyApp.shared.theme.nsFont(size: settings.interfaceFontSize, bold: false)
        return await Confirmation.ask(request, font: font, window: window, expiry: expiry)
    }

    private func restoreWindows(_ listed: [Project]) {
        guard !restored else { return }
        restored = true
        layout.forget(missing: Set(layout.windows.flatMap(\.workspaces)).subtracting(listed.map(\.id)))
        syncSessions()
        for window in layout.windows where !shownWindows.contains(window.id) {
            openWindow?(value: window.id)
        }
    }

    private func trackProjects() {
        guard let listed = withObservationTracking({ projects?.projects }, onChange: { [weak self] in
            Task { @MainActor in self?.trackProjects() }
        }) else { return }
        let present = Set(listed.map(\.id))
        notifications.keep(present)
        let workspaces = Set(layout.windows.flatMap(\.workspaces))
        presence = presence.filter { workspaces.contains($0.key) }
        let gone = workspaces.filter { presence[$0, default: ProjectPresence()].observe(present.contains($0)) }
        guard !gone.isEmpty else { return }
        layout.forget(missing: gone)
        syncSessions()
    }

    private func trackClient() {
        guard let client = withObservationTracking({ supervisor.client }, onChange: { [weak self] in
            Task { @MainActor in self?.trackClient() }
        }) else { return }
        for session in sessions.values {
            session.adopt(client, supervisor, settings)
        }
        notifications.adopt(client)
        publishTheme()
    }

    private func trackTheme() {
        _ = withObservationTracking({ GhosttyApp.shared.theme }, onChange: { [weak self] in
            Task { @MainActor in self?.trackTheme() }
        })
        publishTheme()
    }

    private func publishTheme() {
        guard let client = supervisor.client, let event = GhosttyApp.shared.theme.event else { return }
        let sent = (client: ObjectIdentifier(client), event: event)
        if let publishedTheme, publishedTheme.client == sent.client, publishedTheme.event == event { return }
        publishedTheme = sent
        Task { [weak self] in
            do {
                try await client.busPublish(topic: ThemeEvent.topic, payload: event)
            } catch {
                guard let self, let current = self.publishedTheme, current.client == sent.client, current.event == event else { return }
                self.publishedTheme = nil
            }
        }
    }

    private func listen(_ client: CoreClient) {
        openRequests?.cancel()
        let requests = client.projectOpenRequests()
        openRequests = Task { [weak self] in
            for await id in requests {
                self?.open(id)
            }
        }
    }
}

extension AppModel {
    func openFolder(in window: String?) {
        guard let folders = Self.chooseFolders(prompt: "Open") else { return }
        perform { client in
            let id = try await client.openFolders(folders)
            self.open(id, in: window)
        }
    }

    func newHomeTerminal(in window: String?) {
        perform { client in
            let id = try await client.homeWorkspace()
            self.open(id, in: window)
            let session = self.existingSession(for: id)
            session?.store?.show(section: TerminalsSection.id)
            session?.autoNewRequest += 1
        }
    }

    func addFolder(to projectID: String) {
        guard let folders = Self.chooseFolders(prompt: "Add") else { return }
        perform { client in
            for folder in folders {
                try await client.addRepo(projectID, path: folder)
            }
        }
    }

    func rename(_ projectID: String) {
        Task {
            guard let name = await TextPrompt.ask("Rename Workspace", text: project(projectID)?.name ?? "", action: "Rename"),
                  !name.trimmingCharacters(in: .whitespaces).isEmpty else { return }
            perform { client in try await client.renameProject(projectID, name: name) }
        }
    }

    func remove(_ projectID: String) {
        Task {
            guard await Confirmation.ask(
                "Remove “\(project(projectID)?.name ?? "workspace")”?",
                detail: "Its terminals, editors and agents will stop, and it will be removed from Clubmate.",
                action: "Remove",
                destructive: true
            ) else { return }
            perform { client in try await client.deleteProject(projectID) }
        }
    }

    private func perform(_ action: @escaping (CoreClient) async throws -> Void) {
        guard let client = supervisor.client else {
            return Self.alert("Clubmate is not connected to its core yet.")
        }
        Task {
            do {
                try await action(client)
            } catch {
                Self.alert((error as? ClubmateError)?.message ?? "\(error)")
            }
        }
    }

    func installCommandLineTool() {
        guard let resources = Bundle.main.resourceURL else { return }
        let tool = CommandLineTool(flavor: ClaudeConnector.bundleFlavor(), resources: resources)
        Task {
            switch tool.state() {
            case .installed:
                return Self.inform("“\(tool.name)” is already installed", detail: "Run “\(tool.name) .” in a terminal to open a folder in Clubmate.")
            case .other(let existing):
                guard await Confirmation.ask("Replace \(tool.link)?", detail: "It currently points to \(existing).", action: "Replace", destructive: true) else { return }
            case .missing:
                break
            }
            var error: NSDictionary?
            NSAppleScript(source: tool.installScript)?.executeAndReturnError(&error)
            if let error {
                if error[NSAppleScript.errorNumber] as? Int != Self.userCanceled {
                    Self.alert(error[NSAppleScript.errorMessage] as? String ?? "Could not install \(tool.name).")
                }
                return
            }
            Self.inform("Installed “\(tool.name)”", detail: "Run “\(tool.name) .” in a terminal to open a folder in Clubmate.")
        }
    }

    private static let userCanceled = -128

    private static func inform(_ message: String, detail: String) {
        let alert = NSAlert()
        alert.messageText = message
        alert.informativeText = detail
        if !TestMode.current { alert.runModal() }
    }

    private static func chooseFolders(prompt: String) -> [String]? {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = true
        panel.showsHiddenFiles = true
        panel.prompt = prompt
        guard !TestMode.current, panel.runModal() == .OK, !panel.urls.isEmpty else { return nil }
        return panel.urls.map { $0.path(percentEncoded: false) }
    }

    static func alert(_ message: String) {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = message
        if !TestMode.current { alert.runModal() }
    }
}

@MainActor
private final class ModelLink {
    weak var model: AppModel?

    func connect() async throws -> CoreClient {
        guard let model else { throw ClubmateError.notRunning }
        return try await model.connect()
    }
}

enum WorkspaceOverlay: Equatable {
    case palette
    case pickFile
    case picker
}

struct PickerRequest {
    let model: FuzzyPickerModel
    let choose: @MainActor (PickerItem) -> Void
}

@MainActor
@Observable
final class ProjectSession {
    let projectID: String
    let deck = SurfaceDeck()
    let banner = EventBanner()
    var overlay: WorkspaceOverlay?
    private(set) var picker: PickerRequest?
    private(set) var filePicker: FilePickerModel?
    var thumbnail: NSImage?
    var autoNewRequest = 0
    @ObservationIgnored var autoNew = AutoNewGate()
    private(set) var store: ProjectUIStore?
    @ObservationIgnored private let lookup: @MainActor () -> Project?
    @ObservationIgnored private(set) lazy var worktrees = WorktreeModel(
        run: storeRun,
        project: { [weak self] in self?.lookup() },
        confirm: { message, detail in await Confirmation.ask(message, detail: detail, action: "Remove", destructive: true) },
        report: { [weak self] message in self?.banner.show(message) }
    )

    init(projectID: String, project: @escaping @MainActor () -> Project?) {
        self.projectID = projectID
        lookup = project
    }

    func newWorktree(chooseFolder: Bool) {
        let groups = worktrees.groups
        Task {
            var repo: String?
            if chooseFolder, groups.count > 1 {
                guard let index = await ChoicePrompt.ask("New Worktree in", options: groups.map(\.name), action: "Continue") else { return }
                repo = groups[index].repo
            }
            guard let branch = await TextPrompt.ask("New Worktree", text: "", action: "Create") else { return }
            await worktrees.create(branch, repo: repo)
        }
    }

    private var storeRun: WorktreeModel.Run {
        { [weak self] invocation in
            guard let store = self?.store else { return .failure(.notRunning) }
            return await store.run(invocation)
        }
    }

    func openPicker(_ placeholder: String, source: @escaping FuzzyPickerModel.Source, choose: @escaping @MainActor (PickerItem) -> Void) {
        picker?.model.cancel()
        picker = PickerRequest(model: FuzzyPickerModel(placeholder: placeholder, source: source), choose: choose)
        overlay = .picker
    }

    func chooseWorktree() {
        Task { [weak self] in
            guard let worktrees = self?.worktrees else { return }
            await worktrees.load()
            self?.openPicker("Switch to worktree or branch", source: worktrees.switcherSource) { [weak self] item in
                self?.pickWorktree(item)
            }
            guard let model = self?.picker?.model else { return }
            await worktrees.loadBranches()
            model.update(model.query)
        }
    }

    func chooseWorktreeToRemove() {
        Task {
            await worktrees.menuOpened()
            let trees = worktrees.linked
            guard !trees.isEmpty, let index = await ChoicePrompt.ask("Remove Worktree", options: trees.map(worktrees.label), action: "Continue") else { return }
            await worktrees.remove(repo: trees[index].repo, path: trees[index].path)
        }
    }

    func removeWorktree(_ tree: Worktree) {
        Task { await worktrees.remove(repo: tree.repo, path: tree.path) }
    }

    func pickWorktree(_ item: PickerItem) {
        Task { await worktrees.pick(item) }
    }

    func worktreeMenuOpened() {
        Task { await worktrees.menuOpened() }
    }

    func pickFile(_ target: @escaping @MainActor (String) -> Void) {
        filePicker?.cancel()
        filePicker = FilePickerModel(run: storeRun, dismiss: { [weak self] in self?.close(.pickFile) }, pick: target)
        overlay = .pickFile
    }

    func toggle(_ overlay: WorkspaceOverlay) {
        self.overlay = self.overlay == overlay ? nil : overlay
    }

    func close(_ overlay: WorkspaceOverlay) {
        guard self.overlay == overlay else { return }
        self.overlay = nil
        if overlay == .pickFile {
            filePicker?.cancel()
            filePicker = nil
        }
        if overlay == .picker {
            picker?.model.cancel()
            picker = nil
        }
    }

    func adopt(_ client: CoreClient, _ supervisor: ConnectionSupervisor, _ settings: AppSettings) {
        guard store == nil else { return }
        let store = ProjectUIStore(client: client, projectID: projectID) { [weak settings] in settings?.ordered($0) ?? $0 }
        self.store = store
        supervisor.register(store)
    }

    func run(_ invocation: CommandInvocation?) {
        guard let invocation, let store else { return }
        Task {
            if case .failure(let error) = await store.run(invocation) {
                banner.show(error.message)
            }
        }
    }

    func closeTerminal(_ close: CommandInvocation) {
        guard let store else { return }
        Task {
            switch await store.run(CommandInvocation(id: TerminalsSection.busyCommand, args: close.args)) {
            case .success(let result):
                if let message = TerminalsSection.busyMessage(result), !(await Confirmation.ask(message, action: "Close")) { return }
                run(close)
            case .failure(let error):
                banner.show(error.message)
            }
        }
    }

    func close(_ supervisor: ConnectionSupervisor) async {
        deck.closeAll()
        guard let store else { return }
        supervisor.unregister(store)
        await store.stop()
    }
}

extension Bundle {
    var clubmateVersion: String {
        BuildInfo.display(
            version: infoDictionary?["CFBundleShortVersionString"] as? String ?? "",
            commit: infoDictionary?["ClubmateCommit"] as? String ?? ""
        )
    }
}
