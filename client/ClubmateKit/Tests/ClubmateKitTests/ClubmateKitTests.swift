import Darwin
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
        let streams = [client.subscribeNotifications(), client.subscribeNotifications()]
        _ = try await client.busSubscribe(pattern: "demo.*", after: 0)
        try core?.cli("bus", "publish", "demo.x", "{}")
        for stream in streams {
            let notification = await first(stream, within: .seconds(3))
            XCTAssertEqual(notification?.method, "bus.event")
        }
        await client.close()
    }

    func testCoreStopIsSurfaced() async throws {
        let client = try await startCore()
        let stream = client.subscribeNotifications()
        try core?.cli("core", "stop")
        let notification = await first(stream, within: .seconds(3))
        XCTAssertEqual(notification?.method, "core.stopping")
        XCTAssertTrue(client.stoppedIntentionally)
        await client.close()
    }

    func testProjectOpenRequests() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        try await client.register(handles: ["project.open"])
        let requests = client.projectOpenRequests()
        guard let paths = core?.paths else { throw TestFailure(description: "no core") }
        let caller = try await CoreClient.connect(paths: paths)
        _ = try await caller.call(method: "project.open", params: .object(["id": .string(project.id)]))
        let id = await first(requests, within: .seconds(3))
        XCTAssertEqual(id, project.id)
        await caller.close()
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

    func testCallDeadline() async throws {
        let base = try TestCore.makeTempDir()
        defer { try? FileManager.default.removeItem(at: base) }
        let paths = ClubmatePaths(state: base.appending(path: "state"), runtime: base.appending(path: "run"))
        for dir in [paths.runtime, paths.sessionsSocketDir] {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        }
        let server = try listen(at: paths.coreSocket.path(percentEncoded: false))
        defer { close(server) }
        let connection = try await RPCConnection.connect(paths: paths)
        do {
            let _: JSONValue = try await connection.call("core.ping", JSONValue.object([:]), timeout: .milliseconds(200))
            XCTFail("call without a reply returned")
        } catch {
            XCTAssertEqual(error as? ClubmateError, .timeout)
        }
        await connection.close()
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

    private func listen(at path: String) throws -> Int32 {
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        withUnsafeMutableBytes(of: &addr.sun_path) { $0.copyBytes(from: Array(path.utf8)) }
        let bound = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.bind(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard fd >= 0, bound == 0, Darwin.listen(fd, 1) == 0 else {
            throw TestFailure(description: "listen \(path): \(String(cString: strerror(errno)))")
        }
        return fd
    }
}
