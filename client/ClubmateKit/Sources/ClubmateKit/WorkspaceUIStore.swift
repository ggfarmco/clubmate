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
    private var launchSettled = false
    public var selectedSection: String? {
        didSet {
            guard !restoring, selectedSection != oldValue, let selectedSection else { return }
            updateState("section", .string(selectedSection))
            refocus(selectedSection)
            cacheLayout()
        }
    }
    public var sidebarWidth: Double? {
        didSet {
            guard !restoring, sidebarWidth != oldValue, let sidebarWidth else { return }
            updateState("sidebarWidth", .number(sidebarWidth))
            cacheLayout()
        }
    }
    public var sidebarHidden: [String: Bool] = [:] {
        didSet {
            guard !restoring, sidebarHidden != oldValue else { return }
            updateState("sidebarHidden", .object(sidebarHidden.mapValues { .bool($0) }))
            cacheLayout()
        }
    }
    public var chatHidden: [String: Bool] = [:] {
        didSet {
            guard !restoring, chatHidden != oldValue else { return }
            updateState("chatHidden", .object(chatHidden.mapValues { .bool($0) }))
            cacheLayout()
        }
    }
    public private(set) var chatHintShown = false
    public var chatWidth: Double? {
        didSet {
            guard !restoring, chatWidth != oldValue, let chatWidth else { return }
            updateState("chatWidth", .number(chatWidth))
            cacheLayout()
        }
    }
    public var asideWidth: Double? {
        didSet {
            guard !restoring, asideWidth != oldValue, let asideWidth else { return }
            updateState("asideWidth", .number(asideWidth))
        }
    }

    @ObservationIgnored private var client: CoreClient
    @ObservationIgnored private var subscription: String?
    @ObservationIgnored private var gate: CheckedContinuation<Void, Never>?
    @ObservationIgnored private var starting = false
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var epoch = 0
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
    @ObservationIgnored private let launchCache: LaunchCache?
    @ObservationIgnored private let chatHint: ChatHint
    public let launchRecord: LaunchRecord?

    public init(
        client: CoreClient,
        workspaceID: String,
        legacyChat: LegacyChat? = nil,
        launchCache: LaunchCache? = nil,
        chatHint: ChatHint = ChatHint(),
        order: @escaping @MainActor ([SectionInfo]) -> [SectionInfo] = { $0 }
    ) {
        self.chatHint = chatHint
        self.client = client
        self.workspaceID = workspaceID
        self.legacyChat = legacyChat
        self.launchCache = workspaceID.isEmpty ? nil : launchCache
        launchRecord = workspaceID.isEmpty ? nil : launchCache?.record(workspaceID)
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
        if !launchSettled { launchSettled = true }
        applyRequestedSection()
        cacheLayout()
        openGate()
    }

    private func adopt(_ sections: [SectionInfo]) {
        launchCache?.plugins(Set(sections.map(\.plugin)))
        let scoped = sections.filter { $0.scope == scope }
        let rail = SectionHosting.rail(scoped)
        let hosted = SectionHosting.hosted(scoped)
        if self.sections != rail { self.sections = rail }
        if hostedSections != hosted { hostedSections = hosted }
        popupSections = scoped.filter { !Popups.slots([$0]).isEmpty }
        let popups = Popups.slots(popupSections)
        if popupSlots != popups { popupSlots = popups }
        cacheLayout()
    }

    private func refreshSections(_ generation: Int) async {
        guard generation == self.generation, let sections = try? await client.uiSections(), generation == self.generation else { return }
        adopt(sections)
        guard connected, !self.sections.contains(where: { $0.id == selectedSection }) else { return }
        selectedSection = order(self.sections).first?.id
    }

    public func reloadSections() async {
        await refreshSections(generation)
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

    public func launchKind(_ name: String) -> PaneKind? {
        let cell = cell(name)
        guard !cell.present, !cell.seen, !launchSettled else { return nil }
        return launchRecord?.kinds[name]
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
        textFieldID(name) != nil
    }

    public func textFieldID(_ name: String) -> String? {
        let cell = cell(name)
        cell.track(.textField)
        return cell.textFieldID
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
            let generation = generation
            Task { [weak self] in await self?.heal(generation) }
        }
    }

    private static let firstRetry: Duration = .seconds(1)
    private static let longestRetry: Duration = .seconds(30)

    public func stop() async {
        epoch += 1
        await settle()
    }

    private func settle() async {
        let subscription = self.subscription
        halt()
        if let subscription { await discard(subscription) }
        await saving?.value
    }

    public var sidebarSlot: String? {
        guard let selectedSection, selectedSection != CodeSection.id, !isSidebarHidden(selectedSection) else { return nil }
        return "\(selectedSection).sidebar"
    }

    public var togglesSidebar: Bool {
        guard let selectedSection, selectedSection != CodeSection.id else { return false }
        return hasSlot("\(selectedSection).sidebar")
    }

    public var togglesChat: Bool {
        selectedSection.map { hasSlot(ChatColumn.slot(for: $0)) } ?? false
    }

    public var columnShown: Bool {
        guard let selectedSection else { return false }
        return togglesChat && !isChatHidden(selectedSection)
    }

    public var asideShown: Bool {
        selectedSection == AgentsSection.id && AgentsSection.showsAside(slot(AgentsSection.asideSlot)?.tree)
    }

    public var columnSlot: String? {
        columnShown ? selectedSection.map(ChatColumn.slot) : nil
    }

    public func toggleSidebar() {
        guard let selectedSection, selectedSection != CodeSection.id else { return }
        if selectedSection == ChatHint.section { dismissChatHint() }
        setSidebarHidden(selectedSection, !isSidebarHidden(selectedSection))
    }

    public func dismissChatHint() {
        chatHintShown = false
        chatHint.finish()
    }

    public func hideChat() {
        guard let selectedSection, togglesChat else { return }
        chatHintShown = false
        setChatHidden(selectedSection, true)
    }

    @discardableResult
    public func toggleChat() -> Task<Void, Never>? {
        guard let selectedSection, togglesChat else { return nil }
        guard isChatHidden(selectedSection) else {
            hideChat()
            return nil
        }
        setChatHidden(selectedSection, false)
        chatHintShown = selectedSection == ChatHint.section && chatHint.show()
        let event = UIEvent(workspaceId: workspaceID, slot: ChatColumn.slot(for: selectedSection), nodeId: "panel", action: "open", value: .object(["focus": .bool(true)]))
        return Task { [weak self] in _ = await self?.send(event) }
    }

    public func revealSidebar(_ section: String) {
        setSidebarHidden(section, false)
    }

    public func revealChat(_ section: String) {
        setChatHidden(section, false)
    }

    private func isSidebarHidden(_ section: String) -> Bool {
        sidebarHidden[section] ?? false
    }

    private func setSidebarHidden(_ section: String, _ hidden: Bool) {
        sidebarHidden[section] = hidden ? true : nil
    }

    private func isChatHidden(_ section: String) -> Bool {
        chatHidden[section] ?? true
    }

    private func setChatHidden(_ section: String, _ hidden: Bool) {
        chatHidden[section] = hidden ? nil : false
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
        guard let section = SectionHosting.section(ofSlot: slot, in: sections.map(\.id)) else { return }
        focusedSlots[section] = slot
    }

    private func refocus(_ section: String) {
        guard let slot = focusedSlots[section], refocusable(slot, in: section) else { return }
        focusSerial += 1
        focusRequest = FocusRequest(slot: slot, serial: focusSerial)
    }

    private func refocusable(_ slot: String, in section: String) -> Bool {
        if slot.hasPrefix(ChatColumn.slot(for: section)) { return !isChatHidden(section) }
        if slot.hasPrefix("\(section).sidebar") { return !isSidebarHidden(section) }
        return true
    }

    private func focus(_ params: UIFocusNotification) {
        guard params.subscription == subscription else { return }
        let slot = select(params.section, slot: params.slot)
        guard !slot.isEmpty else { return }
        focusSerial += 1
        focusRequest = FocusRequest(slot: slot, serial: focusSerial, field: params.field ?? false, node: params.node)
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
        epoch += 1
        let epoch = epoch
        await settle()
        var delay = Self.firstRetry
        while epoch == self.epoch, client === self.client, client.isAlive {
            do {
                try await start()
                return
            } catch {
                guard epoch == self.epoch else { return }
                failure = describe(error)
            }
            try? await Task.sleep(for: delay)
            delay = min(delay * 2, Self.longestRetry)
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
        guard connected, let launchCache, PaneKind.caches(name) else { return }
        let kind = PaneKind.of(view.tree)
        if launchCache.record(workspaceID)?.kinds[name] != kind { launchCache.update(workspaceID) { $0.kinds[name] = kind } }
    }

    private func cacheLayout() {
        guard let launchCache, connected, !restoring else { return }
        let kinds = Dictionary(uniqueKeysWithValues: cells.compactMap { name, cell in
            cell.view.flatMap { PaneKind.caches(name) ? (name, PaneKind.of($0.tree)) : nil }
        })
        launchCache.update(workspaceID) { record in
            record.sections = sections.map { LaunchSection(id: $0.id, title: $0.title, icon: $0.icon) }
            record.selected = selectedSection
            record.kinds = kinds
            record.sidebarHidden = sidebarHidden
            record.chatHidden = chatHidden
            record.sidebarWidth = sidebarWidth
            record.chatWidth = chatWidth
        }
    }

    private func restore(from state: JSONValue?) {
        let saved = state?.objectValue ?? [:]
        restoring = true
        let ordered = order(sections)
        selectedSection = (ordered.first(where: \.opensFirst) ?? ordered.first)?.id
        if let section = requestedSection ?? saved["section"]?.stringValue { select(section) }
        sidebarWidth = saved["sidebarWidth"]?.numberValue
        sidebarHidden = saved["sidebarHidden"]?.objectValue?.compactMapValues(\.boolValue) ?? [:]
        chatHidden = saved["chatHidden"]?.objectValue?.compactMapValues(\.boolValue) ?? [:]
        chatWidth = saved["chatWidth"]?.numberValue
        asideWidth = saved["asideWidth"]?.numberValue
        let migrated = saved["chatHidden"] == nil ? sidebarHidden[CodeSection.id] : nil
        restoring = false
        if let migrated {
            sidebarHidden[CodeSection.id] = nil
            chatHidden[CodeSection.id] = migrated
        }
        adoptLegacyChat()
        if let selectedSection, saved["section"]?.stringValue != selectedSection { updateState("section", .string(selectedSection)) }
    }

    private func adoptLegacyChat() {
        guard let legacy = legacyChat else { return }
        legacyChat = nil
        if legacy.shown, chatHidden[CodeSection.id] == nil { revealChat(CodeSection.id) }
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
    private(set) var textFieldID: String?
    private(set) var hasComposer = false
    private(set) var tabs: SDUINode?
    @ObservationIgnored private(set) var seen = false
    @ObservationIgnored private var tracked: Set<SlotFact> = []
    @ObservationIgnored private var tree: SDUINode?

    func set(_ view: SlotView?) {
        guard self.view != view else { return }
        self.view = view
        tree = view?.tree
        if view != nil { seen = true }
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
            let textFieldID = tree?.firstTextFieldID
            if self.textFieldID != textFieldID { self.textFieldID = textFieldID }
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
    public let node: String?

    public init(slot: String, serial: Int, field: Bool = false, node: String? = nil) {
        self.slot = slot
        self.serial = serial
        self.field = field
        self.node = node
    }

    public static func matches(_ node: String?, target: String?) -> Bool {
        guard let target else { return true }
        guard let node else { return false }
        return node == target || node.hasPrefix(target + "/")
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
    let node: String?
}

private struct UINotification: Decodable, Sendable {
    let subscription: String
    let view: SlotUpdate?
}
