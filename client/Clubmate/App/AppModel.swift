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
    let projectsSession = ProjectSession(projectID: "")
    private(set) var projects: ProjectsStore?
    private(set) var commands: [Command] = []

    @ObservationIgnored private var sessions: [String: ProjectSession] = [:]
    @ObservationIgnored private var openRequests: Task<Void, Never>?
    @ObservationIgnored private var openWindow: OpenWindowAction?
    private var launchProject = launchProjectID(CommandLine.arguments)
    @ObservationIgnored private let openProjects = OpenProjects()
    @ObservationIgnored private var restored = false
    @ObservationIgnored private var navigationPending = true
    @ObservationIgnored private var navigationAsked = false

    var phase: ConnectionSupervisor.Phase { supervisor.phase }

    init() {
        let link = ModelLink()
        supervisor = ConnectionSupervisor { try await link.connect() }
        updates = Updates(supervisor: supervisor)
        link.model = self
        trackClient()
        trackNavigationKeys()
        trackTerminalFontSize()
        AppDelegate.willQuit = { [weak self] in self?.terminating() }
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.terminating() }
        }
    }

    func terminating() {
        openProjects.terminating()
    }

    static func launchProjectID(_ arguments: [String]) -> String? {
        guard let index = arguments.firstIndex(of: "--open-project"), arguments.indices.contains(index + 1) else { return nil }
        return arguments[index + 1]
    }

    var launchesProject: Bool { launchProject != nil }

    func launch(_ openWindow: OpenWindowAction) {
        self.openWindow = openWindow
        supervisor.start()
    }

    func retry() {
        supervisor.retry()
    }

    func open(_ projectID: String) {
        openProjects.opened(projectID)
        openWindow?(value: projectID)
        NSApp.activate()
    }

    func session(for projectID: String) -> ProjectSession {
        if let existing = sessions[projectID] { return existing }
        let session = ProjectSession(projectID: projectID)
        session.deck.fontSize = settings.terminalFontSize
        sessions[projectID] = session
        openProjects.opened(projectID)
        if let client = supervisor.client { session.adopt(client, supervisor) }
        return session
    }

    func close(_ session: ProjectSession) {
        guard sessions[session.projectID] === session else { return }
        sessions[session.projectID] = nil
        openProjects.closed(session.projectID)
        Task { await session.close(supervisor) }
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

    private func restoreWindows(_ listed: [Project]) {
        guard !restored else { return }
        restored = true
        let present = Set(listed.map(\.id))
        for id in openProjects.ids {
            if !present.contains(id) {
                openProjects.forget(id)
            } else if sessions[id] == nil, id != launchProject {
                open(id)
            }
        }
    }

    private func trackClient() {
        guard let client = withObservationTracking({ supervisor.client }, onChange: { [weak self] in
            Task { @MainActor in self?.trackClient() }
        }) else { return }
        for session in [projectsSession] + sessions.values {
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

@MainActor
private final class ModelLink {
    weak var model: AppModel?

    func connect() async throws -> CoreClient {
        guard let model else { throw ClubmateError.notRunning }
        return try await model.connect()
    }
}

@MainActor
@Observable
final class ProjectSession {
    let projectID: String
    let deck = SurfaceDeck()
    let banner = EventBanner()
    var paletteShown = false
    private(set) var store: ProjectUIStore?

    var isApp: Bool { projectID.isEmpty }

    init(projectID: String) {
        self.projectID = projectID
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
