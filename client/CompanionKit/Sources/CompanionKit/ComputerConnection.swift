import Foundation
import Observation

public enum ConnectionSignal: Sendable, Equatable {
    case online
    case event(BusEvent)
}

public protocol CommandRunner: AnyObject {
    @MainActor func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue
    @MainActor func command(_ id: String, workspace: String?, args: JSONValue?, requestId: String?) async throws -> JSONValue
    @MainActor func command<T: Decodable & Sendable>(_ id: String, workspace: String?, args: JSONValue?, as type: T.Type) async throws -> T
    @MainActor var dedupes: Bool { get }
    @MainActor var reports: Bool { get }
    @MainActor var macName: String { get }
    @MainActor func report(text: String, mac: Bool, files: [(name: String, data: Data)], requestId: String) async throws -> String
    @MainActor func supports(_ command: String) -> Bool
}

extension CommandRunner {
    @MainActor public func supports(_ command: String) -> Bool { true }

    @MainActor public func command(_ id: String, workspace: String?, args: JSONValue?, requestId: String?) async throws -> JSONValue {
        try await command(id, workspace: workspace, args: args)
    }

    @MainActor public var dedupes: Bool { false }

    @MainActor public var reports: Bool { false }

    @MainActor public var macName: String { "the Mac" }

    @MainActor public func report(text: String, mac: Bool, files: [(name: String, data: Data)], requestId: String) async throws -> String {
        throw RemoteError.badResponse
    }

    @MainActor public func command<T: Decodable & Sendable>(_ id: String, workspace: String?, args: JSONValue?, as type: T.Type) async throws -> T {
        let value = try await command(id, workspace: workspace, args: args)
        return try await OffMain.commands.run((workspace ?? "") + "/" + id) { try value.decode(type) }
    }
}

@MainActor
@Observable
public final class ComputerConnection: CommandRunner {
    public enum State: Equatable, Sendable {
        case offline(String?)
        case connecting
        case online
        case unauthorized

        public var label: String {
            switch self {
            case .offline: "Offline"
            case .connecting: "Connecting…"
            case .online: "Online"
            case .unauthorized: "Access revoked — pair again"
            }
        }
    }

    public static let identityTopic = "remote.identity"
    public static let voiceSettings = "voice.settings"
    public static let voiceSettingsTopic = "voice.settings-changed"
    public static let awakeState = "awake.state"
    public static let awakeSet = "awake.set"
    public static let awakeTopic = "awake.changed"
    public static let usageCommand = "agents.usage"
    public static let usageTopic = "agents.usage"
    public static let backoff: [Duration] = [1, 2, 5, 10, 30].map { .seconds($0) }
    public static let pingInterval: Duration = .seconds(15)
    public static let bannerGrace: Duration = .seconds(5)
    public nonisolated static let workspaceRefreshEvery: Duration = .milliseconds(2500)

    public private(set) var computer: Computer
    public private(set) var state: State = .offline(nil)
    public private(set) var hello: Hello?
    public private(set) var workspaces: [Workspace] = []
    public private(set) var lastEventID: Int?
    public private(set) var lastSeen: Date?
    public private(set) var voiceEnabled: Bool?
    public private(set) var awake: AwakeState?
    public private(set) var usage: ClaudeUsage?
    public private(set) var log = ConnectionLog()
    public private(set) var events = EventLog()
    public private(set) var activePath: NetworkPath?
    public private(set) var revokedBy: RemoteError?
    public private(set) var showsBanner = false
    @ObservationIgnored public internal(set) var background = false
    @ObservationIgnored public private(set) var streamID: String?

