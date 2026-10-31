import Foundation
import Observation

@MainActor
@Observable
public final class WorkspacesStore {
    public private(set) var workspaces: [Workspace] = []

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
            let events = try await client.busEvents(pattern: "workspace.*")
            guard generation == self.generation else { return }
            listener = Task { [weak self] in
                for await _ in events {
                    try? await self?.load()
                }
            }
            let workspaces = try await client.listWorkspaces()
            guard generation == self.generation else { return }
            self.workspaces = workspaces
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
        workspaces = try await client.listWorkspaces()
    }

    public func create(name: String, repos: [String]) async throws -> Workspace {
        let workspace = try await client.createWorkspace(name: name, repos: repos)
        try await load()
        return workspace
    }
}

public struct WorkspacePresence: Sendable, Equatable {
    public private(set) var seen = false

    public init() {}

    public mutating func observe(_ present: Bool) -> Bool {
        if present { seen = true }
        return seen && !present
    }
}
