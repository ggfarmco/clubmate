import Foundation
import Observation

@MainActor
@Observable
public final class ProjectsStore {
    public private(set) var projects: [Project] = []

    @ObservationIgnored private let client: CoreClient
    @ObservationIgnored private var listener: Task<Void, Never>?
    @ObservationIgnored private var generation = 0

    public init(client: CoreClient) {
        self.client = client
    }

    public func start() async throws {
        guard listener == nil else { return }
        let generation = self.generation
        do {
            let events = try await client.busEvents(pattern: "project.*")
            guard generation == self.generation else { return }
            listener = Task { [weak self] in
                for await _ in events {
                    try? await self?.load()
                }
            }
            let projects = try await client.listProjects()
            guard generation == self.generation else { return }
            self.projects = projects
        } catch {
            if generation == self.generation { halt() }
            throw error
        }
    }

    public func stop() async {
        halt()
    }

    private func halt() {
        generation += 1
        listener?.cancel()
        listener = nil
    }

    public func load() async throws {
        projects = try await client.listProjects()
    }

    public func create(name: String, repos: [String]) async throws -> Project {
        let project = try await client.createProject(name: name, repos: repos)
        try await load()
        return project
    }
}

public struct ProjectPresence: Sendable, Equatable {
    public private(set) var seen = false

    public init() {}

    public mutating func observe(_ present: Bool) -> Bool {
        if present { seen = true }
        return seen && !present
    }
}
