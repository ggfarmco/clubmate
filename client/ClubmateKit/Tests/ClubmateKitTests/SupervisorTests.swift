import Darwin
import Foundation
import XCTest

import ClubmateKit

@MainActor
final class SupervisorTests: XCTestCase {
    private var core: TestCore?

    override func tearDown() async throws {
        await core?.stop()
        core = nil
    }

    func testSupervisorBacksOffThenFails() async throws {
        let delays = Recorder<Duration>()
        let supervisor = ConnectionSupervisor(connect: { throw ClubmateError.notRunning }, sleep: { await delays.add($0) })
        supervisor.start()
        try await waitUntil { supervisor.phase == .failed("Core unreachable") }
        XCTAssertEqual(delays.values, [0.5, 1, 2, 4, 5, 5, 5, 5, 5, 5].map { Duration.seconds($0) })
    }

    func testSupervisorFailsFastOnPermanentError() async throws {
        let delays = Recorder<Duration>()
        let connects = Recorder<Int>()
        let error = ClubmateError.insecureRuntime("/run")
        let supervisor = ConnectionSupervisor(connect: {
            connects.add(1)
            throw error
        }, sleep: { await delays.add($0) })
        supervisor.start()
        try await waitUntil { supervisor.phase == .failed(error.message) }
        XCTAssertEqual(connects.values.count, 1)
        XCTAssertEqual(delays.values, [])
    }

    func testSupervisorStopsOnIntentionalShutdown() async throws {
        let core = try await TestCore.start()
        self.core = core
        let connects = Recorder<Int>()
        let supervisor = ConnectionSupervisor(connect: {
            connects.add(1)
            return try await CoreClient.connect(paths: core.paths)
        })
        supervisor.start()
        try await waitUntil { supervisor.phase == .ready }
        _ = try await supervisor.client?.call(method: "core.shutdown")
        try await waitUntil { supervisor.phase == .failed("Core stopped") }
        XCTAssertEqual(connects.values.count, 1)
    }

    func testWorkspaceOpenRegistered() async throws {
        let core = try await TestCore.start()
        self.core = core
        let supervisor = ConnectionSupervisor(connect: { try await CoreClient.connect(paths: core.paths) })
        supervisor.start()
        let client = try await waitFor { supervisor.phase == .ready ? supervisor.client : nil }
        let workspace = try await client.createWorkspace(name: "demo")
        let caller = try await CoreClient.connect(paths: core.paths)
        let result = try await caller.call(method: "workspace.show", params: .object(["id": .string(workspace.id)]))
        XCTAssertEqual(result.objectValue?["clients"]?.numberValue, 1)
        await caller.close()
    }

    func testSupervisorRebindsRegistryStoresAfterCoreRestart() async throws {
        let core = try await TestCore.start()
        self.core = core
        let registry = Recorder<WorkspaceUIStore>()
        let supervisor = ConnectionSupervisor(connect: { try await CoreClient.connect(paths: core.paths) }, stores: { registry.values })
        supervisor.start()
        let client = try await waitFor { supervisor.phase == .ready ? supervisor.client : nil }
        let workspace = try await client.createWorkspace(name: "demo")
        let kept = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        let dropped = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        registry.add(kept)
        registry.add(dropped)
        supervisor.attach(kept)
        supervisor.attach(dropped)
        try await waitUntil { kept.connected && dropped.connected && kept.slot("terminals.sidebar") != nil }
        registry.remove { $0 === dropped }

        try core.crash()
        try await waitUntil { !kept.connected && !dropped.connected }
        try await core.launch()
        try await waitUntil(within: .seconds(15)) { kept.connected && supervisor.client !== client }
        XCTAssertFalse(dropped.connected)
        let button = try await waitFor { kept.slot("terminals.sidebar")?.tree.find(id: "new") }
        _ = try await kept.send(UIAction.event(for: button, trigger: .click, slot: "terminals.sidebar", workspaceID: workspace.id)).get()
        try await waitUntil { kept.slot("terminals.main")?.tree.type == "surface" }
        await kept.stop()
        await dropped.stop()
    }
}

@MainActor
final class Recorder<Value: Sendable> {
    private(set) var values: [Value] = []

    func add(_ value: Value) {
        values.append(value)
    }

    func remove(where match: (Value) -> Bool) {
        values.removeAll(where: match)
    }
}
