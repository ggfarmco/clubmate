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

    @ObservationIgnored private var openRequests: Task<Void, Never>?
    @ObservationIgnored private var gitChanges: Task<Void, Never>?
    @ObservationIgnored private var pluginChanges: Task<Void, Never>?
    private var launchProject = launchProjectID(CommandLine.arguments)
    @ObservationIgnored private var presence: [String: ProjectPresence] = [:]
    @ObservationIgnored private var publishedTheme: (client: ObjectIdentifier, event: ThemeEvent)?

    @ObservationIgnored private(set) lazy var sessions: SessionRegistry = SessionRegistry(supervisor: supervisor, settings: settings) { [weak self] id in
        self?.project(id)
    }

    @ObservationIgnored private(set) lazy var windows: WindowRouter = WindowRouter(
        sessions: sessions,
        opened: { [weak self] workspaces in self?.notifications.keepOpen(workspaces) },
        touched: { [weak self] projectID in self?.touch(projectID) },
        focused: { [weak self] in self?.notifications.focusChanged() }
    )

    @ObservationIgnored private lazy var focus: NotificationFocus = NotificationFocus(windows: windows, sessions: sessions)

    @ObservationIgnored private lazy var actions: WorkspaceActions = WorkspaceActions(supervisor: supervisor, windows: windows, sessions: sessions) { [weak self] id in
        self?.project(id)
    }

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
        focus: { [weak self] in self?.focus.current ?? HubFocus(appActive: false, workspace: nil) },
        workspaces: { [weak self] in self?.openWorkspaces ?? [] },
        workspaceName: { [weak self] workspace in self?.project(workspace)?.name },
        reveal: { [weak self] workspace, session, owner in self?.focus.reveal(workspace, session: session, owner: owner) }
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
        windows.syncSessions()
        _ = notifications
        AppDelegate.willQuit = { [weak self] in self?.terminating() }
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.terminating() }
        }
    }

    func terminating() {
        windows.terminating()
        notifications.flush()
    }

    static func launchProjectID(_ arguments: [String]) -> String? {
        guard let index = arguments.firstIndex(of: "--open-project"), arguments.indices.contains(index + 1) else { return nil }
        return arguments[index + 1]
    }

    func launch(_ openWindow: OpenWindowAction) {
        windows.launch(openWindow)
        supervisor.start()
    }

    func retry() {
        supervisor.retry()
    }

    var switcherWindow: String? { windows.switcherWindow }
    var inboxWindow: String? { windows.inboxWindow }
    var openWorkspaces: Set<String> { windows.openWorkspaces }
    var keyWorkspace: (projectID: String?, window: NSWindow?) { windows.keyWorkspace }

    func window(_ id: String) -> LayoutWindow? { windows.window(id) }
    func toggleSidebar(_ store: ProjectUIStore?, in window: String?) { windows.toggleSidebar(store, in: window) }
    func showChat(in window: String) { windows.showChat(in: window) }
    func setChatWidth(_ width: Double, in window: String) { windows.setChatWidth(width, in: window) }
    func claimWindow() -> String { windows.claimWindow() }
    func windowShown(_ window: String) { windows.windowShown(window) }
    func windowAttached(_ window: String, _ host: NSWindow) { windows.windowAttached(window, host) }
    func windowBecameKey(_ window: String) { windows.windowBecameKey(window) }
    func windowClosed(_ window: String) { windows.windowClosed(window) }
    func open(_ projectID: String, in target: String? = nil) { windows.open(projectID, in: target) }
    func activate(_ projectID: String, in window: String) { windows.activate(projectID, in: window) }
    func activateFromSwitcher(_ projectID: String, in window: String) { windows.activateFromSwitcher(projectID, in: window) }
    func next(in window: String) { windows.next(in: window) }
    func previous(in window: String) { windows.previous(in: window) }
    func closeWorkspace(_ projectID: String, in window: String) { windows.closeWorkspace(projectID, in: window) }
    func moveToNewWindow(_ projectID: String) { windows.moveToNewWindow(projectID) }
    func toggleSwitcher(in window: String) { windows.toggleSwitcher(in: window) }
    func hideSwitcher() { windows.hideSwitcher() }
    func toggleInbox(in window: String) { windows.toggleInbox(in: window) }
    func hideInbox() { windows.hideInbox() }

    func existingSession(for projectID: String) -> ProjectSession? { sessions.existing(projectID) }

    func openFolder(in window: String?) { actions.openFolder(in: window) }
    func newHomeTerminal(in window: String?) { actions.newHomeTerminal(in: window) }
    func addFolder(to projectID: String) { actions.addFolder(to: projectID) }
    func rename(_ projectID: String) { actions.rename(projectID) }
    func remove(_ projectID: String) { actions.remove(projectID) }
    func installCommandLineTool() { CLIInstaller.install() }

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
        windows.activeWorkspace ?? projects?.projects.first?.id
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
            client.keyHandler = { try await TestHarness.shared.key($0) }
        }
        do {
            let commands = try await client.commandList()
            let projects = ProjectsStore(client: client)
            try await projects.start()
            await self.projects?.stop()
            self.projects = projects
            self.commands = commands
            listen(client)
            windows.restore(projects.projects)
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
        let window = windows.attentionWindow
        let attention = NSApp.requestUserAttention(.criticalRequest)
        defer { NSApp.cancelUserAttentionRequest(attention) }
        let font = GhosttyApp.shared.theme.nsFont(size: settings.interfaceFontSize, bold: false)
        return await Confirmation.ask(request, font: font, window: window, expiry: expiry)
    }

    private func trackProjects() {
        guard let listed = withObservationTracking({ projects?.projects }, onChange: { [weak self] in
            Task { @MainActor in self?.trackProjects() }
        }) else { return }
        let present = Set(listed.map(\.id))
        notifications.keep(present)
        let workspaces = windows.openWorkspaces
        presence = presence.filter { workspaces.contains($0.key) }
        let gone = workspaces.filter { presence[$0, default: ProjectPresence()].observe(present.contains($0)) }
        guard !gone.isEmpty else { return }
        windows.forget(gone)
    }

    private func trackClient() {
        guard let client = withObservationTracking({ supervisor.client }, onChange: { [weak self] in
            Task { @MainActor in self?.trackClient() }
        }) else { return }
        sessions.adopt(client)
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
        gitChanges?.cancel()
        gitChanges = Task { [weak self] in
            await WorktreeModel.follow(client) { workspace in
                await self?.sessions.existing(workspace)?.worktrees.load()
            }
        }
        pluginChanges?.cancel()
        pluginChanges = Task { [weak self] in
            guard let events = try? await client.busEvents(pattern: Topic.pluginChanged) else { return }
            await self?.reloadCommands(client)
            for await _ in events {
                await self?.reloadCommands(client)
            }
        }
    }

    private func reloadCommands(_ client: CoreClient) async {
        guard let commands = try? await client.commandList(), !Task.isCancelled, commands != self.commands else { return }
        self.commands = commands
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

extension Bundle {
    var clubmateVersion: String {
        BuildInfo.display(
            version: infoDictionary?["CFBundleShortVersionString"] as? String ?? "",
            commit: infoDictionary?["ClubmateCommit"] as? String ?? ""
        )
    }
}