    @ObservationIgnored public private(set) var client: RemoteClient
    @ObservationIgnored public private(set) var clients: [NetworkPath: RemoteClient] = [:]
    @ObservationIgnored private var answered: NetworkPath?
    @ObservationIgnored private var probeFrom: ContinuousClock.Instant?
    @ObservationIgnored private var probeTask: Task<Void, Never>?
    @ObservationIgnored let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored let now: @Sendable () -> ContinuousClock.Instant
    @ObservationIgnored private var heardAt: ContinuousClock.Instant?
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var bannerTask: Task<Void, Never>?
    @ObservationIgnored private let bannerGrace: Duration
    @ObservationIgnored private var refreshTask: Task<Void, Never>?
    @ObservationIgnored private var refreshQueued = false
    @ObservationIgnored private var refreshRun = 0
    @ObservationIgnored private var pending: PendingAttempt?
    @ObservationIgnored private var listeners: [UUID: @MainActor (ConnectionSignal) -> Void] = [:]
    @ObservationIgnored private var interfaces: [String: WeakInterface] = [:]
    @ObservationIgnored private let cache: ListCache
    @ObservationIgnored private let token: String?
    @ObservationIgnored private let relayTicket: String?
    @ObservationIgnored public private(set) var routes: [RemoteClient.Route] = []
    @ObservationIgnored private let transport: any HTTPTransport
    @ObservationIgnored private let policy: HostPolicy
    @ObservationIgnored private let eventSaves = SaveThrottle()
    @ObservationIgnored public let device: DeviceIdentity?
    @ObservationIgnored public let appVersion: String
    @ObservationIgnored public let instance = UUID()
    @ObservationIgnored private var loggedState: String?
    @ObservationIgnored private var awakeSeq = 0

