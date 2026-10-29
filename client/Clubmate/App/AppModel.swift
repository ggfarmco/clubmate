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
    let settings = AppSettings()
    let claude = ClaudeIntegration()
    private(set) var projects: ProjectsStore?
    private(set) var commands: [Command] = []
    private(set) var switcherWindow: String?
    private let layout = WindowLayout()

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
    @ObservationIgnored private var navigationPending = true
    @ObservationIgnored private var navigationAsked = false
    @ObservationIgnored private(set) lazy var nvimConfig = NvimConfigModel(
        run: { [weak self] workspace, invocation in
            await self?.runCommand(invocation, in: workspace) ?? .failure(.notRunning)
        },
        workspaces: { [weak self] in await self?.nvimWorkspaces() ?? [] },
        displayed: { [weak self] in Set(self?.layout.windows.compactMap(\.active) ?? []) },
        confirm: { message in await Confirmation.ask(message, action: "Restart", destructive: true) }
    )

    var phase: ConnectionSupervisor.Phase { supervisor.phase }

    init() {
        let link = ModelLink()
        supervisor = ConnectionSupervisor { try await link.connect() }
        updates = Updates(supervisor: supervisor)
        link.model = self
        trackClient()
        trackProjects()
        syncSessions()
        trackNavigationKeys()
        trackTerminalFontSize()
        AppDelegate.willQuit = { [weak self] in self?.terminating() }
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.terminating() }
        }
    }

    func terminating() {
        layout.terminating()
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
        NSApp.activate()
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
    }

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
        let session = ProjectSession(projectID: projectID)
        session.deck.fontSize = settings.terminalFontSize
        sessions[projectID] = session
        if let client = supervisor.client { session.adopt(client, supervisor) }
        return session
    }

    private func syncSessions() {
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
        Task { await nvimConfig.activated(projectID) }
        guard let client = supervisor.client else { return }
        Task {
            try? await client.touchWorkspace(projectID)
            try? await projects?.load()
        }
    }

    func syncNavigationKeys() {
        guard navigationPending, let enabled = settings.navigationKeys,
              let session = sessions.values.first(where: { $0.store?.connected == true }),
              let store = session.store else { return }
        navigationPending = false
        Task {
            switch await store.run(.navigation(enabled: enabled, side: settings.sidebarSide)) {
            case .success(let result):
                if let message = CodeSection.skippedKeysMessage(result) { session.banner.show(message) }
            case .failure(let error):
                navigationPending = true
                session.banner.show(error.message)
            }
        }
    }

    func askNavigationKeys() {
        guard settings.navigationKeys == nil, !navigationAsked else { return }
        navigationAsked = true
        Task {
            settings.navigationKeys = await Confirmation.ask("Use Ctrl+H/J/K/L to move between nvim and the sidebar?", action: "Enable", cancel: "Not now")
        }
    }

    private func trackNavigationKeys() {
        withObservationTracking { _ = (settings.navigationKeys, settings.sidebarSide) } onChange: { [weak self] in
            Task { @MainActor in
                guard let self else { return }
                self.navigationPending = true
                self.syncNavigationKeys()
                self.trackNavigationKeys()
            }
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
        let active = keyWindowID.flatMap { layout.window($0)?.active } ?? layout.windows.lazy.compactMap(\.active).first
        guard let projectID = workspace ?? active ?? projects?.projects.first?.id else { return .failure(NvimConfigModel.noWorkspace) }
        do {
            return .success(try await client.commandRun(id: invocation.id, projectID: projectID, args: invocation.args))
        } catch let error as ClubmateError {
            return .failure(error)
        } catch {
            return .failure(.protocolError("\(error)"))
        }
    }

    private func nvimWorkspaces() async -> [NvimWorkspace] {
        guard let client = supervisor.client, let sessions = try? await client.call(method: "session.list").arrayValue else { return [] }
        let ids = sessions.compactMap { session -> String? in
            guard let fields = session.objectValue, fields["owner"]?.stringValue == CodeSection.id,
                  fields["state"]?.stringValue == "running" else { return nil }
            return fields["projectId"]?.stringValue
        }
        return Array(Set(ids)).sorted().map { NvimWorkspace(id: $0, name: project($0)?.name ?? $0) }
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
        let window = keyWindowID.flatMap { hosts.object(forKey: $0 as NSString) } ?? layout.windows.lazy.compactMap { self.hosts.object(forKey: $0.id as NSString) }.first
        let attention = NSApp.requestUserAttention(.criticalRequest)
        defer { NSApp.cancelUserAttentionRequest(attention) }
        return await Confirmation.ask(request.message, detail: request.detail, action: "Allow", cancel: "Deny", destructive: true, window: window, expiry: expiry)
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
            session.adopt(client, supervisor)
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

    private static func chooseFolders(prompt: String) -> [String]? {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = true
        panel.prompt = prompt
        guard panel.runModal() == .OK, !panel.urls.isEmpty else { return nil }
        return panel.urls.map { $0.path(percentEncoded: false) }
    }

    private static func alert(_ message: String) {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = message
        alert.runModal()
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
    case goToFile
    case findInFiles
}

@MainActor
@Observable
final class ProjectSession {
    let projectID: String
    let deck = SurfaceDeck()
    let banner = EventBanner()
    var overlay: WorkspaceOverlay?
    private(set) var findInFiles: FindInFilesModel?
    var thumbnail: NSImage?
    var autoNewRequest = 0
    @ObservationIgnored var autoNew = AutoNewGate()
    private(set) var store: ProjectUIStore?

    init(projectID: String) {
        self.projectID = projectID
    }

    func toggle(_ overlay: WorkspaceOverlay) {
        if overlay == .findInFiles, findInFiles == nil { findInFiles = makeFindInFiles() }
        self.overlay = self.overlay == overlay ? nil : overlay
    }

    private func makeFindInFiles() -> FindInFilesModel {
        FindInFilesModel(
            run: { [weak self] invocation in
                guard let store = self?.store else { return .failure(.notRunning) }
                return await store.run(invocation)
            },
            dismiss: { [weak self] in self?.close(.findInFiles) },
            report: { [weak self] message in self?.banner.show(message) },
            opened: { [weak self] in self?.store?.requestFocus(section: CodeSection.id, slot: CodeSection.mainSlot) }
        )
    }

    func close(_ overlay: WorkspaceOverlay) {
        if self.overlay == overlay { self.overlay = nil }
    }

    func adopt(_ client: CoreClient, _ supervisor: ConnectionSupervisor) {
        guard store == nil else { return }
        let store = ProjectUIStore(client: client, projectID: projectID)
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
        findInFiles?.stop()
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
