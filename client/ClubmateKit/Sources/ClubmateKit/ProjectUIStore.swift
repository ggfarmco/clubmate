import Foundation
import Observation

@MainActor
@Observable
public final class ProjectUIStore {
    public let projectID: String
    public private(set) var sections: [SectionInfo] = []
    public private(set) var hostedSections: [SectionInfo] = []
    public var sidebarTabs = SidebarTabs()
    public private(set) var slots: [String: SlotView] = [:]
    public private(set) var connected = false
    public private(set) var failure: String?
    public private(set) var focusRequest: FocusRequest?
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
    public var sidebarHidden: [String: Bool] = [:] {
        didSet {
            guard !restoring, sidebarHidden != oldValue else { return }
            updateState("sidebarHidden", .object(sidebarHidden.mapValues { .bool($0) }))
        }
    }
    public var sidebarAutoHidden = false

    @ObservationIgnored private var client: CoreClient
    @ObservationIgnored private var subscription: String?
    @ObservationIgnored private var gate: CheckedContinuation<Void, Never>?
    @ObservationIgnored private var starting = false
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var revs: [String: Int64] = [:]
    @ObservationIgnored private var listener: Task<Void, Never>?
    @ObservationIgnored private var saving: Task<Void, Never>?
    @ObservationIgnored private var restoring = false
    @ObservationIgnored private var focusSerial = 0
    @ObservationIgnored private var requestedSection: String?
    @ObservationIgnored private let order: @MainActor ([SectionInfo]) -> [SectionInfo]

    public init(client: CoreClient, projectID: String, order: @escaping @MainActor ([SectionInfo]) -> [SectionInfo] = { $0 }) {
        self.client = client
        self.projectID = projectID
        self.order = order
    }

    public func start() async throws {
        guard listener == nil else { return }
        let generation = self.generation
        let stream = client.subscribeNotifications()
        starting = true
        listener = Task { [weak self] in
            for await notification in stream {
                let message = await UIMessage.decode(notification)
                await self?.receive(message, generation)
            }
            guard !Task.isCancelled else { return }
            self?.connected = false
        }
        do {
            let sections = try await client.uiSections()
            guard generation == self.generation else { return }
            let scoped = sections.filter { $0.scope == scope }
            self.sections = SectionHosting.rail(scoped)
            hostedSections = SectionHosting.hosted(scoped)
            let views = try await client.uiGet(projectID: projectID)
            guard generation == self.generation else { return }
            replace(with: views)
            let state = isAppScope ? nil : try await client.uiStateGet(projectID: projectID)
            guard generation == self.generation else { return }
            restore(from: state)
            let subscription = try await client.uiSubscribe(projectID: projectID)
            guard generation == self.generation else { return await discard(subscription) }
            self.subscription = subscription
        } catch {
            if generation == self.generation {
                halt()
                if (error as? ClubmateError)?.isTransient != true { failure = describe(error) }
            }
            throw error
        }
        connected = true
        failure = nil
        applyRequestedSection()
        openGate()
    }

    public func show(section: String) {
        requestedSection = section
        if connected { applyRequestedSection() }
    }

    private func applyRequestedSection() {
        guard let requested = requestedSection else { return }
        requestedSection = nil
        select(requested)
    }

    public var allSections: [SectionInfo] {
        sections + hostedSections
    }

    @discardableResult
    public func select(_ section: String, slot: String = "") -> String {
        guard let target = SectionHosting.resolve(section, slot: slot, in: allSections) else { return slot }
        if let tab = target.tab {
            sidebarTabs.choose(tab, in: target.section)
            revealSidebar(target.section)
        }
        selectedSection = target.section
        return target.slot
    }

    public func reconnect(using client: CoreClient) async throws {
        await stop()
        self.client = client
        try await start()
    }

    func attach(_ client: CoreClient) async {
        do {
            try await reconnect(using: client)
        } catch {
            await heal(generation)
        }
    }

    public func stop() async {
        let subscription = self.subscription
        halt()
        if let subscription { await discard(subscription) }
        await saving?.value
    }

    public func toggleSidebar(_ settings: AppSettings) {
        guard let selectedSection else { return }
        if settings.hidesSidebar(selectedSection, manual: sidebarHidden[selectedSection] == true, autoHidden: sidebarAutoHidden) {
            revealSidebar(selectedSection)
        } else {
            sidebarHidden[selectedSection] = true
        }
    }

