import Foundation
import Observation

@MainActor
@Observable
public final class CompanionModel {
    public private(set) var computers: [Computer] = []
    public private(set) var connections: [String: ComputerConnection] = [:]
    public var inbox = Inbox()
    @ObservationIgnored public let queue: ActionQueue
    @ObservationIgnored public let lists: ListCache
    @ObservationIgnored public let transcripts: TranscriptCache
    @ObservationIgnored public var posted: (@MainActor (InboxItem) -> Void)?
    @ObservationIgnored public var withdrawn: (@MainActor ([String]) -> Void)?
    @ObservationIgnored public var received: (@MainActor (String, ConnectionSignal) -> Void)?

    @ObservationIgnored public let device: DeviceIdentity
    @ObservationIgnored public let policy: HostPolicy
    @ObservationIgnored private let store: ComputerStore
    @ObservationIgnored private let transport: any HTTPTransport
    @ObservationIgnored private var wentBackground = false
    @ObservationIgnored private let cursors: UserDefaults?
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private let bannerGrace: Duration
    @ObservationIgnored private var agents: [String: AgentsStore] = [:]
    @ObservationIgnored private var backlogs: [String: BacklogStore] = [:]
    @ObservationIgnored private var docsLists: [String: DocsListStore] = [:]
    @ObservationIgnored private var observers: [String: UUID] = [:]
    @ObservationIgnored private var lastSignal = ContinuousClock.now
    @ObservationIgnored private var receiving = 0
    @ObservationIgnored private var resolvedAt: [String: Int] = [:]
    @ObservationIgnored private var retrying: Set<String> = []
    @ObservationIgnored public private(set) var pushToken: PushToken?

