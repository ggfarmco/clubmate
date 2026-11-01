import Foundation
import Observation

@MainActor
@Observable
public final class WorkspaceUI {
    public static let popups = "*." + Popups.name

    public let workspace: String
    public private(set) var sections: [SectionInfo] = []
    public private(set) var failure: String?
    private var views: [String: SlotView] = [:]
    private var popupOrder: [String] = []
    private var dismissed: Set<String> = []

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

    public var popup: String? {
        Popups.current(views.filter { !dismissed.contains($0.key) }, order: popupOrder)
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

    public func dismissPopup(_ slot: String) async throws {
        dismissed.insert(slot)
        do {
            _ = try await send(slot: slot, nodeId: "", action: "close", value: nil)
        } catch {
            dismissed.remove(slot)
            throw error
        }
    }

    func reload() {
        stale = Set(views.keys)
        sync()
    }

    func receive(_ view: SlotView) {
        guard stale.remove(view.slot) != nil || view.rev > views[view.slot]?.rev ?? 0 else { return }
        store(view)
    }

    private var desired: Set<String> {
        owners.isEmpty ? [] : owners.values.reduce(into: [Self.popups]) { $0.formUnion($1) }
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
            if sentStream == stream, sent == target { return }
            do {
                if sectionsStream != stream {
                    sections = try await connection.uiSections()
                    sectionsStream = stream
                }
                let previous = sentStream == stream ? sent : []
                let slots = target.subtracting([Self.popups])
                try await connection.uiWatch(stream: stream, workspace: workspace, slots: target.isEmpty ? [] : slots.sorted() + [Self.popups])
                var fetch = slots.subtracting(previous)
                if !previous.contains(Self.popups), target.contains(Self.popups) { fetch.formUnion(Popups.slots(sections)) }
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

    private func store(_ view: SlotView) {
        if view.slot.hasSuffix("." + Popups.name), Popups.isOpen(view), views[view.slot].map(Popups.isOpen) != true || dismissed.contains(view.slot) {
            popupOrder.removeAll { $0 == view.slot }
            popupOrder.append(view.slot)
        }
        dismissed.remove(view.slot)
        views[view.slot] = view
    }
}
