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
    private(set) var workspaceStore: WorkspacesStore?
    private(set) var commands: [Command] = []

    @ObservationIgnored private var openRequests: Task<Void, Never>?
    @ObservationIgnored private var gitChanges: Task<Void, Never>?
    @ObservationIgnored private var pluginChanges: Task<Void, Never>?
    @ObservationIgnored private var usageChanges: Task<Void, Never>?
    private var launchWorkspace = launchWorkspaceID(CommandLine.arguments)
    @ObservationIgnored private var presence: [String: WorkspacePresence] = [:]
    @ObservationIgnored private var publishedTheme: (client: ObjectIdentifier, event: ThemeEvent)?

    @ObservationIgnored private(set) lazy var sessions: SessionRegistry = SessionRegistry(supervisor: supervisor, settings: settings) { [weak self] id in
        self?.workspace(id)
    }

    @ObservationIgnored private(set) lazy var windows: WindowRouter = WindowRouter(
        sessions: sessions,
        opened: { [weak self] workspaces in self?.notifications.keepOpen(workspaces) },
        touched: { [weak self] workspaceID in self?.touch(workspaceID) },
        focused: { [weak self] in self?.notifications.focusChanged() }
    )

    @ObservationIgnored private lazy var focus: NotificationFocus = NotificationFocus(windows: windows, sessions: sessions)

    @ObservationIgnored private lazy var actions: WorkspaceActions = WorkspaceActions(supervisor: supervisor, windows: windows, sessions: sessions) { [weak self] id in
        self?.workspace(id)
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

    @ObservationIgnored private(set) lazy var usage = SubscriptionUsageModel { [weak self] invocation in
        await self?.runCommand(invocation, in: "") ?? .failure(.notRunning)
    }

    @ObservationIgnored private(set) lazy var notifications = Notifications(
        store: ClubmatePaths.resolve().state.appending(path: "notifications.json"),
        run: { [weak self] workspace, invocation in
            await self?.runCommand(invocation, in: workspace) ?? .failure(.notRunning)
        },
        focus: { [weak self] in self?.focus.current ?? HubFocus(appActive: false, workspace: nil) },
        workspaces: { [weak self] in self?.openWorkspaces ?? [] },
        workspaceName: { [weak self] workspace in self?.workspace(workspace)?.name },
        reveal: { [weak self] workspace, session, owner in self?.focus.reveal(workspace, session: session, owner: owner) }
    )

    var phase: ConnectionSupervisor.Phase { supervisor.phase }

    init() {
        let link = ModelLink()
        supervisor = ConnectionSupervisor(connect: { try await link.connect() }, stores: { link.model?.sessions.stores ?? [] })
        updates = Updates(supervisor: supervisor)
        link.model = self
        trackClient()
        trackTheme()
        trackWorkspaces()
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

    static func launchWorkspaceID(_ arguments: [String]) -> String? {
        guard let index = arguments.firstIndex(of: "--open-workspace"), arguments.indices.contains(index + 1) else { return nil }
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
    var keyWorkspace: (workspaceID: String?, window: NSWindow?) { windows.keyWorkspace }

    func window(_ id: String) -> LayoutWindow? { windows.window(id) }
    func claimWindow() -> String { windows.claimWindow() }
    func windowShown(_ window: String) { windows.windowShown(window) }
    func windowAttached(_ window: String, _ host: NSWindow) { windows.windowAttached(window, host) }
    func windowBecameKey(_ window: String) { windows.windowBecameKey(window) }
    func windowClosed(_ window: String) { windows.windowClosed(window) }
    func open(_ workspaceID: String, in target: String? = nil) { windows.open(workspaceID, in: target) }
    func activate(_ workspaceID: String, in window: String) { windows.activate(workspaceID, in: window) }
    func activateFromSwitcher(_ workspaceID: String, in window: String) { windows.activateFromSwitcher(workspaceID, in: window) }
    func next(in window: String) { windows.next(in: window) }
    func previous(in window: String) { windows.previous(in: window) }
    func closeWorkspace(_ workspaceID: String, in window: String) { windows.closeWorkspace(workspaceID, in: window) }
    func moveToNewWindow(_ workspaceID: String) { windows.moveToNewWindow(workspaceID) }
    func toggleSwitcher(in window: String) { windows.toggleSwitcher(in: window) }
    func hideSwitcher() { windows.hideSwitcher() }
    func toggleInbox(in window: String) { windows.toggleInbox(in: window) }
    func hideInbox() { windows.hideInbox() }

    func existingSession(for workspaceID: String) -> WorkspaceSession? { sessions.existing(workspaceID) }

    func openFolder(in window: String?) { actions.openFolder(in: window) }
    func newHomeTerminal(in window: String?) { actions.newHomeTerminal(in: window) }
    func addFolder(to workspaceID: String) { actions.addFolder(to: workspaceID) }
    func rename(_ workspaceID: String) { actions.rename(workspaceID) }
    func remove(_ workspaceID: String) { actions.remove(workspaceID) }
    func installCommandLineTool() { CLIInstaller.install() }

    private func touch(_ workspaceID: String) {
        notifications.focusChanged()
        guard let client = supervisor.client else { return }
        Task {
            try? await client.touchWorkspace(workspaceID)
            try? await workspaceStore?.load()
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
        guard let workspaceID = workspace ?? defaultWorkspace else { return .failure(.noWorkspace) }
        do {
            return .success(try await client.commandRun(id: invocation.id, workspaceID: workspaceID, args: invocation.args))
        } catch let error as ClubmateError {
            return .failure(error)
        } catch {
            return .failure(.protocolError("\(error)"))
        }
    }

    private var defaultWorkspace: String? {
        windows.activeWorkspace ?? workspaceStore?.workspaces.first?.id
    }

    func workspace(_ id: String) -> Workspace? {
        workspaceStore?.workspaces.first { $0.id == id }
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
            let store = WorkspacesStore(client: client)
            try await store.start()
            await workspaceStore?.stop()
            workspaceStore = store
            self.commands = commands
            listen(client)
            windows.restore(store.workspaces)
            if let id = launchWorkspace {
                launchWorkspace = nil
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

    private func trackWorkspaces() {
        guard let listed = withObservationTracking({ workspaceStore?.workspaces }, onChange: { [weak self] in
            Task { @MainActor in self?.trackWorkspaces() }
        }) else { return }
        let present = Set(listed.map(\.id))
        notifications.keep(present)
        let workspaces = windows.openWorkspaces
        presence = presence.filter { workspaces.contains($0.key) }
        let gone = workspaces.filter { presence[$0, default: WorkspacePresence()].observe(present.contains($0)) }
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
        let requests = client.workspaceOpenRequests()
        openRequests = Task { [weak self] in
            for await id in requests {
                self?.open(id)
            }
        }
        gitChanges?.cancel()
        gitChanges = Task { [weak self] in
            await WorktreeModel.follow(client) { event in
                guard let worktrees = self?.sessions.existing(event.workspace)?.worktrees else { return }
                Task { await worktrees.receive(event) }
            }
        }
        usageChanges?.cancel()
        usageChanges = Task { [weak self] in
            guard let usage = self?.usage else { return }
            await usage.follow(client)
        }
        pluginChanges?.cancel()
        pluginChanges = Task { [weak self] in
            guard let events = try? await client.busEvents(pattern: Topic.pluginChanged) else { return }
            await self?.reloadCommands(client)
            for await _ in events {
                await self?.reloadCommands(client)
                await self?.usage.load()
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
