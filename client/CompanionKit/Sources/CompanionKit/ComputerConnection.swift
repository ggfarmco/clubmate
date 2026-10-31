import Foundation
import Observation

public enum ConnectionSignal: Sendable, Equatable {
    case online
    case event(BusEvent)
}

public protocol CommandRunner: AnyObject {
    @MainActor func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue
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

    public static let backoff: [Duration] = [1, 2, 5, 10, 30].map { .seconds($0) }

    public private(set) var computer: Computer
    public private(set) var state: State = .offline(nil)
    public private(set) var hello: Hello?
    public private(set) var workspaces: [Workspace] = []
    public private(set) var lastEventID: Int?

    @ObservationIgnored public private(set) var client: RemoteClient
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var listeners: [UUID: @MainActor (ConnectionSignal) -> Void] = [:]

    public init(computer: Computer, token: String?, transport: any HTTPTransport, policy: HostPolicy = .tailnet, lastEventID: Int? = nil, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.computer = computer
        self.lastEventID = lastEventID
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
        do {
            return try await client.command(id, workspace: workspace, args: args)
        } catch RemoteError.unauthorized {
            revoke()
            throw RemoteError.unauthorized
        }
    }

    public func supports(_ command: String) -> Bool {
        hello?.commands.contains(command) ?? false
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
                computer.name = hello.computer.name
                for try await item in try await client.events(after: lastEventID) {
                    if Task.isCancelled { return }
                    failures = 0
                    switch item {
                    case .alive:
                        state = .online
                        emit(.online)
                    case .event(let event):
                        lastEventID = event.seq
                        if event.topic == Topic.pluginChanged, let fresh = try? await client.hello() {
                            if Task.isCancelled { return }
                            self.hello = fresh
                        }
                        emit(.event(event))
                    }
                }
                if Task.isCancelled { return }
                state = .offline(nil)
            } catch RemoteError.unauthorized {
                if Task.isCancelled { return }
                revoke()
                return
            } catch {
                if Task.isCancelled || error is CancellationError { return }
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

    private func revoke() {
        state = .unauthorized
        hello = nil
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
