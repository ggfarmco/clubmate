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

    @ObservationIgnored private var client: CoreClient
    @ObservationIgnored private var subscription: String?
    @ObservationIgnored private var backlog: [RPCNotification] = []
    @ObservationIgnored private var revs: [String: Int64] = [:]
    @ObservationIgnored private var listener: Task<Void, Never>?
    @ObservationIgnored private var saving: Task<Void, Never>?
    @ObservationIgnored private var restoring = false

    public init(client: CoreClient, projectID: String) {
        self.client = client
        self.projectID = projectID
    }

    public func start() async throws {
        let stream = client.subscribeNotifications()
        listener = Task { [weak self] in
            for await notification in stream {
                await self?.receive(notification)
            }
            guard !Task.isCancelled else { return }
            self?.connected = false
        }
        do {
            sections = try await client.uiSections()
            replace(with: try await client.uiGet(projectID: projectID))
            try await restoreSection()
            subscription = try await client.uiSubscribe(projectID: projectID)
        } catch {
            listener?.cancel()
            listener = nil
            backlog = []
            throw error
        }
        connected = true
        let early = backlog
        backlog = []
        var closed = false
        for notification in early {
            closed = route(notification) || closed
        }
        if closed { await recover() }
    }

    public func reconnect(using client: CoreClient) async throws {
        await stop()
        self.client = client
        try await start()
    }

    public func stop() async {
        listener?.cancel()
        listener = nil
        if let subscription {
            try? await client.uiUnsubscribe(subscription)
        }
        subscription = nil
        backlog = []
        connected = false
        await saving?.value
    }

    public func send(_ event: UIEvent) async -> Result<JSONValue, ClubmateError> {
        do {
            return .success(try await client.uiEvent(event))
        } catch let error as ClubmateError {
            return .failure(error)
        } catch {
            return .failure(.protocolError("\(error)"))
        }
    }

    private func receive(_ notification: RPCNotification) async {
        guard subscription != nil else {
            backlog.append(notification)
            return
        }
        if route(notification) { await recover() }
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
        revs = [:]
        do {
            subscription = try await client.uiSubscribe(projectID: projectID)
            replace(with: try await client.uiGet(projectID: projectID))
        } catch {
            connected = false
        }
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

    private func restoreSection() async throws {
        let saved = try await client.uiStateGet(projectID: projectID)?.objectValue?["section"]?.stringValue
        restoring = true
        defer { restoring = false }
        selectedSection = sections.first { $0.id == saved }?.id ?? sections.first?.id
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
