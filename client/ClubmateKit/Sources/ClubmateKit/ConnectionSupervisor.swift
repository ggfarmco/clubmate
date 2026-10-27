import Foundation
import Observation

@MainActor
@Observable
public final class ConnectionSupervisor {
    public enum Phase: Equatable {
        case connecting
        case ready
        case reconnecting(attempt: Int)
        case failed(String)
    }

    public private(set) var phase: Phase = .connecting
    public private(set) var client: CoreClient?

    @ObservationIgnored private let connect: @MainActor () async throws -> CoreClient
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var stores: [ObjectIdentifier: ProjectUIStore] = [:]
    @ObservationIgnored private var started = false
    @ObservationIgnored private var running = false

    public init(
        connect: @escaping @MainActor () async throws -> CoreClient,
        sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.connect = connect
        self.sleep = sleep
    }

    public func start() {
        guard !started else { return }
        started = true
        supervise()
    }

    public func retry() {
        guard !running, case .failed = phase else { return }
        supervise()
    }

    public func register(_ store: ProjectUIStore) {
        stores[ObjectIdentifier(store)] = store
        if phase == .ready, let client {
            Task { await store.attach(client) }
        }
    }

    public func unregister(_ store: ProjectUIStore) {
        stores[ObjectIdentifier(store)] = nil
    }

    private func supervise() {
        running = true
        Task {
            var immediate = true
            while let client = await establish(immediate: immediate) {
                await bind(client)
                for await _ in client.subscribeNotifications() {}
                await client.close()
                if client.stoppedIntentionally {
                    phase = .failed("Core stopped")
                    running = false
                    return
                }
                immediate = false
            }
            phase = .failed("Core unreachable")
            running = false
        }
    }

    private func establish(immediate: Bool) async -> CoreClient? {
        if immediate {
            phase = .connecting
            if let client = try? await connect() { return client }
        }
        for attempt in 1...10 {
            phase = .reconnecting(attempt: attempt)
            try? await sleep(min(.milliseconds(500) * (1 << (attempt - 1)), .seconds(5)))
            if let client = try? await connect() { return client }
        }
        return nil
    }

    private func bind(_ client: CoreClient) async {
        self.client = client
        phase = .ready
        await withTaskGroup(of: Void.self) { group in
            for store in stores.values {
                group.addTask { await store.attach(client) }
            }
        }
    }
}
