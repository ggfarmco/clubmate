import Foundation
import Observation

@MainActor
@Observable
public final class CompanionModel {
    public private(set) var computers: [Computer] = []
    public private(set) var connections: [String: ComputerConnection] = [:]
    public var inbox = Inbox()
    @ObservationIgnored public var posted: (@MainActor (InboxItem) -> Void)?
    @ObservationIgnored public var withdrawn: (@MainActor ([String]) -> Void)?

    @ObservationIgnored public let device: DeviceIdentity
    @ObservationIgnored public let policy: HostPolicy
    @ObservationIgnored private let store: ComputerStore
    @ObservationIgnored private let transport: any HTTPTransport
    @ObservationIgnored private let cursors: UserDefaults?
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var agents: [String: AgentsStore] = [:]
    @ObservationIgnored private var backlogs: [String: BacklogStore] = [:]
    @ObservationIgnored private var observers: [String: UUID] = [:]
    @ObservationIgnored private var lastSignal = ContinuousClock.now
    @ObservationIgnored private var receiving = 0
    @ObservationIgnored private var resolvedAt: [String: Int] = [:]

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
        sendMacs(excluding: result.computer.id)
        return result.computer
    }

    public func remove(_ id: String) throws {
        detach(id)
        cursors?.removeObject(forKey: Self.cursorKey(id))
        try store.remove(id)
        computers = store.computers
        inbox.remove(computer: id)
        sendMacs(excluding: nil)
    }

    public var namesMacs: Bool {
        computers.count > 1
    }

    public func tag(_ computer: String) -> MacTag? {
        guard namesMacs else { return nil }
        return (connections[computer]?.computer ?? computers.first { $0.id == computer })?.tag
    }

    public func macs(excluding id: String?) -> [MacTag] {
        computers.filter { $0.id != id }.map { (connections[$0.id]?.computer ?? $0).tag }
    }

    public func setColour(_ colour: MacColour, computer: String) async throws {
        guard let connection = connections[computer] else { return }
        identified(try await connection.setColour(colour))
    }

    private func identified(_ computer: Computer) {
        let before = computers.first { $0.id == computer.id }?.tag
        try? store.identify(computer.id, as: ComputerInfo(id: computer.id, name: computer.name, colour: computer.colour))
        computers = store.computers
        if computers.first(where: { $0.id == computer.id })?.tag != before { sendMacs(excluding: computer.id) }
    }

    private func sendMacs(excluding id: String?) {
        for connection in connections.values where connection.computer.id != id && connection.state == .online {
            let list = macs(excluding: connection.computer.id)
            Task { await connection.sendMacs(list) }
        }
    }

    public func agents(computer: String, workspace: String) -> AgentsStore? {
        let key = computer + "|" + workspace
        if let existing = agents[key] { return existing }
        guard let connection = connections[computer] else { return nil }
        let store = AgentsStore(workspace: workspace, runner: connection)
        agents[key] = store
        return store
    }

    public func needsYou() -> [NeedsYouItem] {
        let known = computers.compactMap { connections[$0.id] }.filter { $0.state != .unauthorized }
        let offline = Set(known.filter { $0.state != .online }.map(\.computer.id))
        return NeedsYouItem.collect(known.flatMap { connection in
            connection.workspaces.compactMap { workspace in
                agents(computer: connection.computer.id, workspace: workspace.id).map { (connection.computer.id, tag(connection.computer.id), workspace, $0.sessions) }
            }
        }, offline: offline)
    }

    public func backlog(computer: String, workspace: String) -> BacklogStore? {
        let key = computer + "|" + workspace
        if let existing = backlogs[key] { return existing }
        guard let connection = connections[computer] else { return nil }
        let store = BacklogStore(workspace: workspace, runner: connection)
        backlogs[key] = store
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
        backlogs = backlogs.filter { !$0.key.hasPrefix(id + "|") }
    }

    private static let resolvedKind = "resolved"

    private static func resolvedNotifications(_ payload: JSONValue) -> [NotificationKind] {
        payload["requestKind"]?.stringValue == "elicitation" ? [.question, .needsInput] : [.permission]
    }

    private static func resolvedKey(_ computer: String, _ session: String, _ kind: NotificationKind) -> String {
        computer + "|" + session + "|" + kind.rawValue
    }

    private static func cursorKey(_ computer: String) -> String {
        "companion.cursor." + computer
    }

    private func route(_ signal: ConnectionSignal, from computer: String) {
        lastSignal = .now
        if let seen = connections[computer]?.lastSeen {
            try? store.seen(computer, at: seen)
        }
        if case .event = signal, let seq = connections[computer]?.lastEventID {
            cursors?.set(seq, forKey: Self.cursorKey(computer))
        }
        if let connection = connections[computer] {
            switch signal {
            case .online:
                identified(connection.computer)
                let list = macs(excluding: computer)
                Task { await connection.sendMacs(list) }
            case .event(let event) where event.topic == ComputerConnection.identityTopic:
                identified(connection.computer)
            case .event:
                break
            }
        }
        if case .event(let event) = signal, event.topic == Topic.agentsChanged, event.payload["kind"]?.stringValue == Self.resolvedKind,
           let session = event.payload["session"]?.stringValue {
            let ids = Self.resolvedNotifications(event.payload).flatMap { kind in
                resolvedAt[Self.resolvedKey(computer, session, kind)] = event.seq
                return inbox.resolve(computer: computer, session: session, kind: kind)
            }
            if !ids.isEmpty { withdrawn?(ids) }
        }
        if case .event(let event) = signal, let item = InboxItem(event, computerID: computer, received: .now) {
            receiving += 1
            Task {
                await receive(item, seq: event.seq)
                receiving -= 1
                lastSignal = .now
            }
        }
        for (key, store) in agents where key.hasPrefix(computer + "|") {
            store.handle(signal)
        }
        for (key, store) in backlogs where key.hasPrefix(computer + "|") {
            store.handle(signal)
        }
    }

    private func receive(_ item: InboxItem, seq: Int) async {
        let allowed = await policy(item).allows(item.kind)
        guard inbox.add(item) else { return }
        let resolved = (item.session.flatMap { resolvedAt[Self.resolvedKey(item.computerID, $0, item.kind)] } ?? 0) > seq
        if allowed && !resolved {
            posted?(item)
        } else {
            inbox.markRead(item)
        }
    }

    private func policy(_ item: InboxItem) async -> NotificationPolicy {
        guard let session = item.session, AgentSessionID.matches(session), let connection = connections[item.computerID],
              let sessions = try? await connection.command(AgentCommand.sessions, workspace: item.workspace, args: nil) else { return .all }
        return NotificationPolicy(session: session, in: sessions)
    }
}
