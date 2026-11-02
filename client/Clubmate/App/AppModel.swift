import AppKit
import Foundation
import Observation
import SwiftUI
import WebKit

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
    @ObservationIgnored private var browserClosing: [Task<Void, Never>] = []
    @ObservationIgnored private var usageChanges: Task<Void, Never>?
    @ObservationIgnored private var remoteChanges: Task<Void, Never>?
    @ObservationIgnored private var voiceChanges: Task<Void, Never>?
    @ObservationIgnored private var voiceSettingsChanges: Task<Void, Never>?
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

    @ObservationIgnored private lazy var voiceHotkeys = VoiceHotkeys { [weak self] in
        guard let self, voiceSettings.isOn else { return nil }
        return windows.activeWorkspace.flatMap { self.sessions.existing($0)?.voice }
    }

    @ObservationIgnored private(set) lazy var actions: WorkspaceActions = WorkspaceActions(supervisor: supervisor, windows: windows, sessions: sessions) { [weak self] id in
        self?.workspace(id)
    }

    @ObservationIgnored private(set) lazy var agentFinish = AgentFinishModel { [weak self] workspace, invocation in
        await self?.runCommand(invocation, in: workspace) ?? .failure(.notRunning)
    }

    @ObservationIgnored private(set) lazy var remoteDevices = makeRemoteDevices()

    @ObservationIgnored private(set) lazy var voiceSettings = VoiceSettingsModel { [weak self] invocation in
        await self?.runCommand(invocation, in: "") ?? .failure(.notRunning)
    }

    @ObservationIgnored private(set) lazy var usage = SubscriptionUsageModel { [weak self] invocation in
        await self?.runCommand(invocation, in: "") ?? .failure(.notRunning)
    }

    @ObservationIgnored private(set) lazy var notifications = Notifications(
        store: ClubmatePaths.resolve().state.appending(path: "notifications.json"),
        run: { [weak self] workspace, invocation in
            await self?.runCommand(invocation, in: workspace) ?? .failure(.notRunning)
        },
        focus: { [weak self] in self?.focus.current ?? HubFocus(appActive: false, workspace: nil) },
        workspaces: { [weak self] in self?.windows.openWorkspaces ?? [] },
        workspaceName: { [weak self] workspace in self?.workspace(workspace)?.name },
        reveal: { [weak self] workspace, session, owner in self?.focus.reveal(workspace, session: session, owner: owner) }
    )

    @ObservationIgnored private lazy var browserHost = BrowserWindowHost(
        theme: { [weak self] in GhosttyApp.shared.theme.scaled(self?.settings.interfaceFontSize ?? 12) },
        title: { [weak self] owner in await self?.sessionTitle(owner) }
    )

    @ObservationIgnored private var agentBrowser: AgentBrowser?

    private var browser: AgentBrowser {
        if let agentBrowser { return agentBrowser }
        let browser = AgentBrowser(
            host: browserHost,
            dataStore: WKWebsiteDataStore(forIdentifier: BrowserWindowHost.dataStoreID(flavor: ClubmatePaths.resolve().flavor)),
            userAgent: BrowserWindowHost.userAgent(),
            screenshots: FileManager.default.temporaryDirectory.appending(path: "clubmate-browser")
        )
        agentBrowser = browser
        return browser
    }

    var phase: ConnectionSupervisor.Phase { supervisor.phase }

    init() {
        let link = ModelLink()
        supervisor = ConnectionSupervisor(connect: { try await link.connect() }, stores: { link.model?.sessions.stores ?? [] }, browser: true)
        updates = Updates(supervisor: supervisor)
        link.model = self
        trackClient()
        trackTheme()
        trackWorkspaces()
        windows.syncSessions()
        _ = notifications
        voiceHotkeys.install()
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

    func revealSession(_ session: String, in workspace: String) {
        focus.reveal(workspace, session: session, owner: nil)
    }

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
        client.browserHandler = { [weak self] call in
            guard let self else { throw ClubmateError.notRunning }
            return try await self.browser.handle(call)
        }
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
                windows.open(id)
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
                self?.windows.open(id)
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
        remoteChanges?.cancel()
        remoteChanges = Task { [weak self] in
            guard let remote = self?.remoteDevices else { return }
            await remote.follow(client)
        }
        voiceSettingsChanges?.cancel()
        voiceSettingsChanges = Task { [weak self] in
            guard let voiceSettings = self?.voiceSettings else { return }
            await voiceSettings.follow(client)
        }
        voiceChanges?.cancel()
        voiceChanges = Task { [weak self] in
            guard let events = try? await client.busEvents(pattern: VoiceController.changedTopic) else { return }
            for await event in events {
                let workspace = event.payload["workspace"]?.stringValue ?? event.workspace
                self?.sessions.existing(workspace)?.voice.busEvent(event)
            }
        }
        pluginChanges?.cancel()
        pluginChanges = Task { [weak self] in
            guard let events = try? await client.busEvents(pattern: Topic.pluginChanged) else { return }
            await self?.reloadCommands(client)
            for await _ in events {
                await self?.reloadCommands(client)
                await self?.usage.load()
                await self?.remoteDevices.load()
                await self?.voiceSettings.load()
            }
        }
        browserClosing.forEach { $0.cancel() }
        browserClosing = [Topic.agentsChanged, Topic.sessionRemoved].map { topic in
            Task { [weak self] in
                guard let events = try? await client.busEvents(pattern: topic) else { return }
                for await event in events {
                    if let owner = AgentBrowser.owner(closedBy: event) { self?.agentBrowser?.close(owner: owner) }
                }
            }
        }
    }

    private func sessionTitle(_ owner: String) async -> String? {
        for workspace in windows.openWorkspaces {
            guard case .success(let sessions) = await runCommand(CommandInvocation(id: "agents.sessions"), in: workspace) else { continue }
            if let title = sessions.arrayValue?.first(where: { $0["id"]?.stringValue == owner })?["title"]?.stringValue { return title }
        }
        return nil
    }

    private func makeRemoteDevices() -> RemoteDevicesModel {
        #if DEBUG
        if CommandLine.arguments.contains("--remote-fixture") { return DevicesPreview.model("devices") }
        #endif
        return RemoteDevicesModel(
            run: { [weak self] invocation in
                await self?.runCommand(invocation, in: self?.defaultWorkspace ?? "") ?? .failure(.notRunning)
            },
            confirm: { message in await Confirmation.ask(message, action: "Revoke", destructive: true) }
        )
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
