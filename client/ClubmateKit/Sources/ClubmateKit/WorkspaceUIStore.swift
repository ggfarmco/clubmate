import Foundation
import Observation

@MainActor
@Observable
public final class WorkspaceUIStore {
    public let workspaceID: String
    public let drafts = ComposerDrafts()
    public private(set) var sections: [SectionInfo] = []
    public private(set) var hostedSections: [SectionInfo] = []
    public private(set) var popupSlots: [String] = []
    private var popupSections: [SectionInfo] = []
    private var popupOrder: [String] = []
    public private(set) var connected = false
    public private(set) var failure: String?
    public private(set) var focusRequest: FocusRequest?
    public var selectedSection: String? {
        didSet {
            guard !restoring, selectedSection != oldValue, let selectedSection else { return }
            updateState("section", .string(selectedSection))
            refocus(selectedSection)
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
    public var chatWidth: Double? {
        didSet {
            guard !restoring, chatWidth != oldValue, let chatWidth else { return }
            updateState("chatWidth", .number(chatWidth))
        }
    }

    @ObservationIgnored private var client: CoreClient
    @ObservationIgnored private var subscription: String?
    @ObservationIgnored private var gate: CheckedContinuation<Void, Never>?
    @ObservationIgnored private var starting = false
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var revs: [String: Int64] = [:]
    @ObservationIgnored private var cells: [String: SlotCell] = [:]
    @ObservationIgnored private var listener: Task<Void, Never>?
    @ObservationIgnored private var pluginWatch: Task<Void, Never>?
    @ObservationIgnored private var saving: Task<Void, Never>?
    @ObservationIgnored private var restoring = false
    @ObservationIgnored private var focusSerial = 0
    @ObservationIgnored private var focusedSlots: [String: String] = [:]
    @ObservationIgnored private var requestedSection: String?
    @ObservationIgnored private var legacyChat: LegacyChat?
    @ObservationIgnored private let order: @MainActor ([SectionInfo]) -> [SectionInfo]

    public init(
        client: CoreClient,
        workspaceID: String,
        legacyChat: LegacyChat? = nil,
        order: @escaping @MainActor ([SectionInfo]) -> [SectionInfo] = { $0 }
    ) {
        self.client = client
        self.workspaceID = workspaceID
        self.legacyChat = legacyChat
        self.order = order
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
        let client = self.client
        pluginWatch = Task { [weak self] in
            guard let events = try? await client.busEvents(pattern: Topic.pluginChanged) else { return }
            for await _ in events {
                await self?.refreshSections(generation)
            }
        }
        do {
            let sections = try await client.uiSections()
            guard generation == self.generation else { return }
            adopt(sections)
            let views = try await client.uiGet(workspaceID: workspaceID)
            guard generation == self.generation else { return }
            replace(with: views)
            let state = isAppScope ? nil : try await client.uiStateGet(workspaceID: workspaceID)
            guard generation == self.generation else { return }
            restore(from: state)
            let subscription = try await client.uiSubscribe(workspaceID: workspaceID)
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

    private func adopt(_ sections: [SectionInfo]) {
        let scoped = sections.filter { $0.scope == scope }
        let rail = SectionHosting.rail(scoped)
        let hosted = SectionHosting.hosted(scoped)
        if self.sections != rail { self.sections = rail }
        if hostedSections != hosted { hostedSections = hosted }
        popupSections = scoped.filter { !Popups.slots([$0]).isEmpty }
        let popups = Popups.slots(popupSections)
        if popupSlots != popups { popupSlots = popups }
    }

    private func refreshSections(_ generation: Int) async {
        guard generation == self.generation, let sections = try? await client.uiSections(), generation == self.generation else { return }
        adopt(sections)
        guard connected, !self.sections.contains(where: { $0.id == selectedSection }) else { return }
        selectedSection = order(self.sections).first?.id
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

    public var popup: String? {
        let views = Dictionary(popupSlots.compactMap { name in slot(name).map { (name, $0) } }) { _, last in last }
        return Popups.current(views, order: popupOrder)
    }

    public func popupTitle(_ slot: String) -> String {
        popupSections.first { "\($0.id).\(Popups.name)" == slot }?.title ?? ""
    }

    public func slot(_ name: String) -> SlotView? {
        cell(name).view
    }

    public func hasSlot(_ name: String) -> Bool {
        cell(name).present
    }

    public func surfaceSessionID(_ name: String) -> String? {
        let cell = cell(name)
        cell.track(.surface)
        return cell.surface
    }

    public func isOpen(_ name: String) -> Bool {
        cell(name).open
    }

    public func hasTextField(_ name: String) -> Bool {
        let cell = cell(name)
        cell.track(.textField)
        return cell.hasTextField
    }

    public func hasComposer(_ name: String) -> Bool {
        let cell = cell(name)
        cell.track(.composer)
        return cell.hasComposer
    }

    public func firstTabs(_ name: String) -> SDUINode? {
        let cell = cell(name)
        cell.track(.tabs)
        return cell.tabs
    }

    private func cell(_ name: String) -> SlotCell {
        if let cell = cells[name] { return cell }
        let cell = SlotCell()
        cells[name] = cell
        return cell
    }

    public var allSections: [SectionInfo] {
        sections + hostedSections
    }

    @discardableResult
    public func select(_ section: String, slot: String = "") -> String {
        guard let target = SectionHosting.resolve(section, slot: slot, in: allSections) else { return slot }
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

    public var sidebarSlot: String? {
        guard let selectedSection, selectedSection != CodeSection.id, !isSidebarHidden(selectedSection) else { return nil }
        return "\(selectedSection).sidebar"
    }

    public var togglesChat: Bool {
        selectedSection == CodeSection.id && hasSlot(CodeSection.chatSlot)
    }

    public var columnShown: Bool {
        togglesChat && !isSidebarHidden(CodeSection.id)
    }

    public var columnSlot: String? {
        columnShown ? CodeSection.chatSlot : nil
    }

    public func toggleSidebar() {
        guard let selectedSection, selectedSection != CodeSection.id || togglesChat else { return }
        setSidebarHidden(selectedSection, !isSidebarHidden(selectedSection))
    }

    public func revealSidebar(_ section: String) {
        setSidebarHidden(section, false)
    }

    private func isSidebarHidden(_ section: String) -> Bool {
        sidebarHidden[section] ?? Self.hiddenByDefault(section)
    }

    private func setSidebarHidden(_ section: String, _ hidden: Bool) {
        sidebarHidden[section] = hidden == Self.hiddenByDefault(section) ? nil : hidden
    }

    private static func hiddenByDefault(_ section: String) -> Bool {
        section == CodeSection.id
    }

    public func send(_ event: UIEvent) async -> Result<JSONValue, ClubmateError> {
        await result { try await self.client.uiEvent(event) }
    }

    public func run(_ invocation: CommandInvocation) async -> Result<JSONValue, ClubmateError> {
        await result { try await self.client.commandRun(id: invocation.id, workspaceID: self.workspaceID, args: invocation.args) }
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
        guard generation == self.generation, !Task.isCancelled, UIMessage.carries(notification.method) else { return }
        if starting {
            await withCheckedContinuation { gate = $0 }
            guard generation == self.generation else { return }
        }
        guard notification.subscription == subscription else { return }
        let message = await UIMessage.decode(notification)
        guard generation == self.generation else { return }
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
        pluginWatch?.cancel()
        pluginWatch = nil
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
            if let update = params.view, !apply(update) { return true }
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

    public func rememberFocus(_ slot: String) {
        guard let section = slot.split(separator: ".", maxSplits: 1).first, !section.isEmpty else { return }
        focusedSlots[String(section)] = slot
    }

    private func refocus(_ section: String) {
        guard let slot = focusedSlots[section], !slot.hasPrefix("\(section).sidebar") || !isSidebarHidden(section) else { return }
        focusSerial += 1
        focusRequest = FocusRequest(slot: slot, serial: focusSerial)
    }

    private func focus(_ params: UIFocusNotification) {
        guard params.subscription == subscription else { return }
        let slot = select(params.section, slot: params.slot)
        guard !slot.isEmpty else { return }
        focusSerial += 1
        focusRequest = FocusRequest(slot: slot, serial: focusSerial, field: params.field ?? false)
    }

    private func recover() async {
        let generation = self.generation
        let stale = subscription
        revs = [:]
        guard let subscription = try? await client.uiSubscribe(workspaceID: workspaceID) else {
            return lose(generation)
        }
        if let stale { await discard(stale) }
        let views = try? await client.uiGet(workspaceID: workspaceID)
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

    private var isAppScope: Bool { workspaceID.isEmpty }

    private var scope: String { isAppScope ? "app" : "workspace" }

    private func describe(_ error: any Error) -> String {
        (error as? ClubmateError)?.message ?? "\(error)"
    }

    private func replace(with views: [SlotView]) {
        let mine = Dictionary(views.filter { $0.workspaceId == workspaceID }.map { ($0.slot, $0) }) { _, last in last }
        for name in cells.keys where mine[name] == nil { cell(name).set(nil) }
        for (name, view) in mine { store(view, in: name) }
        revs = mine.mapValues(\.rev)
    }

    private func apply(_ update: SlotUpdate) -> Bool {
        guard update.workspaceId == workspaceID, update.rev > revs[update.slot] ?? 0 else { return true }
        guard let view = update.view(over: cells[update.slot]?.view) else { return false }
        revs[view.slot] = view.rev
        store(view, in: view.slot)
        return true
    }

    private func store(_ view: SlotView, in name: String) {
        let cell = cell(name)
        if popupSlots.contains(name), Popups.isOpen(view), cell.view.map(Popups.isOpen) != true {
            popupOrder.removeAll { $0 == name }
            popupOrder.append(name)
        }
        cell.set(view)
    }

    private func restore(from state: JSONValue?) {
        let saved = state?.objectValue ?? [:]
        restoring = true
        selectedSection = order(sections).first?.id
        if let section = saved["section"]?.stringValue { select(section) }
        sidebarWidth = saved["sidebarWidth"]?.numberValue
        sidebarHidden = saved["sidebarHidden"]?.objectValue?.compactMapValues(\.boolValue) ?? [:]
        chatWidth = saved["chatWidth"]?.numberValue
        restoring = false
        adoptLegacyChat()
        if let selectedSection, saved["section"]?.stringValue != selectedSection { updateState("section", .string(selectedSection)) }
    }

    private func adoptLegacyChat() {
        guard let legacy = legacyChat else { return }
        legacyChat = nil
        if legacy.shown, sidebarHidden[CodeSection.id] == nil { revealSidebar(CodeSection.id) }
        if chatWidth == nil, let width = legacy.width { chatWidth = width }
    }

    private func updateState(_ key: String, _ value: JSONValue) {
        guard !isAppScope else { return }
        let previous = saving
        saving = Task {
            await previous?.value
            try? await client.uiStatePatch(workspaceID: workspaceID, key: key, value: value)
        }
    }
}

@MainActor
@Observable
private final class SlotCell {
    private(set) var view: SlotView?
    private(set) var present = false
    private(set) var open = false
    private(set) var surface: String?
    private(set) var hasTextField = false
    private(set) var hasComposer = false
    private(set) var tabs: SDUINode?
    @ObservationIgnored private var tracked: Set<SlotFact> = []
    @ObservationIgnored private var tree: SDUINode?

    func set(_ view: SlotView?) {
        guard self.view != view else { return }
        self.view = view
        tree = view?.tree
        if present != (view != nil) { present = view != nil }
        let open = view.map(Popups.isOpen) ?? false
        if self.open != open { self.open = open }
        refresh(tracked)
    }

    func track(_ fact: SlotFact) {
        guard tracked.insert(fact).inserted else { return }
        refresh([fact])
    }

    private func refresh(_ facts: Set<SlotFact>) {
        if facts.contains(.surface) {
            let surface = tree?.surfaceSessionID
            if self.surface != surface { self.surface = surface }
        }
        if facts.contains(.textField) {
            let hasTextField = tree?.hasTextField ?? false
            if self.hasTextField != hasTextField { self.hasTextField = hasTextField }
        }
        if facts.contains(.composer) {
            let hasComposer = tree?.hasComposer ?? false
            if self.hasComposer != hasComposer { self.hasComposer = hasComposer }
        }
        if facts.contains(.tabs) {
            let tabs = tree?.firstTabs
            if self.tabs != tabs { self.tabs = tabs }
        }
    }
}

private enum SlotFact: Hashable {
    case surface
    case textField
    case composer
    case tabs
}

public struct FocusRequest: Equatable, Sendable {
    public let slot: String
    public let serial: Int
    public let field: Bool

    public init(slot: String, serial: Int, field: Bool = false) {
        self.slot = slot
        self.serial = serial
        self.field = field
    }
}

private enum UIMessage: Sendable {
    case view(UINotification, closed: Bool)
    case focus(UIFocusNotification)
    case ignored

    static func carries(_ method: String) -> Bool {
        ["ui.focus", "ui.updated", "ui.closed"].contains(method)
    }

    @concurrent static func decode(_ notification: RPCNotification) async -> UIMessage {
        switch notification.method {
        case "ui.focus":
            return (try? notification.decodeParams(UIFocusNotification.self)).map { .focus($0) } ?? .ignored
        case "ui.updated", "ui.closed":
            return (try? notification.decodeParams(UINotification.self)).map { .view($0, closed: notification.method == "ui.closed") } ?? .ignored
        default:
            return .ignored
        }
    }
}

private struct UIFocusNotification: Decodable, Sendable {
    let subscription: String
    let section: String
    let slot: String
    let field: Bool?
}

private struct UINotification: Decodable, Sendable {
    let subscription: String
    let view: SlotUpdate?
}
