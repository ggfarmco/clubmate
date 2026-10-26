import Foundation
import Observation

@MainActor
@Observable
public final class ProjectsStore {
    public private(set) var projects: [Project] = []

    @ObservationIgnored private let client: CoreClient
    @ObservationIgnored private var subscription: String?
    @ObservationIgnored private var listener: Task<Void, Never>?

    public init(client: CoreClient) {
        self.client = client
    }

    public func start() async throws {
        let stream = client.subscribeNotifications()
        listener = Task { [weak self] in
            for await notification in stream {
                await self?.receive(notification)
            }
        }
        do {
            try await subscribe()
        } catch {
            listener?.cancel()
            listener = nil
            throw error
        }
    }

    public func stop() async {
        listener?.cancel()
        listener = nil
        if let subscription {
            try? await client.busUnsubscribe(subscription)
        }
        subscription = nil
    }

    public func load() async throws {
        projects = try await client.listProjects()
    }

    public func create(name: String, repos: [String]) async throws -> Project {
        let project = try await client.createProject(name: name, repos: repos)
        try await load()
        return project
    }

    private func subscribe() async throws {
        subscription = try await client.busSubscribe(pattern: "project.*", after: .max)
        try await load()
    }

    private func receive(_ notification: RPCNotification) async {
        guard let subscription,
              let params = try? notification.params.decode(BusNotification.self),
              params.subscription == subscription
        else { return }
        switch notification.method {
        case "bus.event":
            try? await load()
        case "bus.closed":
            self.subscription = nil
            try? await subscribe()
        default:
            break
        }
    }
}

private struct BusNotification: Decodable {
    let subscription: String
}
