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
        try await command(id, workspace: workspace, args: args).decode(type)
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
    public static let backoff: [Duration] = [1, 2, 5, 10, 30].map { .seconds($0) }
    public static let pingInterval: Duration = .seconds(15)

    public private(set) var computer: Computer
    public private(set) var state: State = .offline(nil)
    public private(set) var hello: Hello?
    public private(set) var workspaces: [Workspace] = []
    public private(set) var lastEventID: Int?
    public private(set) var lastSeen: Date?
    public private(set) var voiceEnabled: Bool?
    public private(set) var awake: AwakeState?
    public private(set) var log = ConnectionLog()
    public private(set) var events = EventLog()
    @ObservationIgnored public internal(set) var background = false
    @ObservationIgnored public private(set) var streamID: String?

    @ObservationIgnored public private(set) var client: RemoteClient
    @ObservationIgnored let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored let now: @Sendable () -> ContinuousClock.Instant
    @ObservationIgnored private var heardAt: ContinuousClock.Instant?
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var listeners: [UUID: @MainActor (ConnectionSignal) -> Void] = [:]
    @ObservationIgnored private var interfaces: [String: WeakInterface] = [:]
    @ObservationIgnored private let cache: ListCache
    @ObservationIgnored private let token: String?
    @ObservationIgnored private let transport: any HTTPTransport
    @ObservationIgnored private let policy: HostPolicy
    @ObservationIgnored private let eventSaves = SaveThrottle()
    @ObservationIgnored public let device: DeviceIdentity?
    @ObservationIgnored public let appVersion: String
    @ObservationIgnored private var loggedState: String?

    public init(computer: Computer, token: String?, transport: any HTTPTransport, policy: HostPolicy = .tailnet, lastEventID: Int? = nil, cache: ListCache = ListCache(dir: nil), device: DeviceIdentity? = nil, appVersion: String = ProblemReport.appVersion, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }, now: @escaping @Sendable () -> ContinuousClock.Instant = { .now }) {
        self.computer = computer
        self.lastEventID = lastEventID
        lastSeen = computer.lastSeen
        self.cache = cache
        self.sleep = sleep
        self.now = now
        self.token = token
        self.transport = transport
        self.policy = policy
        self.device = device
        self.appVersion = appVersion
        client = RemoteClient(hosts: computer.hosts, port: computer.port, token: token, transport: transport, policy: policy)
        log = cache.load(logKey, as: ConnectionLog.self) ?? ConnectionLog()
        events = cache.load(eventsKey, as: EventLog.self) ?? EventLog()
        loggedState = events.lines.last { $0.kind == .state }?.text
        if token == nil {
            state = .unauthorized
        } else if let saved = cache.load(savedKey, as: Saved.self) {
            hello = saved.hello
            workspaces = saved.workspaces
        }
    }

    private struct Saved: Codable {
        let hello: Hello
        let workspaces: [Workspace]
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
        let line: String? = switch new {
        case .online: "online"
        case .unauthorized: "unauthorized"
        case .offline: error.map { "offline err=" + EventLog.code($0) } ?? "offline stopped"
        case .connecting: nil
        }
        guard let line, line != loggedState else { return }
        loggedState = line
        note(.state, line)
    }

    public func makeCheck(probe: any NetworkProbe) -> ConnectionCheck {
        ConnectionCheck(hosts: client.hosts, port: computer.port, token: token, transport: transport, policy: policy, probe: probe) { [weak self] in
            self?.reconnectNow()
        }
    }

    private func record(_ step: ConnectionAttempt.Step, started: ContinuousClock.Instant, trace: HostTrace, error: String? = nil, lived: ContinuousClock.Instant? = nil) {
        let elapsed = now() - started
        let milliseconds = Int(elapsed.components.seconds * 1000 + elapsed.components.attoseconds / 1_000_000_000_000_000)
        let livedFor = lived.map { Int((now() - $0).components.seconds) }
        log.record(ConnectionAttempt(at: .now, step: step, ok: error == nil, milliseconds: milliseconds, error: error, tries: trace.values, background: background, lived: livedFor))
        cache.save(log, logKey)
    }

    private func save() {
        guard let hello else { return }
        cache.save(Saved(hello: hello, workspaces: workspaces), savedKey)
    }

    public func start(background: Bool = false) {
        guard task == nil, state != .unauthorized else { return }
        self.background = background
        task = Task { await run() }
    }

    public func stop() {
        task?.cancel()
        task = nil
        streamID = nil
        if state != .unauthorized { move(to: .offline(nil)) }
        saveEvents()
    }

    public func reconnectNow() {
        guard state != .unauthorized else { return }
        if state == .online {
            guard let heardAt, now() - heardAt > Self.pingInterval * 2 else { return }
            streamID = nil
        }
        task?.cancel()
        task = nil
        start()
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
        guard let list = try? await client.workspaces() else { return }
        workspaces = list
        save()
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
        } catch RemoteError.unauthorized {
            note(.command, "failed \(what) err=" + EventLog.code(RemoteError.unauthorized))
            revoke()
            throw RemoteError.unauthorized
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
        do {
            identify(try await client.setIdentityColour(colour.rawValue))
            return computer
        } catch RemoteError.unauthorized {
            revoke()
            throw RemoteError.unauthorized
        }
    }

    public var presence: String {
        switch state {
        case .online: "online"
        case .connecting: "connecting…"
        case .unauthorized: "access revoked"
        case .offline: lastSeen.map { "offline since " + Self.clock($0) } ?? "offline"
        }
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
            do {
                let hello = try await client.hello(trace: trace)
                step = .workspaces
                let list = try await client.workspaces(trace: trace)
                if Task.isCancelled { return }
                self.hello = hello
                workspaces = list
                save()
                identify(hello.computer)
                await loadVoiceSettings()
                await loadAwake()
                if Task.isCancelled { return }
                step = .events
                for try await item in try await client.events(after: lastEventID, trace: trace) {
                    if Task.isCancelled { return }
                    failures = 0
                    lastSeen = .now
                    heardAt = now()
                    switch item {
                    case .alive:
                        move(to: .online)
                        connectedAt = now()
                        record(.events, started: started, trace: trace)
                        emit(.online)
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
                            if Task.isCancelled { return }
                        }
                        if event.topic == Self.voiceSettingsTopic {
                            voiceEnabled = event.payload["enabled"]?.boolValue
                        }
                        if event.topic == Self.awakeTopic, supports(Self.awakeState), let state = try? event.payload.decode(AwakeState.self) {
                            awake = state
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
                record(step, started: started, trace: trace, error: connectedAt == nil ? "stream closed before it said connected" : "stream closed by the Mac", lived: connectedAt)
                streamID = nil
                move(to: .offline(nil), error: RemoteError.unreachable(connectedAt == nil ? "stream closed before it said connected" : "stream closed by the Mac"))
            } catch RemoteError.unauthorized {
                if Task.isCancelled { return }
                record(step, started: started, trace: trace, error: ConnectionAttempt.describe(RemoteError.unauthorized), lived: connectedAt)
                revoke()
                return
            } catch {
                if Task.isCancelled { return }
                record(step, started: started, trace: trace, error: ConnectionAttempt.describe(error), lived: connectedAt)
                streamID = nil
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
            awake = nil
            return
        }
        guard let state = try? await client.command(Self.awakeState, workspace: nil, as: AwakeState.self), !Task.isCancelled else { return }
        awake = state
    }

    public func setAwake(_ on: Bool, minutes: Int = 0) async throws {
        var args: [String: JSONValue] = ["on": .bool(on)]
        if on, minutes > 0 { args["minutes"] = .number(Double(minutes)) }
        awake = try await command(Self.awakeSet, workspace: nil, args: .object(args), as: AwakeState.self)
    }

    private func revoke() {
        move(to: .unauthorized)
        saveEvents()
        streamID = nil
        hello = nil
        voiceEnabled = nil
        awake = nil
        workspaces = []
        task?.cancel()
        task = nil
    }

    private func emit(_ signal: ConnectionSignal) {
        for listener in listeners.values {
            listener(signal)
        }
    }
}

private struct WeakInterface {
    weak var value: WorkspaceUI?
}
