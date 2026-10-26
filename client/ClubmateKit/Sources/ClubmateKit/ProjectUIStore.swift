import Foundation
import Observation

@MainActor
@Observable
public final class ProjectUIStore {
    public let projectID: String
    public private(set) var sections: [SectionInfo] = []
    public private(set) var slots: [String: SlotView] = [:]
    public private(set) var connected = false
    public var selectedSection: String? {
        didSet {
            guard !restoring, selectedSection != oldValue, let selectedSection else { return }
            updateState("section", .string(selectedSection))
        }
    }
    public var sidebarWidth: Double? {
        didSet {
            guard !restoring, sidebarWidth != oldValue, let sidebarWidth else { return }
            updateState("sidebarWidth", .number(sidebarWidth))
        }
    }

    @ObservationIgnored private var client: CoreClient
    @ObservationIgnored private var subscription: String?
    @ObservationIgnored private var gate: CheckedContinuation<Void, Never>?
    @ObservationIgnored private var starting = false
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var revs: [String: Int64] = [:]
    @ObservationIgnored private var listener: Task<Void, Never>?
    @ObservationIgnored private var saving: Task<Void, Never>?
    @ObservationIgnored private var restoring = false

    public init(client: CoreClient, projectID: String) {
        self.client = client
        self.projectID = projectID
    }

    public func start() async throws {
        guard listener == nil else { return }
        let generation = self.generation
        let stream = client.subscribeNotifications()
        starting = true
        listener = Task { [weak self] in
            for await notification in stream {
                await self?.receive(notification, generation)
            }
            guard !Task.isCancelled else { return }
            self?.connected = false
        }
        do {
            let sections = try await client.uiSections()
            guard generation == self.generation else { return }
            self.sections = sections
            let views = try await client.uiGet(projectID: projectID)
            guard generation == self.generation else { return }
            replace(with: views)
            let state = try await client.uiStateGet(projectID: projectID)
            guard generation == self.generation else { return }
            restore(from: state)
            let subscription = try await client.uiSubscribe(projectID: projectID)
            guard generation == self.generation else { return await discard(subscription) }
            self.subscription = subscription
        } catch {
            if generation == self.generation { halt() }
            throw error
        }
        connected = true
        openGate()
    }

    public func reconnect(using client: CoreClient) async throws {
        await stop()
        self.client = client
        try await start()
    }

    public func stop() async {
        let subscription = self.subscription
        halt()
        if let subscription { await discard(subscription) }
        await saving?.value
    }

    public func send(_ event: UIEvent) async -> Result<JSONValue, ClubmateError> {
        await result { try await self.client.uiEvent(event) }
    }

    public func run(_ invocation: CommandInvocation) async -> Result<JSONValue, ClubmateError> {
        await result { try await self.client.commandRun(id: invocation.id, projectID: self.projectID, args: invocation.args) }
    }

    private func result(_ call: () async throws -> JSONValue) async -> Result<JSONValue, ClubmateError> {
        do {
            return .success(try await call())
        } catch let error as ClubmateError {
            return .failure(error)
        } catch {
            return .failure(.protocolError("\(error)"))
        }
    }

    private func receive(_ notification: RPCNotification, _ generation: Int) async {
        guard generation == self.generation, !Task.isCancelled else { return }
        if starting {
            await withCheckedContinuation { gate = $0 }
            guard generation == self.generation else { return }
        }
        if route(notification) { await recover() }
    }

    private func openGate() {
        starting = false
        gate?.resume()
        gate = nil
    }

    private func halt() {
        generation += 1
        listener?.cancel()
        listener = nil
        subscription = nil
        connected = false
        openGate()
    }

    private func discard(_ subscription: String) async {
        try? await client.uiUnsubscribe(subscription)
    }

    private func route(_ notification: RPCNotification) -> Bool {
        guard notification.method == "ui.updated" || notification.method == "ui.closed",
              let params = try? notification.params.decode(UINotification.self),
              params.subscription == subscription
        else { return false }
        if let view = params.view { apply(view) }
        return notification.method == "ui.closed"
    }

    private func recover() async {
        let generation = self.generation
        revs = [:]
        guard let subscription = try? await client.uiSubscribe(projectID: projectID) else {
            return lose(generation)
        }
        let views = try? await client.uiGet(projectID: projectID)
        guard generation == self.generation, let views else {
            await discard(subscription)
            return lose(generation)
        }
        self.subscription = subscription
        replace(with: views)
    }

    private func lose(_ generation: Int) {
        guard generation == self.generation else { return }
        subscription = nil
        connected = false
    }

    private func replace(with views: [SlotView]) {
        let mine = views.filter { $0.projectId == projectID }
        slots = Dictionary(mine.map { ($0.slot, $0) }) { _, last in last }
        revs = slots.mapValues(\.rev)
    }

    private func apply(_ view: SlotView) {
        guard view.projectId == projectID, view.rev > revs[view.slot] ?? 0 else { return }
        revs[view.slot] = view.rev
        slots[view.slot] = view
    }

    private func restore(from state: JSONValue?) {
        let saved = state?.objectValue ?? [:]
        restoring = true
        defer { restoring = false }
        selectedSection = sections.first { $0.id == saved["section"]?.stringValue }?.id ?? sections.first?.id
        sidebarWidth = saved["sidebarWidth"]?.numberValue
    }

    private func updateState(_ key: String, _ value: JSONValue) {
        let previous = saving
        saving = Task {
            await previous?.value
            try? await mergeState(key, value)
        }
    }

    private func mergeState(_ key: String, _ value: JSONValue) async throws {
        var state = try await client.uiStateGet(projectID: projectID)?.objectValue ?? [:]
        state[key] = value
        try await client.uiStateSet(projectID: projectID, state: .object(state))
    }
}

private struct UINotification: Decodable {
    let subscription: String
    let view: SlotView?
}
