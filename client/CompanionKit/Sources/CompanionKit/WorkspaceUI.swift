import Foundation
import Observation

@MainActor
@Observable
public final class WorkspaceUI {
    public let workspace: String
    public private(set) var sections: [SectionInfo] = []
    public private(set) var failure: String?
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

    static let firstRetry: Duration = .seconds(1)
    static let longestRetry: Duration = .seconds(30)

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

    public func send(slot: String, nodeId: String, action: String, value: JSONValue?) async throws -> JSONValue {
        guard let connection else { throw CancellationError() }
        return try await connection.uiEvent(workspace: workspace, slot: slot, nodeId: nodeId, action: action, value: value)
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
