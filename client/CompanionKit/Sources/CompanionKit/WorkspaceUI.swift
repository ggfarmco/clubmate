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
        guard syncing == nil else { return }
        syncing = Task { [weak self] in
            await self?.drain()
            self?.syncing = nil
        }
    }

    private func drain() async {
        while let connection, connection.supportsUI, let stream = connection.streamID {
            let target = desired
            if sentStream == stream, sent == target, sectionsStream == stream { return }
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
                    let found = try await connection.uiGet(workspace: workspace, slots: fetch.sorted())
                    for view in found where view.workspaceId == workspace {
                        receive(view)
                    }
                }
                sent = target
                sentStream = stream
                failure = nil
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
                return
            }
        }
    }
}
