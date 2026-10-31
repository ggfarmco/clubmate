import Foundation
import Observation

@MainActor
@Observable
public final class CompanionModel {
    public private(set) var computers: [Computer] = []
    public private(set) var connections: [String: ComputerConnection] = [:]
    public var inbox = Inbox()
    @ObservationIgnored public var posted: (@MainActor (InboxItem) -> Void)?

    @ObservationIgnored public let device: DeviceIdentity
    @ObservationIgnored public let policy: HostPolicy
    @ObservationIgnored private let store: ComputerStore
    @ObservationIgnored private let transport: any HTTPTransport
    @ObservationIgnored private let cursors: UserDefaults?
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var agents: [String: AgentsStore] = [:]
    @ObservationIgnored private var observers: [String: UUID] = [:]
    @ObservationIgnored private var lastSignal = ContinuousClock.now
    @ObservationIgnored private var receiving = 0

    public init(store: ComputerStore, transport: any HTTPTransport, device: DeviceIdentity, policy: HostPolicy = .tailnet, cursors: UserDefaults? = nil, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.store = store
        self.transport = transport
        self.device = device
        self.policy = policy
        self.cursors = cursors
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

    public func catchUp(quiet: Duration = .seconds(2), timeout: Duration = .seconds(20)) async {
        for connection in connections.values {
            connection.stop()
            connection.start()
        }
        lastSignal = .now
        let deadline = ContinuousClock.now + timeout
        while ContinuousClock.now < deadline, !Task.isCancelled {
            let settled = connections.values.allSatisfy { $0.state == .online || $0.state == .unauthorized }
            if settled, receiving == 0, ContinuousClock.now - lastSignal >= quiet { return }
            try? await Task.sleep(for: .milliseconds(50))
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
        cursors?.removeObject(forKey: Self.cursorKey(id))
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
        let saved = cursors?.object(forKey: Self.cursorKey(computer.id)) as? Int
        let connection = ComputerConnection(computer: computer, token: store.token(for: computer.id), transport: transport, policy: policy, lastEventID: saved, sleep: sleep)
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

    private static func cursorKey(_ computer: String) -> String {
        "companion.cursor." + computer
    }

    private func route(_ signal: ConnectionSignal, from computer: String) {
        lastSignal = .now
        if case .event = signal, let seq = connections[computer]?.lastEventID {
            cursors?.set(seq, forKey: Self.cursorKey(computer))
        }
        if case .event(let event) = signal, let item = InboxItem(event, computerID: computer, received: .now) {
            receiving += 1
            Task {
                await receive(item)
                receiving -= 1
                lastSignal = .now
            }
        }
        for (key, store) in agents where key.hasPrefix(computer + "|") {
            store.handle(signal)
        }
    }

    private func receive(_ item: InboxItem) async {
        let allowed = await policy(item).allows(item.kind)
        guard inbox.add(item) else { return }
        if allowed {
            posted?(item)
        } else {
            inbox.markRead(item.id)
        }
    }

    private func policy(_ item: InboxItem) async -> NotificationPolicy {
        guard let session = item.session, AgentSessionID.matches(session), let connection = connections[item.computerID],
              let sessions = try? await connection.command(AgentCommand.sessions, workspace: item.workspace, args: nil) else { return .all }
        return NotificationPolicy(session: session, in: sessions)
    }
}
