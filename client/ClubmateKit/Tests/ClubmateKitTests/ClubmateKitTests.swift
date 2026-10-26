import Foundation
import XCTest

import ClubmateKit

@MainActor
final class ClubmateKitTests: XCTestCase {
    private var core: TestCore?

    override func tearDown() async throws {
        await core?.stop()
        core = nil
    }

    func testPingAndProjects() async throws {
        let client = try await startCore()
        let ping = try await client.ping()
        XCTAssertGreaterThan(ping.pid, 0)
        _ = try await client.createProject(name: "demo", repos: [])
        let projects = try await client.listProjects()
        XCTAssertEqual(projects.map(\.name), ["demo"])
        await client.close()
    }

    func testNotificationsDelivered() async throws {
        let client = try await startCore()
        _ = try await client.busSubscribe(pattern: "demo.*", after: 0)
        try core?.cli("bus", "publish", "demo.x", "{}")
        let notification = await first(client.notifications, within: .seconds(3))
        XCTAssertEqual(notification?.method, "bus.event")
        await client.close()
    }

    func testRefusesInsecureRuntime() async throws {
        let base = try TestCore.makeTempDir()
        defer { try? FileManager.default.removeItem(at: base) }
        let paths = ClubmatePaths(state: base.appending(path: "state"), runtime: base.appending(path: "run"))
        try FileManager.default.createDirectory(at: paths.runtime.appending(path: "s"), withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: paths.runtime.path(percentEncoded: false))
        do {
            _ = try await RPCConnection.connect(paths: paths)
            XCTFail("connected to an insecure runtime dir")
        } catch {
            XCTAssertEqual(error as? ClubmateError, .insecureRuntime(paths.runtime.path(percentEncoded: false)))
        }
    }

    func testLauncherStartsCore() async throws {
        let core = try TestCore.prepare()
        self.core = core
        try await CoreLauncher(cliPath: core.bin.appending(path: "clubmate")).ensureRunning(paths: core.paths)
        let client = try await CoreClient.connect(paths: core.paths)
        let ping = try await client.ping()
        XCTAssertGreaterThan(ping.pid, 0)
        await client.close()
    }

    func testRPCErrorMapping() async throws {
        let client = try await startCore()
        do {
            _ = try await client.uiStateGet(projectID: "prj_0000000000000000")
            XCTFail("expected not found")
        } catch let ClubmateError.rpc(code, _) {
            XCTAssertEqual(code, -32004)
        }
        await client.close()
    }

    private func startCore() async throws -> CoreClient {
        let core = try await TestCore.start()
        self.core = core
        return try await CoreClient.connect(paths: core.paths)
    }

    private func first(_ stream: AsyncStream<RPCNotification>, within timeout: Duration) async -> RPCNotification? {
        await withTaskGroup(of: RPCNotification?.self) { group in
            group.addTask {
                for await notification in stream { return notification }
                return nil
            }
            group.addTask {
                try? await Task.sleep(for: timeout)
                return nil
            }
            let result = await group.next() ?? nil
            group.cancelAll()
            return result
        }
    }
}
