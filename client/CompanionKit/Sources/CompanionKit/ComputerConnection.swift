import Foundation
import Observation

public enum ConnectionSignal: Sendable, Equatable {
    case online
    case event(BusEvent)
}

public protocol CommandRunner: AnyObject {
    @MainActor func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue
    @MainActor func command<T: Decodable & Sendable>(_ id: String, workspace: String?, args: JSONValue?, as type: T.Type) async throws -> T
}

extension CommandRunner {
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
    public static let backoff: [Duration] = [1, 2, 5, 10, 30].map { .seconds($0) }

    public private(set) var computer: Computer
    public private(set) var state: State = .offline(nil)
    public private(set) var hello: Hello?
    public private(set) var workspaces: [Workspace] = []
    public private(set) var lastEventID: Int?
    public private(set) var lastSeen: Date?
    public private(set) var voiceEnabled: Bool?
    @ObservationIgnored public private(set) var streamID: String?

    @ObservationIgnored public private(set) var client: RemoteClient
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var listeners: [UUID: @MainActor (ConnectionSignal) -> Void] = [:]
    @ObservationIgnored private var interfaces: [String: WeakInterface] = [:]

    public init(computer: Computer, token: String?, transport: any HTTPTransport, policy: HostPolicy = .tailnet, lastEventID: Int? = nil, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.computer = computer
        self.lastEventID = lastEventID
        lastSeen = computer.lastSeen
        self.sleep = sleep
        client = RemoteClient(hosts: computer.hosts, port: computer.port, token: token, transport: transport, policy: policy)
        if token == nil { state = .unauthorized }
    }

    public func start() {
        guard task == nil, state != .unauthorized else { return }
        task = Task { await run() }
    }

    public func stop() {
        task?.cancel()
        task = nil
        streamID = nil
        if state != .unauthorized { state = .offline(nil) }
    }

    public func reconnectNow() {
        guard state != .online, state != .unauthorized else { return }
        task?.cancel()
        task = nil
        start()
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
    }

    public func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        try await authorized { try await $0.command(id, workspace: workspace, args: args) }
    }

    public func command<T: Decodable & Sendable>(_ id: String, workspace: String?, args: JSONValue?, as type: T.Type) async throws -> T {
        try await authorized { try await $0.command(id, workspace: workspace, args: args, as: type) }
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
        try await authorized { try await $0.uiSections() }
    }

    func uiGet(workspace: String, slots: [String]) async throws -> [SlotView] {
        try await authorized { try await $0.uiGet(workspace: workspace, slots: slots) }
    }

    func uiWatch(stream: String, workspace: String, slots: [String]) async throws {
        try await authorized { try await $0.uiWatch(stream: stream, workspace: workspace, slots: slots) }
    }

    func uiEvent(workspace: String, slot: String, nodeId: String, action: String, value: JSONValue?) async throws -> JSONValue {
        try await authorized { try await $0.uiEvent(workspace: workspace, slot: slot, nodeId: nodeId, action: action, value: value) }
    }

    private func authorized<T: Sendable>(_ call: (RemoteClient) async throws -> T) async throws -> T {
        do {
            return try await call(client)
        } catch RemoteError.unauthorized {
            revoke()
            throw RemoteError.unauthorized
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
        let offline = "Can't answer — \(computer.name) is offline"
        switch state {
        case .online: return nil
        case .unauthorized: return "Can't answer — access revoked"
        case .offline, .connecting: return lastSeen.map { offline + " · as of " + Self.clock($0) } ?? offline
        }
    }

    public var queues: Bool {
        switch state {
        case .offline, .connecting: true
        case .online, .unauthorized: false
        }
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
            if state != .online { state = .connecting }
            do {
                let hello = try await client.hello()
                let list = try await client.workspaces()
                if Task.isCancelled { return }
                self.hello = hello
                workspaces = list
                identify(hello.computer)
                await loadVoiceSettings()
                if Task.isCancelled { return }
                for try await item in try await client.events(after: lastEventID) {
                    if Task.isCancelled { return }
                    failures = 0
                    lastSeen = .now
                    switch item {
                    case .alive:
                        state = .online
                        emit(.online)
                    case .event(let event):
                        lastEventID = event.seq
                        if event.topic == Topic.pluginChanged {
                            for ui in interfaces.values.compactMap(\.value) {
                                ui.reloadSections()
                            }
                        }
                        if event.topic == Topic.pluginChanged, let fresh = try? await client.hello() {
                            if Task.isCancelled { return }
                            self.hello = fresh
                            identify(fresh.computer)
                            await loadVoiceSettings()
                            if Task.isCancelled { return }
                        }
                        if event.topic == Self.voiceSettingsTopic {
                            voiceEnabled = event.payload["enabled"]?.boolValue
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
                        lastEventID = seq
                    }
                }
                if Task.isCancelled { return }
                streamID = nil
                state = .offline(nil)
            } catch RemoteError.unauthorized {
                if Task.isCancelled { return }
                revoke()
                return
            } catch {
                if Task.isCancelled || error is CancellationError { return }
                streamID = nil
                state = .offline((error as? RemoteError)?.message ?? error.localizedDescription)
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

    private func revoke() {
        state = .unauthorized
        streamID = nil
        hello = nil
        voiceEnabled = nil
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
