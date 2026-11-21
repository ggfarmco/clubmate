import Foundation
import Observation

public struct PageTree: Equatable {
    public let tree: SDUINode
    public let updating: Bool
}

@MainActor
@Observable
public final class WorkspaceUI {
    public let workspace: String
    public private(set) var sections: [SectionInfo] = []
    public private(set) var sectionsLoaded = false
    public private(set) var failure: String?
    public private(set) var updating: Set<String> = []
    private var views: [String: SlotView] = [:]

    @ObservationIgnored private weak var connection: ComputerConnection?
    @ObservationIgnored private var owners: [AnyHashable: Set<String>] = [:]
    @ObservationIgnored private var sent: Set<String> = []
    @ObservationIgnored private var sentStream: String?
    @ObservationIgnored private var sectionsStream: String?
    @ObservationIgnored private var stale: Set<String> = []
    @ObservationIgnored private var syncing: Task<Void, Never>?
    @ObservationIgnored private var waiting: Task<Void, Never>?
    @ObservationIgnored private var backoff = WorkspaceUI.firstRetry
    @ObservationIgnored private var kept: [(path: String, tree: SDUINode)] = []
    @ObservationIgnored private var selecting: [String: String] = [:]

    static let firstRetry: Duration = .seconds(1)
    static let longestRetry: Duration = .seconds(30)
    public static let keptPages = 20

    public init(connection: ComputerConnection, workspace: String) {
        self.connection = connection
        self.workspace = workspace
    }

    public func view(_ slot: String) -> SlotView? {
        views[slot]
    }

    public func show(_ slots: Set<String>, owner: AnyHashable) {
        owners[owner] = slots
        sync()
    }

    public func hide(owner: AnyHashable) {
        guard owners.removeValue(forKey: owner) != nil else { return }
        sync()
    }

    public func page(_ id: String, path: String, in slot: String) -> PageTree? {
        let live = views[slot]?.tree
        if let live, PhoneSections.document(in: live) != nil, PhoneSections.shows(live, page: id, path: path) { return PageTree(tree: live, updating: false) }
        if let tree = kept.last(where: { PhoneSections.shows($0.tree, page: id, path: path) })?.tree { return PageTree(tree: tree, updating: updating.contains(id)) }
        return live.flatMap { PhoneSections.shows($0, page: id, path: path) ? PageTree(tree: $0, updating: false) : nil }
    }

    public func select(_ item: String, in slot: String) async {
        guard let list = views[slot].flatMap({ PhoneSections.list(in: $0.tree) }), let id = list.id, list.string("selected") != item, selecting[slot] != item else { return }
        let mark = selecting(item, in: slot)
        await Task { [connection, workspace] in
            _ = try? await connection?.uiEvent(workspace: workspace, slot: slot, nodeId: id, action: "select", value: .string(item))
        }.value
        selected(item, in: slot, mark: mark)
    }

    public func send(slot: String, nodeId: String, action: String, value: JSONValue?) async throws -> JSONValue {
        guard let connection else { throw CancellationError() }
        guard action == "select", let item = value?.stringValue else { return try await connection.uiEvent(workspace: workspace, slot: slot, nodeId: nodeId, action: action, value: value) }
        let mark = selecting(item, in: slot)
        defer { selected(item, in: slot, mark: mark) }
        return try await connection.uiEvent(workspace: workspace, slot: slot, nodeId: nodeId, action: action, value: value)
    }

    private func selecting(_ item: String, in slot: String) -> Task<Void, Never> {
        selecting[slot] = item
        return Task { [weak self, sleep = connection?.sleep] in
            guard let sleep, (try? await sleep(AgentsStore.refreshMark)) != nil, !Task.isCancelled else { return }
            self?.updating.insert(item)
        }
    }

    private func selected(_ item: String, in slot: String, mark: Task<Void, Never>) {
        mark.cancel()
        if selecting[slot] == item { selecting[slot] = nil }
        if updating.contains(item) { updating.remove(item) }
    }

    private func keep(_ tree: SDUINode) {
        guard let path = PhoneSections.document(in: tree)?.string("path"), !path.contains("@") else { return }
        kept.removeAll { $0.path == path }
        kept.append((path, tree))
        if kept.count > Self.keptPages { kept.removeFirst(kept.count - Self.keptPages) }
    }

    func reloadSections() {
        sectionsStream = nil
        sync()
    }

    func reload() {
        stale = Set(views.keys)
        sync()
    }

    func receive(_ view: SlotView) {
        guard stale.remove(view.slot) != nil || view.rev > views[view.slot]?.rev ?? 0 else { return }
        views[view.slot] = view
        keep(view.tree)
    }

    private var desired: Set<String> {
        owners.values.reduce(into: []) { $0.formUnion($1) }
    }

    private func sync() {
        waiting?.cancel()
        waiting = nil
        guard syncing == nil else { return }
        syncing = Task { [weak self] in
            let failed = await self?.drain()
            self?.syncing = nil
            if let failed { self?.retry(failed) }
        }
    }

    private func retry(_ stream: String) {
        guard let sleep = connection?.sleep else { return }
        let delay = backoff
        backoff = min(backoff * 2, Self.longestRetry)
        waiting = Task { [weak self] in
            guard (try? await sleep(delay)) != nil, !Task.isCancelled, let self, connection?.streamID == stream else { return }
            waiting = nil
            sync()
        }
    }

    private func drain() async -> String? {
        while let connection, connection.supportsUI, let stream = connection.streamID {
            let target = desired
            if sentStream == stream, sent == target, sectionsStream == stream { return nil }
            do {
                if sectionsStream != stream {
                    sectionsStream = stream
                    do {
                        sections = try await connection.uiSections()
                        sectionsLoaded = true
                    } catch {
                        sectionsStream = nil
                        throw error
                    }
                }
                let previous = sentStream == stream ? sent : []
                try await connection.uiWatch(stream: stream, workspace: workspace, slots: target.sorted())
                let fetch = target.subtracting(previous)
                if !fetch.isEmpty {
                    let found = try await connection.uiGet(workspace: workspace, slots: fetch.sorted()).filter { $0.workspaceId == workspace }
                    for view in found {
                        receive(view)
                    }
                    for slot in fetch.subtracting(found.map(\.slot)) {
                        views[slot] = nil
                        stale.remove(slot)
                    }
                }
                sent = target
                sentStream = stream
                failure = nil
                backoff = Self.firstRetry
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
                return stream
            }
        }
        return nil
    }
}
