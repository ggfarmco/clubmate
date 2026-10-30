import Foundation
import Observation

@MainActor
@Observable
public final class CompanionModel {
    public private(set) var computers: [Computer] = []
    public private(set) var connections: [String: ComputerConnection] = [:]
    public var inbox = Inbox()

    @ObservationIgnored public let device: DeviceIdentity
    @ObservationIgnored public let policy: HostPolicy
    @ObservationIgnored private let store: ComputerStore
    @ObservationIgnored private let transport: any HTTPTransport
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var agents: [String: AgentsStore] = [:]
    @ObservationIgnored private var observers: [String: UUID] = [:]

    public init(store: ComputerStore, transport: any HTTPTransport, device: DeviceIdentity, policy: HostPolicy = .tailnet, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.store = store
        self.transport = transport
        self.device = device
        self.policy = policy
        self.sleep = sleep
        computers = store.computers
        for computer in computers {
            attach(computer)
        }
    }

    public func start() {
        for connection in connections.values {
            connection.start()
        }
    }

    public func reconnectAll() {
        for connection in connections.values {
            connection.reconnectNow()
        }
    }

    public func connection(_ id: String) -> ComputerConnection? {
        connections[id]
    }

    public func pair(_ link: PairingLink) async throws -> Computer {
        let result = try await RemoteClient.pair(link, device: device, transport: transport, policy: policy)
        try store.save(result.computer, token: result.token)
        computers = store.computers
        detach(result.computer.id)
        attach(result.computer)
        connections[result.computer.id]?.start()
        return result.computer
    }

    public func remove(_ id: String) throws {
        detach(id)
        try store.remove(id)
        computers = store.computers
        inbox.remove(computer: id)
    }

    public func agents(computer: String, workspace: String) -> AgentsStore? {
        let key = computer + "|" + workspace
        if let existing = agents[key] { return existing }
        guard let connection = connections[computer] else { return nil }
        let store = AgentsStore(workspace: workspace, runner: connection)
        agents[key] = store
        return store
    }

    private func attach(_ computer: Computer) {
        let connection = ComputerConnection(computer: computer, token: store.token(for: computer.id), transport: transport, policy: policy, sleep: sleep)
        connections[computer.id] = connection
        observers[computer.id] = connection.observe { [weak self] signal in
            self?.route(signal, from: computer.id)
        }
    }

    private func detach(_ id: String) {
        connections[id]?.stop()
        if let observer = observers.removeValue(forKey: id) {
            connections[id]?.removeObserver(observer)
        }
        connections[id] = nil
        agents = agents.filter { !$0.key.hasPrefix(id + "|") }
    }

    private func route(_ signal: ConnectionSignal, from computer: String) {
        if case .event(let event) = signal, let item = InboxItem(event, computerID: computer, received: .now) {
            inbox.add(item)
        }
        for (key, store) in agents where key.hasPrefix(computer + "|") {
            store.handle(signal)
        }
    }
}
