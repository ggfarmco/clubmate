import AppKit
import Foundation
import Observation
import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class AppModel {
    let supervisor: ConnectionSupervisor
    let projectsSession = ProjectSession(projectID: "")
    private(set) var projects: ProjectsStore?
    private(set) var commands: [Command] = []

    @ObservationIgnored private var client: CoreClient?
    @ObservationIgnored private var sessions: [String: ProjectSession] = [:]
    @ObservationIgnored private var openRequests: Task<Void, Never>?
    @ObservationIgnored private var openWindow: OpenWindowAction?
    private var launchProject = launchProjectID(CommandLine.arguments)

    var phase: ConnectionSupervisor.Phase { supervisor.phase }

    init() {
        let link = ModelLink()
        supervisor = ConnectionSupervisor { try await link.connect() }
        link.model = self
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
        openWindow?(value: projectID)
        NSApp.activate()
    }

    func session(for projectID: String) -> ProjectSession {
        if let existing = sessions[projectID] { return existing }
        let session = ProjectSession(projectID: projectID)
        sessions[projectID] = session
        if let client { session.adopt(client, supervisor) }
        return session
    }

    func close(_ session: ProjectSession) {
        guard sessions[session.projectID] === session else { return }
        sessions[session.projectID] = nil
        Task { await session.close(supervisor) }
    }

    func project(_ id: String) -> Project? {
        projects?.projects.first { $0.id == id }
    }

    fileprivate func connect() async throws -> CoreClient {
        guard let cli = Bundle.main.resourceURL?.appending(path: "bin/clubmate") else {
            throw ClubmateError.coreStartFailed("bundled clubmate CLI not found")
        }
        let paths = ClubmatePaths.resolve()
        try await CoreLauncher(cliPath: cli).ensureRunning(paths: paths)
        let client = try await CoreClient.connect(paths: paths)
        do {
            let commands = try await client.commandList()
            let projects = ProjectsStore(client: client)
            try await projects.start()
            await self.projects?.stop()
            self.client = client
            self.projects = projects
            self.commands = commands
            for session in [projectsSession] + sessions.values {
                session.adopt(client, supervisor)
            }
            listen(client)
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