    public func revealSidebar(_ section: String) {
        sidebarHidden[section] = nil
        sidebarAutoHidden = false
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

    private func receive(_ message: UIMessage, _ generation: Int) async {
        guard generation == self.generation, !Task.isCancelled else { return }
        if starting {
            await withCheckedContinuation { gate = $0 }
            guard generation == self.generation else { return }
        }
        if route(message) { await recover() }
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

    private func route(_ message: UIMessage) -> Bool {
        switch message {
        case .focus(let params):
            focus(params)
            return false
        case .view(let params, let closed):
            guard params.subscription == subscription else { return false }
            if let view = params.view { apply(view) }
            return closed
        case .ignored:
            return false
        }
    }

    public func requestFocus(section: String, slot: String) {
        let slot = select(section, slot: slot)
        focusSerial += 1
        focusRequest = FocusRequest(slot: slot, serial: focusSerial)
    }

    private func focus(_ params: UIFocusNotification) {
        guard params.subscription == subscription else { return }
        let slot = select(params.section, slot: params.slot)
        focusSerial += 1
        focusRequest = FocusRequest(slot: slot, serial: focusSerial, edge: params.edge)
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
        Task { [weak self] in await self?.heal(generation) }
    }

    private func heal(_ generation: Int) async {
        let client = self.client
        guard generation == self.generation, client.isAlive else { return }
        await stop()
        guard generation + 1 == self.generation, client === self.client else { return }
        do {
            try await start()
        } catch {
            failure = describe(error)
        }
    }

    private var isAppScope: Bool { projectID.isEmpty }

    private var scope: String { isAppScope ? "app" : "project" }

    private func describe(_ error: any Error) -> String {
        (error as? ClubmateError)?.message ?? "\(error)"
    }

    private func replace(with views: [SlotView]) {
        let mine = views.filter { $0.projectId == projectID }
        slots = Dictionary(mine.map { ($0.slot, $0) }) { _, last in last }
        revs = slots.mapValues(\.rev)
        sidebarTabs.reviewSeen(slots[ReviewSlot.slot]?.tree ?? SDUINode(type: "empty"))
    }

    private func apply(_ view: SlotView) {
        guard view.projectId == projectID, view.rev > revs[view.slot] ?? 0 else { return }
        revs[view.slot] = view.rev
        slots[view.slot] = view
        if view.slot == ReviewSlot.slot { sidebarTabs.reviewSeen(view.tree) }
    }

    private func restore(from state: JSONValue?) {
        let saved = state?.objectValue ?? [:]
        restoring = true
        selectedSection = order(sections).first?.id
        if let section = saved["section"]?.stringValue { select(section) }
        sidebarWidth = saved["sidebarWidth"]?.numberValue
        sidebarHidden = saved["sidebarHidden"]?.objectValue?.compactMapValues(\.boolValue) ?? [:]
        restoring = false
        if let selectedSection, saved["section"]?.stringValue != selectedSection { updateState("section", .string(selectedSection)) }
    }

    private func updateState(_ key: String, _ value: JSONValue) {
        guard !isAppScope else { return }
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

public struct FocusRequest: Equatable, Sendable {
    public let slot: String
    public let serial: Int
    public let edge: String?

    public init(slot: String, serial: Int, edge: String? = nil) {
        self.slot = slot
        self.serial = serial
        self.edge = edge
    }

    public func accepts(side: SidebarSide) -> Bool {
        switch edge {
        case nil: true
        case "h": side == .left
        case "l": side == .right
        default: false
        }
    }
}

private enum UIMessage: Sendable {
    case view(UINotification, closed: Bool)
    case focus(UIFocusNotification)
    case ignored

    @concurrent static func decode(_ notification: RPCNotification) async -> UIMessage {
        switch notification.method {
        case "ui.focus":
            return (try? notification.params.decode(UIFocusNotification.self)).map { .focus($0) } ?? .ignored
        case "ui.updated", "ui.closed":
            return (try? notification.params.decode(UINotification.self)).map { .view($0, closed: notification.method == "ui.closed") } ?? .ignored
        default:
            return .ignored
        }
    }
}

private struct UIFocusNotification: Decodable, Sendable {
    let subscription: String
    let section: String
    let slot: String
    let edge: String?
}

private struct UINotification: Decodable, Sendable {
    let subscription: String
    let view: SlotView?
}