    public init(store: ComputerStore, transport: any HTTPTransport, device: DeviceIdentity, policy: HostPolicy = .tailnet, cursors: UserDefaults? = nil, queueFile: URL? = nil, reportsDir: URL? = nil, listsDir: URL? = nil, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }, bannerGrace: Duration = ComputerConnection.bannerGrace) {
        self.bannerGrace = bannerGrace
        queue = ActionQueue(file: queueFile, reports: reportsDir)
        lists = ListCache(dir: listsDir)
        transcripts = TranscriptCache(lists: lists)
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
        queue.onLanded = { [weak self] landed in
            let key = landed.item.computer + "|" + landed.item.workspace
            self?.backlogs[key]?.landed(landed)
            self?.agents[key]?.landed(landed)
        }
        queue.onEvent = { [weak self] item, line in
            self?.connections[item.computer]?.note(.outbox, line)
        }
    }

    public func start() {
        for connection in connections.values {
            connection.start()
        }
    }

    public func enteredBackground() {
        wentBackground = true
    }

    public func reconnectAll() {
        let opened = wentBackground
        wentBackground = false
        for connection in connections.values {
            connection.background = false
            if opened { connection.restartBannerGrace() }
            connection.reconnectNow()
        }
    }

    public func networkReturned() {
        for connection in connections.values {
            connection.networkReturned()
        }
    }

    public func disconnectAll() {
        for connection in connections.values {
            connection.stop()
        }
    }

    public func registered(_ token: PushToken) {
        guard token != pushToken else { return }
        pushToken = token
        for connection in connections.values where connection.state == .online {
            Task { await connection.sendPushToken(token) }
        }
    }

    public func catchUp(quiet: Duration = .seconds(2), timeout: Duration = .seconds(20)) async {
        for connection in connections.values {
            connection.stop()
            connection.start(background: true)
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
        var computer = result.computer
        if let existing = computers.first(where: { $0.id == computer.id }) { computer.path = existing.path }
        try store.save(computer, token: result.token, relayTicket: result.relayTicket)
        computers = store.computers
        detach(computer.id)
        attach(computer)
        connections[computer.id]?.start()
        sendMacs(excluding: computer.id)
        return computer
    }

    public func remove(_ id: String) throws {
        if let connection = connections[id] {
            Task { await connection.sendPushToken(PushToken(token: "", environment: "")) }
        }
        detach(id)
        queue.forget(computer: id)
        lists.forget(computer: id)
        Task { [transcripts] in await transcripts.forget(computer: id) }
        cursors?.removeObject(forKey: Self.cursorKey(id))
        try store.remove(id)
        computers = store.computers
        inbox.remove(computer: id)
        sendMacs(excluding: nil)
    }

    public var claudeAccounts: ClaudeAccounts {
        ClaudeAccounts(connections.values.compactMap { connection in
            connection.usage.map { UsageReport(computer: connection.computer.id, mac: connection.computer.name, usage: $0) }
        })
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

    public func setName(_ name: String, computer: String) async throws {
        guard let connection = connections[computer] else { return }
        identified(try await connection.setName(name))
    }

    private func identified(_ computer: Computer) {
        let before = computers.first { $0.id == computer.id }?.tag
        try? store.identify(computer.id, as: ComputerInfo(id: computer.id, name: computer.name, colour: computer.colour, model: computer.model))
        computers = store.computers
        if computers.first(where: { $0.id == computer.id })?.tag != before { sendMacs(excluding: computer.id) }
    }

    public func setPath(_ path: PathChoice, computer id: String) throws {
        try store.setPath(path, for: id)
        computers = store.computers
        connections[id]?.setPath(path)
    }

    private func tunnelChanged(_ tunnel: RelayRoute, connection: ComputerConnection) {
        let id = connection.computer.id
        guard let saved = computers.first(where: { $0.id == id })?.relay, saved != tunnel, tunnel.macKey != nil, policy.allowsRelay(tunnel.url) else { return }
        do {
            try store.setRelay(tunnel, for: id)
        } catch {
            connection.note(.state, "relay key not saved err=" + EventLog.code(error))
            return
        }
        computers = store.computers
        connection.setRelay(tunnel)
        connection.note(.state, "relay key kid=" + tunnel.kid)
    }

    private func sendMacs(excluding id: String?) {
        for connection in connections.values where connection.computer.id != id && connection.state == .online {
            let list = macs(excluding: connection.computer.id)
            Task { await connection.sendMacs(list) }
        }
    }

    public func agents(computer: String, workspace: String, settled: Bool = true) -> AgentsStore? {
        let store = agentsStore(computer: computer, workspace: workspace)
        if settled { store?.settle() }
        return store
    }

    private func agentsStore(computer: String, workspace: String) -> AgentsStore? {
        let key = computer + "|" + workspace
        if let existing = agents[key] { return existing }
        guard let connection = connections[computer] else { return nil }
        let store = AgentsStore(workspace: workspace, runner: connection, queue: queue, computer: computer, cache: lists, transcriptCache: transcripts, queues: { [weak connection] in connection?.queues ?? false }, retry: { [weak self, weak connection] in
            if let self, let connection { self.flushQueue(connection) }
        })
        agents[key] = store
        return store
    }

    public func needsYou() -> [NeedsYouItem] {
        let known = computers.compactMap { connections[$0.id] }.filter { $0.state != .unauthorized }
        let offline = Set(known.filter { $0.state != .online }.map(\.computer.id))
        return NeedsYouItem.collect(known.flatMap { connection in
            connection.workspaces.compactMap { workspace in
                agentsStore(computer: connection.computer.id, workspace: workspace.id).map { (connection.computer.id, tag(connection.computer.id), workspace, $0.fetched ? $0.sessions : []) }
            }
        }, offline: offline)
    }

    public func nextWaiting(after session: String, computer: String, workspace: String) -> NeedsYouItem? {
        NeedsYouItem.next(needsYou(), after: session, computer: computer, workspace: workspace)
    }

    public func waitingCount(computer: String, workspace: String, excluding session: String? = nil) -> Int {
        guard let store = agentsStore(computer: computer, workspace: workspace), store.fetched else { return 0 }
        return WorkspaceActivity(store.sessions.filter { $0.id != session }).waiting
    }

    public func backlog(computer: String, workspace: String, settled: Bool = true) -> BacklogStore? {
        let store = backlogStore(computer: computer, workspace: workspace)
        if settled { store?.settle() }
        return store
    }

    private func backlogStore(computer: String, workspace: String) -> BacklogStore? {
        let key = computer + "|" + workspace
        if let existing = backlogs[key] { return existing }
        guard let connection = connections[computer] else { return nil }
        let store = BacklogStore(workspace: workspace, runner: connection, queue: queue, cache: lists, computer: computer, queues: { [weak connection] in connection?.queues ?? false }, retry: { [weak self, weak connection] in
            if let self, let connection { self.flushQueue(connection) }
        })
        backlogs[key] = store
        return store
    }

    public func docs(computer: String, workspace: String, session: String? = nil) -> DocsListStore? {
        let key = computer + "|" + workspace + "|" + (session ?? "")
        if let existing = docsLists[key] { return existing }
        guard let connection = connections[computer] else { return nil }
        let store = DocsListStore(workspace: workspace, session: session, runner: connection, cache: lists, computer: computer)
        docsLists[key] = store
        return store
    }

    private func attach(_ computer: Computer) {
        let saved = cursors?.object(forKey: Self.cursorKey(computer.id)) as? Int
        lists.allow(computer: computer.id)
        let connection = ComputerConnection(computer: computer, token: store.token(for: computer.id), transport: transport, policy: policy, relayTicket: store.relayTicket(for: computer.id), lastEventID: saved, cache: lists, device: device, sleep: sleep, bannerGrace: bannerGrace)
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
        docsLists = docsLists.filter { !$0.key.hasPrefix(id + "|") }
    }

    private static let resolvedKind = "resolved"

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
                if let tunnel = connection.hello?.tunnel { tunnelChanged(tunnel, connection: connection) }
                let list = macs(excluding: computer)
                let token = pushToken
                Task {
                    await connection.sendMacs(list)
                    if let token { await connection.sendPushToken(token) }
                }
            case .event(let event) where event.topic == ComputerConnection.identityTopic:
                identified(connection.computer)
            case .event:
                break
            }
            flushQueue(connection)
        }
        if case .event(let event) = signal, event.topic == Topic.agentsChanged, event.payload["kind"]?.stringValue == Self.resolvedKind,
           let session = event.payload["session"]?.stringValue {
            let request = DeliveredNotice.cardRequest(event.payload["requestKind"]?.stringValue, event.payload["request"]?.stringValue)
            let ids = NotificationKind.resolved(by: event.payload["requestKind"]?.stringValue).flatMap { kind in
                resolvedAt[Self.resolvedKey(computer, request ?? session, kind)] = event.seq
                return inbox.resolve(computer: computer, session: session, kind: kind, request: request)
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
        for (key, store) in docsLists where key.hasPrefix(computer + "|") {
            store.handle(signal)
        }
        received?(computer, signal)
    }

    public func reportFiles(computer: String, screenshot: Data?) -> [ReportFile] {
        guard let connection = connections[computer] else { return [] }
        return ProblemReport.files(connection: connection, queue: queue, device: device, appVersion: connection.appVersion, screenshot: screenshot)
    }

    @discardableResult
    public func sendReport(computer: String, text: String, files: [ReportFile], mac: Bool) throws -> QueuedAction {
        let item = try queue.enqueueReport(computer: computer, text: text, files: files, mac: mac)
        if let connection = connections[computer], connection.state == .online { flushQueue(connection) }
        return item
    }

    public func resendReport(_ item: QueuedAction) throws {
        try queue.resendReport(item)
        if let connection = connections[item.computer], connection.state == .online { flushQueue(connection) }
    }

    private func flushQueue(_ connection: ComputerConnection) {
        let computer = connection.computer.id
        guard queue.isDue(computer: computer) else { return }
        Task {
            let flushed = await queue.flush(computer: computer, runner: connection, supports: connection.supports)
            for workspace in flushed.touched {
                await agents(computer: computer, workspace: workspace)?.refreshAll()
                await backlogs[computer + "|" + workspace]?.refresh()
            }
            retryLater(connection)
        }
    }

    private func retryLater(_ connection: ComputerConnection) {
        let computer = connection.computer.id
        guard let wait = queue.nextWait(computer: computer), retrying.insert(computer).inserted else { return }
        Task { [sleep] in
            try? await sleep(.seconds(wait + 1))
            retrying.remove(computer)
            flushQueue(connection)
        }
    }

    private func receive(_ item: InboxItem, seq: Int) async {
        let allowed = item.always ? true : await policy(item).allows(item.kind)
        guard inbox.add(item) else { return }
        let resolved = ([item.session, item.request].compactMap { $0 }.compactMap { resolvedAt[Self.resolvedKey(item.computerID, $0, item.kind)] }.max() ?? 0) > seq
        if !allowed || resolved {
            inbox.markRead(item)
        } else if !item.pushed {
            posted?(item)
        }
    }

    private func policy(_ item: InboxItem) async -> NotificationPolicy {
        guard let session = item.session, AgentSessionID.matches(session), let connection = connections[item.computerID],
              let sessions = try? await connection.command(AgentCommand.sessions, workspace: item.workspace, args: .object(["session": .string(session)])) else { return .all }
        return NotificationPolicy(session: session, in: sessions)
    }
}