    public init(computer: Computer, token: String?, transport: any HTTPTransport, policy: HostPolicy = .tailnet, relayTicket: String? = nil, lastEventID: Int? = nil, cache: ListCache = ListCache(dir: nil), device: DeviceIdentity? = nil, appVersion: String = ProblemReport.appVersion, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }, now: @escaping @Sendable () -> ContinuousClock.Instant = { .now }, bannerGrace: Duration = ComputerConnection.bannerGrace) {
        self.computer = computer
        self.bannerGrace = bannerGrace
        self.lastEventID = lastEventID
        lastSeen = computer.lastSeen
        self.cache = cache
        self.sleep = sleep
        self.now = now
        self.token = token
        self.relayTicket = relayTicket
        self.transport = transport
        self.policy = policy
        self.device = device
        self.appVersion = appVersion
        routes = Self.routes(computer, ticket: relayTicket, transport: transport, policy: policy)
        clients = Dictionary(routes.map { ($0.path, RemoteClient(route: $0, token: token, transport: transport, policy: policy)) }) { first, _ in first }
        client = RemoteClient(route: .tailnet(hosts: computer.hosts, port: computer.port), token: token, transport: transport, policy: policy)
        answered = cache.load(pathKey, as: SavedPath.self)?.path
        if let first = selector.order.first, let chosen = clients[first] { client = chosen }
        log = cache.load(logKey, as: ConnectionLog.self) ?? ConnectionLog()
        events = cache.load(eventsKey, as: EventLog.self) ?? EventLog()
        loggedState = events.lines.last { $0.kind == .state }?.text
        if token == nil {
            state = .unauthorized
            showsBanner = true
        } else if let saved = cache.load(savedKey, as: Saved.self) {
            hello = saved.hello
            workspaces = saved.workspaces
            awake = saved.awake
        }
    }

    private struct Saved: Codable, Sendable {
        let hello: Hello
        let workspaces: [Workspace]
        var awake: AwakeState?
    }

    private struct SavedPath: Codable, Sendable {
        let path: NetworkPath
    }

    private var pathKey: ListCache.Key {
        ListCache.Key(computer: computer.id, workspace: "", collection: "path")
    }

    public var selector: PathSelector {
        PathSelector(choice: computer.path, paths: Set(clients.keys), answered: answered)
    }

    private var savedKey: ListCache.Key {
        ListCache.Key(computer: computer.id, workspace: "", collection: "connection")
    }

    private var logKey: ListCache.Key {
        ListCache.Key(computer: computer.id, workspace: "", collection: "attempts")
    }

    private var eventsKey: ListCache.Key {
        ListCache.Key(computer: computer.id, workspace: "", collection: "events")
    }

    public func note(_ kind: EventKind, _ text: String) {
        events.add(kind, text)
        eventSaves.request { [weak self] in self?.saveEvents() }
    }

    private func saveEvents() {
        cache.save(events, eventsKey)
    }

    private func move(to new: State, error: (any Error)? = nil) {
        state = new
        holdBanner()
        let line: String? = switch new {
        case .online: "online" + (activePath.map { " path=" + $0.rawValue } ?? "")
        case .unauthorized: "unauthorized"
        case .offline: error.map { "offline err=" + EventLog.code($0) } ?? "offline stopped"
        case .connecting: nil
        }
        guard let line, line != loggedState else { return }
        loggedState = line
        note(.state, line)
    }

    public nonisolated static func routes(_ computer: Computer, ticket: String?, transport: any HTTPTransport, policy: HostPolicy) -> [RemoteClient.Route] {
        var routes: [RemoteClient.Route] = []
        if !policy.filter(computer.hosts).isEmpty {
            routes.append(.tailnet(hosts: computer.hosts, port: computer.port))
        }
        if let relay = computer.relay, let key = relay.macKey, let ticket, policy.allowsRelay(relay.url) {
            let tunnel = TunnelTransport(relay: relay.url, macKey: key, ticket: ticket, outer: transport)
            let route = RemoteClient.Route.relay(tunnel, computer: computer.id)
            if computer.path == .relay { routes.insert(route, at: 0) } else { routes.append(route) }
        }
        return routes
    }

    public func setRelay(_ relay: RelayRoute) {
        guard relay != computer.relay else { return }
        computer.relay = relay
        routes = Self.routes(computer, ticket: relayTicket, transport: transport, policy: policy)
        let fresh = routes.first { $0.path == .relay }.map { RemoteClient(route: $0, token: token, transport: transport, policy: policy) }
        clients[.relay] = fresh
        guard client.route.path == .relay, let fresh else { return }
        client = fresh
    }

    public func setPath(_ choice: PathChoice) {
        guard choice != computer.path else { return }
        computer.path = choice
        note(.state, "path choice=" + choice.rawValue)
        guard task != nil, let first = selector.order.first, first != activePath else { return }
        restart("the path changed")
    }

    public func makeCheck(probe: any NetworkProbe) -> ConnectionCheck {
        var relay: ConnectionCheck.Relay?
        if routes.contains(where: { $0.path == .relay }), let route = computer.relay, let key = route.macKey, let relayTicket {
            relay = ConnectionCheck.Relay(url: route.url, macKey: key, kid: route.kid, ticket: relayTicket, computer: computer.id)
        }
        let first = activePath ?? selector.order.first ?? .tailnet
        return ConnectionCheck(hosts: policy.filter(computer.hosts), port: computer.port, token: token, transport: transport, policy: policy, probe: probe, relay: relay, macName: computer.name, first: first) { [weak self] in
            self?.reconnectNow()
        }
    }

    private func record(_ step: ConnectionAttempt.Step, started: ContinuousClock.Instant, trace: HostTrace, path: NetworkPath? = nil, error: String? = nil, lived: ContinuousClock.Instant? = nil, cancelled: Bool = false) {
        pending = nil
        let elapsed = now() - started
        let milliseconds = Int(elapsed.components.seconds * 1000 + elapsed.components.attoseconds / 1_000_000_000_000_000)
        let livedFor = lived.map { Int((now() - $0).components.seconds) }
        log.record(ConnectionAttempt(at: .now, step: step, ok: error == nil, milliseconds: milliseconds, error: error, tries: trace.values, background: background, lived: livedFor, cancelled: cancelled, path: path))
        cache.save(log, logKey)
    }

    private func advance(to step: ConnectionAttempt.Step) {
        guard !Task.isCancelled else { return }
        pending?.step = step
    }

    private func abandon(_ why: String) {
        guard let pending else { return }
        record(pending.step, started: pending.started, trace: pending.trace, path: pending.path, error: why, cancelled: true)
    }

    private func save() {
        guard let hello else { return }
        cache.save(Saved(hello: hello, workspaces: workspaces, awake: awake), savedKey)
    }

    public func start(background: Bool = false) {
        guard task == nil, state != .unauthorized else { return }
        self.background = background
        task = Task { await run() }
    }

    public func stop() {
        abandon("the app stopped it")
        task?.cancel()
        task = nil
        refreshTask?.cancel()
        refreshTask = nil
        refreshQueued = false
        refreshRun &+= 1
        streamID = nil
        leavePath()
        if state != .unauthorized {
            move(to: .offline(nil))
            bannerTask?.cancel()
            bannerTask = nil
            showsBanner = false
        }
        saveEvents()
    }

    private func holdBanner() {
        switch state {
        case .online:
            bannerTask?.cancel()
            bannerTask = nil
            showsBanner = false
        case .unauthorized:
            bannerTask?.cancel()
            bannerTask = nil
            showsBanner = true
        case .offline, .connecting:
            guard !showsBanner, bannerTask == nil else { return }
            bannerTask = Task { [weak self, bannerGrace] in
                try? await Task.sleep(for: bannerGrace)
                guard let self, !Task.isCancelled else { return }
                bannerTask = nil
                showsBanner = state != .online
            }
        }
    }

    public func restartBannerGrace() {
        guard state != .online, state != .unauthorized else { return }
        bannerTask?.cancel()
        bannerTask = nil
        showsBanner = false
        holdBanner()
    }

    public func reconnectNow() {
        guard state != .unauthorized else { return }
        if state == .online {
            guard let heardAt, now() - heardAt > Self.pingInterval * 2 else { return }
            streamID = nil
        }
        abandon("the app restarted it")
        task?.cancel()
        task = nil
        start()
    }

    private func restart(_ why: String) {
        streamID = nil
        abandon(why)
        task?.cancel()
        task = nil
        endProbe()
        start(background: background)
    }

    private func leavePath() {
        activePath = nil
        endProbe()
    }

    private func endProbe() {
        probeTask?.cancel()
        probeTask = nil
        probeFrom = nil
    }

    private func remember(_ path: NetworkPath) {
        activePath = path
        probeFrom = path == .relay ? now() : nil
        guard answered != path else { return }
        answered = path
        cache.store(SavedPath(path: path), pathKey)
    }

    private func probeIfDue() {
        guard probeTask == nil, let from = probeFrom, selector.probeDue(active: activePath, elapsed: now() - from), let tailnet = clients[.tailnet] else { return }
        probeFrom = now()
        note(.state, "probe path=tailnet")
        probeTask = Task { [weak self] in
            let answers = await ConnectionCheck.answers(tailnet)
            guard let self, !Task.isCancelled else { return }
            probeTask = nil
            guard answers, activePath == .relay else { return }
            answered = .tailnet
            cache.store(SavedPath(path: .tailnet), pathKey)
            note(.state, "probe ok path=tailnet")
            restart("the app moved to Tailscale")
        }
    }

    private func hello(_ trace: HostTrace) async throws -> (NetworkPath, RemoteClient, Hello) {
        let order = selector.order
        for (index, path) in order.enumerated() {
            guard let candidate = clients[path] else { continue }
            pending?.path = path
            do {
                return (path, candidate, try await candidate.hello(trace: trace, quick: index < order.count - 1))
            } catch {
                guard !Task.isCancelled, index < order.count - 1, PathSelector.fallsOver(error) else { throw error }
            }
        }
        throw RemoteError.hostNotAllowed
    }

    public func networkReturned() {
        guard task != nil else { return }
        reconnectNow()
    }

    @discardableResult
    public func observe(_ handler: @escaping @MainActor (ConnectionSignal) -> Void) -> UUID {
        let id = UUID()
        listeners[id] = handler
        return id
    }

    public func removeObserver(_ id: UUID) {
        listeners[id] = nil
    }

    public func refreshWorkspaces() async {
        guard let list = try? await client.workspaces(), !Task.isCancelled, list != workspaces else { return }
        workspaces = list
        save()
    }

    public func refreshWorkspacesSoon() {
        guard refreshTask == nil else {
            refreshQueued = true
            return
        }
        refreshRun &+= 1
        let run = refreshRun
        refreshTask = Task { [weak self] in
            while let self, !Task.isCancelled {
                await self.refreshWorkspaces()
                try? await self.sleep(Self.workspaceRefreshEvery)
                guard self.refreshQueued, !Task.isCancelled else { break }
                self.refreshQueued = false
            }
            guard let self, self.refreshRun == run else { return }
            self.refreshQueued = false
            self.refreshTask = nil
        }
    }

    public func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        try await authorized("id=" + id) { try await $0.command(id, workspace: workspace, args: args) }
    }

    public func command(_ id: String, workspace: String?, args: JSONValue?, requestId: String?) async throws -> JSONValue {
        try await authorized("id=" + id) { try await $0.command(id, workspace: workspace, args: args, requestId: requestId) }
    }

    public var dedupes: Bool {
        hello?.requests ?? false
    }

    public var reports: Bool {
        hello?.reports ?? false
    }

    public var macName: String {
        computer.name
    }

    public func report(text: String, mac: Bool, files: [(name: String, data: Data)], requestId: String) async throws -> String {
        let device = device
        let appVersion = appVersion
        return try await authorized("method=report") { try await $0.report(text: text, mac: mac, files: files, requestId: requestId, device: device, appVersion: appVersion) }
    }

    public func command<T: Decodable & Sendable>(_ id: String, workspace: String?, args: JSONValue?, as type: T.Type) async throws -> T {
        try await authorized("id=" + id) { try await $0.command(id, workspace: workspace, args: args, as: type) }
    }

    public var supportsUI: Bool {
        hello?.ui ?? false
    }

    public func workspaceUI(_ workspace: String) -> WorkspaceUI {
        if let live = interfaces[workspace]?.value { return live }
        interfaces = interfaces.filter { $0.value.value != nil }
        let ui = WorkspaceUI(connection: self, workspace: workspace)
        interfaces[workspace] = WeakInterface(value: ui)
        return ui
    }

    func uiSections() async throws -> [SectionInfo] {
        try await authorized("method=uiSections") { try await $0.uiSections() }
    }

    func uiGet(workspace: String, slots: [String]) async throws -> [SlotView] {
        try await authorized("method=uiGet") { try await $0.uiGet(workspace: workspace, slots: slots) }
    }

    func uiWatch(stream: String, workspace: String, slots: [String]) async throws {
        try await authorized("method=uiWatch") { try await $0.uiWatch(stream: stream, workspace: workspace, slots: slots) }
    }

    func uiEvent(workspace: String, slot: String, nodeId: String, action: String, value: JSONValue?) async throws -> JSONValue {
        try await authorized("method=uiEvent slot=\(slot) action=\(action)") { try await $0.uiEvent(workspace: workspace, slot: slot, nodeId: nodeId, action: action, value: value) }
    }

    private func authorized<T: Sendable>(_ what: String, _ call: (RemoteClient) async throws -> T) async throws -> T {
        do {
            return try await call(client)
        } catch let error as RemoteError where error == .unauthorized || error == .relayRevoked {
            note(.command, "failed \(what) err=" + EventLog.code(error))
            revoke(error)
            throw error
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            note(.command, "failed \(what) err=" + EventLog.code(error))
            throw error
        }
    }

    public func sendMacs(_ macs: [MacTag]) async {
        try? await client.macs(macs)
    }

    public func sendPushToken(_ token: PushToken) async {
        _ = try? await client.pushToken(token)
    }

    public func setColour(_ colour: MacColour) async throws -> Computer {
        try await setIdentity { try await $0.setIdentityColour(colour.rawValue) }
    }

    public func setName(_ name: String) async throws -> Computer {
        try await setIdentity { try await $0.setIdentityName(name) }
    }

    public var renames: Bool {
        hello?.rename ?? false
    }

    private func setIdentity(_ call: (RemoteClient) async throws -> ComputerInfo) async throws -> Computer {
        do {
            identify(try await call(client))
            return computer
        } catch let error as RemoteError where error == .unauthorized || error == .relayRevoked {
            revoke(error)
            throw error
        }
    }

    public var presence: String {
        switch state {
        case .online: "online"
        case .connecting: "connecting…"
        case .unauthorized: revokedBy == .relayRevoked ? "relay access revoked" : "access revoked"
        case .offline: lastSeen.map { "offline since " + Self.clock($0) } ?? "offline"
        }
    }

    public var revokedTitle: String {
        revokedBy == .relayRevoked ? "Relay access revoked" : "Access revoked"
    }

    public var pathChoices: [PathChoice] {
        selector.paths.count > 1 ? PathChoice.allCases : []
    }

    public var isOffline: Bool {
        if case .offline = state { return true }
        return false
    }

    public var shortPresence: String {
        if case .offline = state { return "offline" }
        return presence
    }

    public var offlineDetail: String? {
        guard case .offline(let reason) = state else { return nil }
        let parts = [reason, lastSeen.map { "as of " + Self.clock($0) }].compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    public var blocked: String? {
        blocked("answer")
    }

    public func blocked(_ action: String) -> String? {
        switch state {
        case .online: nil
        case .unauthorized: "Can't \(action) — access revoked"
        case .offline, .connecting: offline(action)
        }
    }

    public func failure(_ error: any Error, action: String) -> String {
        switch error as? RemoteError {
        case .notSent?: offline(action)
        case .unreachable?: "\(computer.name) stopped answering — it may have gone through, check before trying again"
        case let remote?: remote.message
        case nil: error.localizedDescription
        }
    }

    private func offline(_ action: String) -> String {
        let line = "Can't \(action) — \(computer.name) is offline"
        return lastSeen.map { line + " · as of " + Self.clock($0) } ?? line
    }

    public var queues: Bool {
        switch state {
        case .offline, .connecting: true
        case .online, .unauthorized: false
        }
    }

    public var canWrite: Bool {
        state != .unauthorized
    }

    public var queueNote: String? {
        queues ? "\(computer.name) is offline · this goes out when it's back" : nil
    }

    public var queuedLine: String {
        state == .online ? "\(computer.name) · sending" : "\(computer.name) is offline · queued"
    }

    public nonisolated static func clock(_ date: Date) -> String {
        date.formatted(date: Calendar.current.isDateInToday(date) ? .omitted : .abbreviated, time: .shortened)
    }

    public func supports(_ command: String) -> Bool {
        hello?.commands.contains(command) ?? false
    }

    public var voiceAvailable: Bool {
        guard hello != nil else { return true }
        return supports("voice.start") && (!supports(Self.voiceSettings) || voiceEnabled == true)
    }

    private func run() async {
        var failures = 0
        while !Task.isCancelled {
            if state != .online { move(to: .connecting) }
            let started = now()
            let trace = HostTrace()
            var step = ConnectionAttempt.Step.hello
            var connectedAt: ContinuousClock.Instant?
            var path: NetworkPath?
            pending = PendingAttempt(started: started, step: step, trace: trace)
            do {
                let (answeredPath, active, hello) = try await hello(trace)
                if Task.isCancelled { return }
                path = answeredPath
                client = active
                remember(answeredPath)
                step = .workspaces
                advance(to: step)
                let list = try await client.workspaces(trace: trace)
                if Task.isCancelled { return }
                self.hello = hello
                workspaces = list
                save()
                identify(hello.computer)
                await loadVoiceSettings()
                await loadUsage()
                if Task.isCancelled { return }
                step = .events
                advance(to: step)
                for try await item in try await client.events(after: lastEventID, trace: trace) {
                    if Task.isCancelled { return }
                    failures = 0
                    lastSeen = .now
                    heardAt = now()
                    probeIfDue()
                    switch item {
                    case .alive:
                        move(to: .online)
                        connectedAt = now()
                        record(.events, started: started, trace: trace, path: path)
                        emit(.online)
                        await loadAwake()
                        if Task.isCancelled { return }
                    case .ping:
                        break
                    case .event(let event):
                        lastEventID = event.seq
                        if event.topic == Topic.pluginChanged {
                            note(.stream, "plugin.changed id=" + (event.payload["id"]?.stringValue ?? "-"))
                            for ui in interfaces.values.compactMap(\.value) {
                                ui.reloadSections()
                            }
                        }
                        if event.topic == Topic.pluginChanged, let fresh = try? await client.hello() {
                            if Task.isCancelled { return }
                            self.hello = fresh
                            save()
                            identify(fresh.computer)
                            await loadVoiceSettings()
                            await loadAwake()
                            await loadUsage()
                            if Task.isCancelled { return }
                        }
                        if event.topic == Topic.agentsChanged {
                            refreshWorkspacesSoon()
                        }
                        if event.topic == Self.voiceSettingsTopic {
                            voiceEnabled = event.payload["enabled"]?.boolValue
                        }
                        if event.topic == Self.awakeTopic, supports(Self.awakeState), let state = try? event.payload.decode(AwakeState.self) {
                            show(awake: state)
                        }
                        if event.topic == Self.usageTopic, supports(Self.usageCommand), let state = try? event.payload.decode(ClaudeUsage.self) {
                            usage = state
                        }
                        if event.topic == Self.identityTopic, let info = try? event.payload["computer"]?.decode(ComputerInfo.self) {
                            identify(info)
                        }
                        emit(.event(event))
                    case .stream(let id):
                        streamID = id
                        for ui in interfaces.values.compactMap(\.value) {
                            ui.reload()
                        }
                    case .ui(let view):
                        interfaces[view.workspaceId]?.value?.receive(view)
                    case .reset(let seq):
                        note(.stream, "reset seq=\(seq)")
                        lastEventID = seq
                    }
                }
                if Task.isCancelled { return }
                record(step, started: started, trace: trace, path: path, error: connectedAt == nil ? "stream closed before it said connected" : "stream closed by the Mac", lived: connectedAt)
                streamID = nil
                leavePath()
                move(to: .offline(nil), error: RemoteError.unreachable(connectedAt == nil ? "stream closed before it said connected" : "stream closed by the Mac"))
            } catch let error as RemoteError where error == .unauthorized || error == .relayRevoked {
                if Task.isCancelled { return }
                record(step, started: started, trace: trace, path: path ?? pending?.path, error: ConnectionAttempt.describe(error), lived: connectedAt)
                revoke(error)
                return
            } catch {
                if Task.isCancelled { return }
                record(step, started: started, trace: trace, path: path ?? pending?.path, error: ConnectionAttempt.describe(error), lived: connectedAt)
                streamID = nil
                leavePath()
                move(to: .offline(error is CancellationError ? nil : (error as? RemoteError)?.message ?? error.localizedDescription), error: error)
            }
            let delay = Self.backoff[min(failures, Self.backoff.count - 1)]
            failures += 1
            do {
                try await sleep(delay)
            } catch {
                return
            }
        }
    }

    private func identify(_ info: ComputerInfo) {
        guard info.id == computer.id else { return }
        computer.name = info.name
        if let colour = info.colour { computer.colour = colour }
        if let model = info.model { computer.model = model }
    }

    private func loadVoiceSettings() async {
        guard supports(Self.voiceSettings) else {
            voiceEnabled = nil
            return
        }
        guard let settings = try? await client.command(Self.voiceSettings, workspace: nil), !Task.isCancelled else { return }
        voiceEnabled = settings["enabled"]?.boolValue
    }

    private func loadAwake() async {
        guard supports(Self.awakeState) else {
            if awake != nil { show(awake: nil) }
            return
        }
        let seq = awakeSeq
        do {
            let state = try await client.command(Self.awakeState, workspace: nil, as: AwakeState.self)
            guard !Task.isCancelled, seq == awakeSeq else { return }
            show(awake: state)
        } catch RemoteError.server {
            guard !Task.isCancelled, seq == awakeSeq else { return }
            show(awake: nil)
        } catch {
        }
    }

    private func loadUsage() async {
        guard supports(Self.usageCommand) else {
            usage = nil
            return
        }
        guard let state = try? await client.command(Self.usageCommand, workspace: nil, as: ClaudeUsage.self), !Task.isCancelled else { return }
        usage = state
    }

    public func setAwake(mode: String, hours: Int? = nil) async throws {
        var args: [String: JSONValue] = ["mode": .string(mode)]
        if let hours { args["hours"] = .number(Double(hours)) }
        let seq = awakeSeq
        let state = try await command(Self.awakeSet, workspace: nil, args: .object(args), as: AwakeState.self)
        guard seq == awakeSeq else { return }
        show(awake: state)
    }

    private func show(awake state: AwakeState?) {
        awakeSeq += 1
        guard state != awake else { return }
        awake = state
        save()
    }

    private func revoke(_ reason: RemoteError = .unauthorized) {
        revokedBy = reason
        leavePath()
        move(to: .unauthorized)
        saveEvents()
        streamID = nil
        hello = nil
        voiceEnabled = nil
        awake = nil
        usage = nil
        workspaces = []
        pending = nil
        task?.cancel()
        task = nil
        refreshTask?.cancel()
        refreshTask = nil
        refreshQueued = false
        refreshRun &+= 1
    }

    private func emit(_ signal: ConnectionSignal) {
        for listener in listeners.values {
            listener(signal)
        }
    }
}

private struct PendingAttempt {
    let started: ContinuousClock.Instant
    var step: ConnectionAttempt.Step
    let trace: HostTrace
    var path: NetworkPath?
}

private struct WeakInterface {
    weak var value: WorkspaceUI?
}
