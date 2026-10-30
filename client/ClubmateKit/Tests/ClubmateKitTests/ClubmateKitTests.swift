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
        XCTAssertNotNil(ping.sessionProtocol)
        let project = try await client.createProject(name: "demo", repos: [])
        let projects = try await client.listProjects()
        XCTAssertEqual(projects.map(\.name), ["demo"])
        try core?.cli("session", "create", "--project", project.id, "--", "/bin/sleep", "60")
        let live = try await client.liveSessionCount()
        XCTAssertEqual(live, 1)
        await client.close()
    }

    func testPluginList() async throws {
        let client = try await startCore()
        let plugins = try await client.pluginList()
        XCTAssertEqual(plugins.map(\.id), ["echo", "terminals"])
        let terminals = try XCTUnwrap(plugins.last)
        XCTAssertEqual(terminals.name, "Terminals")
        XCTAssertEqual(terminals.manifest.contributes.sections.map(\.id), ["terminals"])
        XCTAssertFalse(terminals.manifest.contributes.commands.isEmpty)
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

    func testConfirmRequestDecodes() throws {
        let fixture = Data(#"{"title":"Close Terminal","workspace":"demo","command":"terminals.close","args":{"sessionId":"ses_1","force":true,"line":3}}"#.utf8)
        let request = try JSONDecoder().decode(ConfirmRequest.self, from: fixture)
        XCTAssertEqual(request, ConfirmRequest(title: "Close Terminal", workspace: "demo", command: "terminals.close", args: .object(["sessionId": .string("ses_1"), "force": .bool(true), "line": .number(3)])))
        XCTAssertEqual(request.message, "Allow agent to close Terminal?")
        XCTAssertEqual(request.detail, "Workspace: demo\nCommand: terminals.close\nforce: true\nline: 3\nsessionId: ses_1")
    }

    func testConfirmDetailEscapes() {
        let long = String(repeating: "x", count: 300)
        let request = ConfirmRequest(title: "Kill Session", workspace: "demo", command: "session.kill", args: .object(["id\nCommand: fake": .string("ses_1\u{2028}Approved\u{202E}: yes"), "note": .string(long)]), session: ConfirmSession(id: "ses_1", title: "Build", workspace: "demo"))
        XCTAssertEqual(request.detail, "Workspace: demo\nCommand: session.kill\nSession: Build (ses_1) in demo\nid\\nCommand: fake: ses_1\\u{2028}Approved\\u{202e}: yes\nnote: \(long)")
    }

    func testConfirmTooLongIsDeniedUnasked() async {
        let request = ConfirmRequest(title: "Kill Session", workspace: "demo", command: "session.kill", args: .object(["id": .string(String(repeating: "x", count: 201))]))
        var asked = false
        let reply = await ConfirmRequest.answer(request) { _, _ in
            asked = true
            return true
        }
        XCTAssertEqual(reply, ConfirmReply(approved: false, reason: "args too long to display"))
        XCTAssertFalse(asked)
    }

    func testConfirmExpiresBeforeCoreTimeout() async {
        let request = ConfirmRequest(title: "Kill Session", workspace: "demo", command: "session.kill", args: .null, timeoutMs: 5001)
        XCTAssertEqual(request.deadline, .milliseconds(1))
        let reply = await ConfirmRequest.answer(request) { _, expiry in
            await withCheckedContinuation { continuation in
                expiry.onExpire { continuation.resume(returning: true) }
            }
        }
        XCTAssertEqual(reply, ConfirmReply(approved: false, reason: "approval timed out"))
    }

    func testAgentApprovalRoundTrip() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        try core?.cli("session", "create", "--project", project.id, "--", "/bin/sleep", "60")
        let sessions = try await client.call(method: "session.list")
        guard let id = sessions.arrayValue?.first?.objectValue?["id"]?.stringValue else { throw TestFailure(description: "no session") }
        try await client.register(handles: ["ui.confirm"])
        let approvals = Approvals()
        client.confirmHandler = { request, _ in
            approvals.asked.append(request)
            return approvals.answer
        }
        let kill = JSONValue.object(["id": .string("session.kill"), "agent": .bool(true), "args": .object(["id": .string(id)])])
        do {
            _ = try await client.call(method: "command.run", params: kill)
            XCTFail("kill ran without approval")
        } catch {
            XCTAssertEqual(error as? ClubmateError, .rpc(code: -32014, message: "denied by user"))
        }
        approvals.answer = true
        _ = try await client.call(method: "command.run", params: kill)
        XCTAssertEqual(approvals.asked, Array(repeating: ConfirmRequest(title: "Kill Session", workspace: "demo", command: "session.kill", args: .object(["id": .string(id)]), timeoutMs: 60000, session: ConfirmSession(id: id, title: "", workspace: "demo")), count: 2))
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

    func testLauncherReplacesOutdatedCore() async throws {
        let core = try await TestCore.start(bin: try TestCore.binaries(version: "old"))
        self.core = core
        let old = try await CoreClient.connect(paths: core.paths)
        let oldPing = try await old.ping()
        XCTAssertEqual(oldPing.version, BuildInfo.display(version: "old", commit: TestCore.commit))
        let project = try await old.createProject(name: "demo")
        let created = try await old.call(method: "session.create", params: .object([
            "projectId": .string(project.id),
            "command": .array([.string("sleep"), .string("60")]),
        ]))
        await old.close()
        guard case .object(let session) = created, let id = session["id"], case .number(let hostPID)? = session["hostPid"] else {
            throw TestFailure(description: "unexpected session: \(created)")
        }
        let bundled = BuildInfo.display(version: "new", commit: TestCore.commit)
        let cli = try TestCore.binaries(version: "new").appending(path: "clubmate")
        try await CoreLauncher(cliPath: cli, bundledVersion: bundled).ensureRunning(paths: core.paths)
        let client = try await CoreClient.connect(paths: core.paths)
        let ping = try await client.ping()
        XCTAssertEqual(ping.version, bundled)
        XCTAssertNotEqual(ping.pid, oldPing.pid)
        guard case .array(let sessions) = try await client.call(method: "session.list") else {
            throw TestFailure(description: "unexpected session list")
        }
        XCTAssertTrue(sessions.contains { if case .object(let s) = $0 { s["id"] == id } else { false } })
        XCTAssertEqual(kill(pid_t(hostPID), 0), 0)
        try await CoreLauncher(cliPath: cli, bundledVersion: bundled).ensureRunning(paths: core.paths)
        let again = try await client.ping()
        XCTAssertEqual(again.pid, ping.pid)
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

@MainActor
private final class Approvals {
    var asked: [ConfirmRequest] = []
    var answer = false
}
