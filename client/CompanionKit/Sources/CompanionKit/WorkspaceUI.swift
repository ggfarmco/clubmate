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
    public private(set) var missing: Set<String> = []
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
    @ObservationIgnored private var kept: [(slot: String, page: String?, path: String, tree: SDUINode)] = []
    @ObservationIgnored private var selecting: [String: (item: String, run: Task<Result<JSONValue, any Error>, Never>)] = [:]
    @ObservationIgnored private var starts: [String: Int] = [:]
    @ObservationIgnored private var settling: [String: Settling] = [:]
    @ObservationIgnored private var looping: [String: Int] = [:]
    @ObservationIgnored private var naps: [UUID: Task<Void, Never>] = [:]

    private struct Settling {
        let item: String
        let mark: Task<Void, Never>
        var grace: Task<Void, Never>?
        var stream: String?
    }

    static let firstRetry: Duration = .seconds(1)
    static let longestRetry: Duration = .seconds(30)
    public static let keptPages = 20
    static let settleGrace: Duration = .seconds(2)

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
        if let live, PhoneSections.document(in: live) != nil, path.isEmpty ? owner(of: live, in: slot) == id : PhoneSections.shows(live, page: id, path: path) { return PageTree(tree: live, updating: false) }
        if let tree = kept.last(where: { $0.slot == slot && (path.isEmpty ? $0.page == id : $0.path == path) })?.tree { return PageTree(tree: tree, updating: updating.contains(id)) }
        return live.flatMap { PhoneSections.document(in: $0) == nil ? PageTree(tree: $0, updating: false) : nil }
    }

    public func select(_ item: String, in slot: String) async {
        looping[item, default: 0] += 1
        defer {
            if let count = looping[item], count > 1 { looping[item] = count - 1 } else { looping[item] = nil }
            release(item, in: slot)
        }
        var wait = Self.firstRetry
        while !Task.isCancelled {
            guard let list = views[slot].flatMap({ PhoneSections.list(in: $0.tree) }), let id = list.id, list.string("selected") != item else { return }
            if let running = selecting[slot], running.item == item {
                if case .success = await running.run.value { return }
                continue
            }
            if case .success = await start(item, in: slot, nodeId: id, value: .string(item)).value { return }
            let stream = connection?.streamID
            guard await nap(wait) else { return }
            wait = connection?.streamID == stream ? min(wait * 2, Self.longestRetry) : Self.firstRetry
        }
    }

    private func nap(_ wait: Duration) async -> Bool {
        guard let sleep = connection?.sleep else { return false }
        let id = UUID()
        let task = Task { _ = try? await sleep(wait) }
        naps[id] = task
        await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
        naps[id] = nil
        return !Task.isCancelled
    }

    public func send(slot: String, nodeId: String, action: String, value: JSONValue?) async throws -> JSONValue {
        guard let connection else { throw CancellationError() }
        guard action == "select", let item = value?.stringValue else { return try await connection.uiEvent(workspace: workspace, slot: slot, nodeId: nodeId, action: action, value: value) }
        return try await start(item, in: slot, nodeId: nodeId, value: value).value.get()
    }

    private func start(_ item: String, in slot: String, nodeId: String, value: JSONValue?) -> Task<Result<JSONValue, any Error>, Never> {
        if let pending = settling.removeValue(forKey: slot) { end(pending) }
        starts[slot, default: 0] += 1
        let start = starts[slot] ?? 0
        let mark = Task { [weak self, sleep = connection?.sleep] in
            guard let sleep, (try? await sleep(AgentsStore.refreshMark)) != nil, !Task.isCancelled else { return }
            self?.updating.insert(item)
        }
        let run = Task { [weak self, connection, workspace] () -> Result<JSONValue, any Error> in
            let result: Result<JSONValue, any Error>
            do {
                guard let connection else { throw CancellationError() }
                result = .success(try await connection.uiEvent(workspace: workspace, slot: slot, nodeId: nodeId, action: "select", value: value))
            } catch {
                result = .failure(error)
            }
            self?.replied(item, in: slot, start: start, mark: mark, ok: (try? result.get()) != nil)
            return result
        }
        selecting[slot] = (item, run)
        return run
    }

    private func replied(_ item: String, in slot: String, start: Int, mark: Task<Void, Never>, ok: Bool) {
        guard starts[slot] == start else {
            mark.cancel()
            return release(item, in: slot)
        }
        selecting[slot] = nil
        guard ok else {
            mark.cancel()
            return release(item, in: slot)
        }
        let stream = synced
        settling[slot] = Settling(item: item, mark: mark, grace: stream.map { grace(slot, on: $0) }, stream: stream)
        settle(slot, force: false)
    }

    private var synced: String? {
        guard let stream = connection?.streamID, stream == sentStream else { return nil }
        return stream
    }

    private func grace(_ slot: String, on stream: String) -> Task<Void, Never> {
        Task { [weak self, sleep = connection?.sleep] in
            guard let sleep, (try? await sleep(Self.settleGrace)) != nil, !Task.isCancelled, let self, synced == stream else { return }
            settle(slot, force: true)
        }
    }

    private func arm(_ stream: String) {
        for (slot, pending) in settling where pending.stream != stream {
            guard sent.contains(slot) else {
                settling[slot] = nil
                end(pending)
                continue
            }
            pending.grace?.cancel()
            settling[slot]?.grace = grace(slot, on: stream)
            settling[slot]?.stream = stream
        }
    }

    private func release(_ item: String, in slot: String) {
        guard selecting[slot]?.item != item, settling[slot]?.item != item, looping[item] == nil, updating.contains(item) else { return }
        updating.remove(item)
    }

    private func settle(_ slot: String, force: Bool) {
        guard let pending = settling[slot] else { return }
        let selected = PhoneSections.selected(in: views[slot])
        guard force || selected == pending.item else { return }
        settling[slot] = nil
        end(pending)
        guard selected != pending.item, let main = PhoneSections.main(of: slot) else { return }
        missing.insert(pending.item)
        kept.removeAll { $0.slot == main && $0.page == pending.item }
    }

    private func end(_ pending: Settling) {
        pending.mark.cancel()
        pending.grace?.cancel()
        if updating.contains(pending.item) { updating.remove(pending.item) }
    }

    private func owner(of tree: SDUINode, in slot: String) -> String? {
        guard let sidebar = PhoneSections.sidebar(of: slot), let selected = PhoneSections.selected(in: views[sidebar]), PhoneSections.shows(tree, page: selected, path: "") else { return nil }
        return selected
    }

    private func keep(_ view: SlotView) {
        guard let path = PhoneSections.document(in: view.tree)?.string("path"), !path.contains("@") else { return }
        let page = owner(of: view.tree, in: view.slot)
        kept.removeAll { $0.slot == view.slot && ($0.path == path || (page != nil && $0.page == page)) }
        kept.append((view.slot, page, path, view.tree))
        if kept.count > Self.keptPages { kept.removeFirst(kept.count - Self.keptPages) }
    }

    func reloadSections() {
        sectionsStream = nil
        sync()
    }

    func reload() {
        stale = Set(views.keys)
        for nap in naps.values { nap.cancel() }
        sync()
    }

    func receive(_ view: SlotView) {
        guard stale.remove(view.slot) != nil || view.rev > views[view.slot]?.rev ?? 0 else { return }
        views[view.slot] = view
        if let selected = PhoneSections.selected(in: view), missing.contains(selected) { missing.remove(selected) }
        keep(view)
        settle(view.slot, force: false)
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
                    for view in found.sorted(by: { PhoneSections.main(of: $0.slot) != nil && PhoneSections.main(of: $1.slot) == nil }) {
                        receive(view)
                    }
                    for slot in fetch.subtracting(found.map(\.slot)) {
                        views[slot] = nil
                        stale.remove(slot)
                    }
                }
                sent = target
                sentStream = stream
                arm(stream)
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
