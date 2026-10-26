import Foundation
import Observation

import ClubmateKit

@MainActor
@Observable
final class AppModel {
    enum Phase: Equatable {
        case starting
        case ready
        case reconnecting
        case failed(String)
    }

    private(set) var phase: Phase = .starting
    private(set) var projects: ProjectsStore?
    private(set) var commands: [Command] = []

    @ObservationIgnored private var client: CoreClient?
    @ObservationIgnored private var sessions: [String: ProjectSession] = [:]
    @ObservationIgnored private var watcher: Task<Void, Never>?
    @ObservationIgnored private var recovery: Task<Void, Never>?
    @ObservationIgnored private var launched = false
    @ObservationIgnored private var launchProject = launchProjectID(CommandLine.arguments)

    static func launchProjectID(_ arguments: [String]) -> String? {
        guard let index = arguments.firstIndex(of: "--open-project"), arguments.indices.contains(index + 1) else { return nil }
        return arguments[index + 1]
    }

    func takeLaunchProject() -> String? {
        defer { launchProject = nil }
        return launchProject
    }

    func launch() {
        guard !launched else { return }
        launched = true
        retry()
    }

    func retry() {
        guard recovery == nil, phase != .ready else { return }
        phase = .starting
        recovery = Task {
            do {
                let client = try await connect()
                ready(client)
            } catch {
                fail(error)
            }
        }
    }

    func connectionLost() {
        guard phase == .ready, recovery == nil else { return }
        phase = .reconnecting
        recovery = Task {
            var delay = Duration.milliseconds(500)
            var failure: any Error = ClubmateError.notRunning
            for _ in 0..<10 {
                try? await Task.sleep(for: delay)
                do {
                    let client = try await connect()
                    return ready(client)
                } catch {
                    failure = error
                    delay = min(delay * 2, .seconds(5))
                }
            }
            fail(failure)
        }
    }

    func session(for projectID: String) -> ProjectSession {
        if let existing = sessions[projectID] { return existing }
        let session = ProjectSession(projectID: projectID)
        sessions[projectID] = session
        if phase == .ready, let client { attach(session, client) }
        return session
    }

    func close(_ session: ProjectSession) {
        guard sessions[session.projectID] === session else { return }
        sessions[session.projectID] = nil
        Task { await session.close() }
    }

    func createProject(name: String, repos: [String]) async throws -> Project {
        guard let projects else { throw ClubmateError.notRunning }
        return try await projects.create(name: name, repos: repos)
    }

    func project(_ id: String) -> Project? {
        projects?.projects.first { $0.id == id }
    }

    private func connect() async throws -> CoreClient {
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
            for session in sessions.values {
                try await session.attach(client)
            }
            watcher?.cancel()
            await self.projects?.stop()
            let previous = self.client
            self.client = client
            self.projects = projects
            self.commands = commands
            await previous?.close()
            return client
        } catch {
            await client.close()
            throw error
        }
    }

    private func ready(_ client: CoreClient) {
        recovery = nil
        phase = .ready
        watch(client)
        for session in sessions.values where !session.attached {
            attach(session, client)
        }
    }

    private func fail(_ error: any Error) {
        recovery = nil
        phase = .failed((error as? ClubmateError)?.bannerText ?? "\(error)")
    }

    private func attach(_ session: ProjectSession, _ client: CoreClient) {
        Task {
            do {
                try await session.attach(client)
            } catch {
                connectionLost()
            }
        }
    }

    private func watch(_ client: CoreClient) {
        watcher?.cancel()
        let stream = client.subscribeNotifications()
        watcher = Task { [weak self] in
            for await _ in stream {}
            guard !Task.isCancelled else { return }
            self?.connectionLost()
        }
    }
}

@MainActor
@Observable
final class ProjectSession {
    let projectID: String
    let deck = SurfaceDeck()
    let banner = EventBanner()
    private(set) var store: ProjectUIStore?
    private(set) var failure: String?

    private(set) var closed = false

    var attached: Bool { store != nil }

    init(projectID: String) {
        self.projectID = projectID
    }

    func attach(_ client: CoreClient) async throws {
        guard !closed else { return }
        failure = nil
        do {
            if let store {
                try await store.reconnect(using: client)
            } else {
                let store = ProjectUIStore(client: client, projectID: projectID)
                self.store = store
                try await store.start()
            }
        } catch let error as ClubmateError where error == .notRunning || error == .timeout {
            throw error
        } catch {
            failure = (error as? ClubmateError)?.bannerText ?? "\(error)"
        }
        if closed { await store?.stop() }
    }

    func run(_ invocation: CommandInvocation?) {
        guard let invocation, let store else { return }
        Task {
            if case .failure(let error) = await store.run(invocation) {
                banner.show(error.bannerText)
            }
        }
    }

    func close() async {
        closed = true
        deck.closeAll()
        await store?.stop()
    }
}
