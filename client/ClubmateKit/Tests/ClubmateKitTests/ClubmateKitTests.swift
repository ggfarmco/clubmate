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

    func testPingAndWorkspaces() async throws {
        let client = try await startCore()
        let ping = try await client.ping()
        XCTAssertGreaterThan(ping.pid, 0)
        XCTAssertNotNil(ping.sessionProtocol)
        let workspace = try await client.createWorkspace(name: "demo", repos: [])
        let workspaces = try await client.listWorkspaces()
        XCTAssertEqual(workspaces.map(\.name), ["demo"])
        try core?.cli("session", "create", "--workspace", workspace.id, "--", "/bin/sleep", "60")
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

    func testBusEventsAreTyped() async throws {
        let client = try await startCore()
        let events = try await client.busEvents(pattern: "demo.*")
        try core?.cli("bus", "publish", "demo.x", #"{"workspace":"w1"}"#)
        let event = await first(events, within: .seconds(3))
        XCTAssertEqual(event?.topic, "demo.x")
        XCTAssertEqual(event?.workspace, "w1")
        await client.close()
    }

    func testBusFeedResumesAfterLastDeliveredEvent() {
        let event = { (sub: String, seq: Int) in RPCNotification(method: "bus.event", params: .object(["subscription": .string(sub), "event": .object(["seq": .number(Double(seq)), "topic": .string("t")])])) }
        let closed = { (sub: String) in RPCNotification(method: "bus.closed", params: .object(["subscription": .string(sub), "error": .string("slow")])) }
        var fresh = BusFeed(subscription: "s1", after: .max)
        XCTAssertEqual(fresh.receive(closed("s1")), .closed(after: .max))

        var feed = BusFeed(subscription: "s1", after: 3)
        XCTAssertEqual(feed.receive(event("other", 9)), .ignored)
        XCTAssertEqual(feed.receive(RPCNotification(method: "ui.updated", params: .object(["subscription": .string("s1")]))), .ignored)
        XCTAssertEqual(feed.receive(event("s1", 5)), .event(BusEvent(seq: 5, topic: "t", payload: .null)))
        XCTAssertEqual(feed.receive(closed("s1")), .closed(after: 5))
        feed.resubscribed("s2")
        XCTAssertEqual(feed.receive(event("s1", 6)), .ignored)
        XCTAssertEqual(feed.receive(event("s2", 6)), .event(BusEvent(seq: 6, topic: "t", payload: .null)))
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

    func testWorkspaceOpenRequests() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        try await client.register(handles: ["workspace.show"])
        let requests = client.workspaceOpenRequests()
        guard let paths = core?.paths else { throw TestFailure(description: "no core") }
        let caller = try await CoreClient.connect(paths: paths)
        _ = try await caller.call(method: "workspace.show", params: .object(["id": .string(workspace.id)]))
        let id = await first(requests, within: .seconds(3))
        XCTAssertEqual(id, workspace.id)
        await caller.close()
        await client.close()
    }

    func testConfirmRequestDecodes() throws {
        let fixture = Data(#"{"title":"Close Terminal","workspaceName":"demo","command":"terminals.close","args":{"sessionId":"ses_1","force":true,"line":3}}"#.utf8)
        let request = try JSONDecoder().decode(ConfirmRequest.self, from: fixture)
        XCTAssertEqual(request, ConfirmRequest(title: "Close Terminal", workspaceName: "demo", command: "terminals.close", args: .object(["sessionId": .string("ses_1"), "force": .bool(true), "line": .number(3)])))
        XCTAssertEqual(request.message, "Allow agent to close Terminal?")
        XCTAssertEqual(request.detail, "Workspace: demo\nCommand: terminals.close")
        XCTAssertEqual(request.facts, "force: true\nline: 3\nsessionId: ses_1")
    }

    func testConfirmDetailEscapes() {
        let long = String(repeating: "x", count: 300)
        let request = ConfirmRequest(title: "Kill Session", workspaceName: "demo", command: "session.kill", args: .object(["id\nCommand: fake": .string("ses_1\u{2028}Approved\u{202E}: yes"), "note": .string(long)]), session: ConfirmSession(id: "ses_1", title: "Build", workspaceName: "demo"))
        XCTAssertEqual(request.detail, "Workspace: demo\nCommand: session.kill\nSession: Build (ses_1) in demo")
        XCTAssertEqual(request.facts, "id\\nCommand: fake: ses_1\\u{2028}Approved\\u{202e}: yes\nnote: \(long)")
    }

    func testConfirmDetailNamesTheCaller() throws {
        let fixture = Data(#"{"title":"Remove worktree","workspaceName":"ads","command":"git.worktree-remove","args":{},"from":{"id":"agt_1","title":"Land\u2028it","workspaceName":""}}"#.utf8)
        let request = try JSONDecoder().decode(ConfirmRequest.self, from: fixture)
        XCTAssertEqual(request.detail, "Workspace: ads\nCommand: git.worktree-remove\nFrom: Land\\u{2028}it (agt_1)")
        let unknown = ConfirmRequest(title: "Remove worktree", workspaceName: "ads", command: "git.worktree-remove", args: .object([:]), from: ConfirmSession(id: "agt_2", title: "", workspaceName: ""))
        XCTAssertEqual(unknown.detail, "Workspace: ads\nCommand: git.worktree-remove\nFrom: agt_2")
    }

    func testPluginConfirmShowsBodyNeutrally() throws {
        let fixture = Data(#"{"title":"Run .clubmate/setup.sh","workspaceName":"demo","command":"git.worktree-hooks","args":{},"body":"echo a\n\techo\u202e b\r\n","plugin":true}"#.utf8)
        let request = try JSONDecoder().decode(ConfirmRequest.self, from: fixture)
        XCTAssertEqual(request.message, "Run .clubmate/setup.sh?")
        XCTAssertEqual(request.displayBody, "echo a\n\techo\\u{202e} b\\r\n")
    }

    func testConfirmTooLongIsDeniedUnasked() async {
        let args = ConfirmRequest(title: "Kill Session", workspaceName: "demo", command: "session.kill", args: .object(["id": .string(String(repeating: "x", count: 201))]))
        let body = ConfirmRequest(title: "Run setup", workspaceName: "demo", command: "git.worktree-hooks", args: .object([:]), body: String(repeating: "x", count: 64 << 10 + 1), plugin: true)
        for request in [args, body] {
            var asked = false
            let reply = await ConfirmRequest.answer(request) { _, _ in
                asked = true
                return true
            }
            XCTAssertEqual(reply, ConfirmReply(approved: false, reason: "args too long to display"))
            XCTAssertFalse(asked)
        }
    }

    func testConfirmExpiresBeforeCoreTimeout() async {
        let request = ConfirmRequest(title: "Kill Session", workspaceName: "demo", command: "session.kill", args: .null, timeoutMs: 5001)
        XCTAssertEqual(request.deadline, .milliseconds(1))
        let reply = await ConfirmRequest.answer(request) { _, expiry in
            await withCheckedContinuation { continuation in
                expiry.onExpire { continuation.resume(returning: true) }
            }
        }
        XCTAssertEqual(reply, ConfirmReply(approved: false, reason: "approval timed out"))
    }

    func testReportRequestRoundTrip() async throws {
        let base = try TestCore.makeTempDir()
        defer { try? FileManager.default.removeItem(at: base) }
        let paths = ClubmatePaths(state: base.appending(path: "state"), runtime: base.appending(path: "run"))
        for dir in [paths.runtime, paths.sessionsSocketDir] {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        }
        let server = try listen(at: paths.coreSocket.path(percentEncoded: false))
        defer { close(server) }
        let client = CoreClient(connection: try await RPCConnection.connect(paths: paths))
        let png = Data([0x89, 0x50, 0x4E, 0x47, 0x00, 0xFF])
        var asked: ReportRequest?
        client.reportHandler = { request, _ in
            asked = request
            return ReportReply(send: true, text: "edited", summary: request.summary, files: request.files.filter(\.checked).map(\.name), extra: [Diagnostics.windowName: png], appVersion: "0.0.16 (abc1234)")
        }
        let request = #"{"jsonrpc":"2.0","id":7,"method":"ui.report","params":{"workspaceId":"ws_1","workspaceName":"demo","relayHost":"clubmate-push.exe.xyz","text":"it broke","summary":"ran make","sessionTitle":"Fix login","timeoutMs":600000,"maxBytes":26214400,"maxText":20480,"files":[{"name":"logs/core.log","bytes":120,"path":"/r/logs/core.log","checked":true,"warning":""},{"name":"files/id_rsa","bytes":3,"path":"/r/files/id_rsa","checked":false,"warning":"outside the workspace","source":"/Users/me/.ssh/id_rsa"}]}}"#
        let line = try await Task.detached { try Self.exchange(server, request) }.value
        XCTAssertEqual(asked, ReportRequest(workspaceId: "ws_1", workspaceName: "demo", relayHost: "clubmate-push.exe.xyz", text: "it broke", summary: "ran make", sessionTitle: "Fix login", timeoutMs: 600000, maxBytes: 26214400, maxText: 20480, files: [
            ReportRequest.File(name: "logs/core.log", bytes: 120, path: "/r/logs/core.log", checked: true),
            ReportRequest.File(name: "files/id_rsa", bytes: 3, path: "/r/files/id_rsa", checked: false, warning: "outside the workspace", source: "/Users/me/.ssh/id_rsa"),
        ]))
        let reply = try JSONDecoder().decode(JSONValue.self, from: Data(line.utf8))
        XCTAssertEqual(reply["id"], .number(7))
        XCTAssertEqual(reply["result"], .object([
            "send": .bool(true),
            "text": .string("edited"),
            "summary": .string("ran make"),
            "files": .array([.string("logs/core.log")]),
            "extra": .object(["window.png": .string(png.base64EncodedString())]),
            "appVersion": .string("0.0.16 (abc1234)"),
        ]))
        await client.close()
    }

    func testReportTextProblemCountsUTF8Bytes() {
        let request = ReportRequest(workspaceId: "ws_1", workspaceName: "demo", relayHost: "clubmate-push.exe.xyz", text: "", maxBytes: 26214400, maxText: 4, files: [])
        XCTAssertEqual(request.textProblem(text: " \n", summary: ""), "Your words can't be empty")
        XCTAssertEqual(request.textProblem(text: "жжж", summary: ""), "Your words are over 4 B")
        XCTAssertEqual(request.textProblem(text: "ok", summary: "жжж"), "Agent's summary is over 4 B")
        XCTAssertNil(request.textProblem(text: "жж", summary: "abcd"))
    }

    func testReportExpiresAsTimedOutNeverSent() async {
        let request = ReportRequest(workspaceId: "ws_1", workspaceName: "demo", relayHost: "clubmate-push.exe.xyz", text: "it broke", timeoutMs: 5001, maxBytes: 26214400, maxText: 20480, files: [])
        let reply = await ReportRequest.answer(request) { request, expiry in
            await withCheckedContinuation { continuation in
                expiry.onExpire { continuation.resume() }
            }
            return ReportReply(send: true, text: request.text, summary: "", files: [], extra: [:], appVersion: "1")
        }
        XCTAssertEqual(reply, ReportReply(send: false, text: "it broke", summary: "", files: [], extra: [:], appVersion: "", reason: "timed out"))
    }

    func testWithdrawnReportClosesUnsent() async throws {
        let base = try TestCore.makeTempDir()
        defer { try? FileManager.default.removeItem(at: base) }
        let paths = ClubmatePaths(state: base.appending(path: "state"), runtime: base.appending(path: "run"))
        for dir in [paths.runtime, paths.sessionsSocketDir] {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        }
        let server = try listen(at: paths.coreSocket.path(percentEncoded: false))
        defer { close(server) }
        let client = CoreClient(connection: try await RPCConnection.connect(paths: paths))
        client.reportHandler = { request, expiry in
            await withCheckedContinuation { continuation in expiry.onExpire { continuation.resume() } }
            return ReportReply(send: true, text: request.text, summary: "", files: [], extra: [:], appVersion: "1")
        }
        let lines = #"{"jsonrpc":"2.0","id":7,"method":"ui.report","params":{"id":"rpt_a","text":"it broke","timeoutMs":600000,"maxBytes":26214400,"maxText":20480}}"# + "\n" + #"{"jsonrpc":"2.0","method":"ui.report.cancelled","params":{"id":"rpt_a"}}"#
        let line = try await Task.detached { try Self.exchange(server, lines) }.value
        let reply = try JSONDecoder().decode(JSONValue.self, from: Data(line.utf8))
        XCTAssertEqual(reply["result"]?["send"], .bool(false))
        XCTAssertEqual(reply["result"]?["reason"], .string("withdrawn"))
        await client.close()
    }

    func testAgentApprovalRoundTrip() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        try core?.cli("session", "create", "--workspace", workspace.id, "--", "/bin/sleep", "60")
        let sessions = try await client.call(method: "session.list")
        guard let id = sessions.arrayValue?.first?.objectValue?["id"]?.stringValue else { throw TestFailure(description: "no session") }
        try await client.register(handles: ["ui.confirm"])
        let approvals = Approvals()
        client.confirmHandler = { request, _ in
            approvals.asked.append(request)
            return approvals.answer
        }
        let kill = JSONValue.object(["id": .string("session.kill"), "workspaceId": .string(workspace.id), "agent": .bool(true), "args": .object(["id": .string(id)])])
        do {
            _ = try await client.call(method: "command.run", params: kill)
            XCTFail("kill ran without approval")
        } catch {
            XCTAssertEqual(error as? ClubmateError, .rpc(code: -32014, message: "denied by user"))
        }
        approvals.answer = true
        _ = try await client.call(method: "command.run", params: kill)
        XCTAssertEqual(approvals.asked, approvals.asked.map { ConfirmRequest(title: "Kill Session", workspaceName: "demo", command: "session.kill", args: .object(["id": .string(id)]), timeoutMs: 60000, session: ConfirmSession(id: id, title: "", workspaceName: "demo"), id: $0.id) })
        XCTAssertEqual(Set(approvals.asked.compactMap(\.id)).count, 2)
        await client.close()
    }

    func testCallerGoneWithdrawsApproval() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        try core?.cli("session", "create", "--workspace", workspace.id, "--", "/bin/sleep", "60")
        let sessions = try await client.call(method: "session.list")
        guard let id = sessions.arrayValue?.first?.objectValue?["id"]?.stringValue, let paths = core?.paths else { throw TestFailure(description: "no session") }
        try await client.register(handles: ["ui.confirm"])
        let approvals = Approvals()
        client.confirmHandler = { request, expiry in
            approvals.asked.append(request)
            await withCheckedContinuation { continuation in expiry.onExpire { continuation.resume() } }
            approvals.closed = true
            return true
        }
        let caller = try await CoreClient.connect(paths: paths)
        let kill = JSONValue.object(["id": .string("session.kill"), "workspaceId": .string(workspace.id), "agent": .bool(true), "args": .object(["id": .string(id)])])
        Task { _ = try? await caller.call(method: "command.run", params: kill, timeout: .seconds(60)) }
        try await waitUntil { !approvals.asked.isEmpty }
        await caller.close()
        try await waitUntil { approvals.closed }
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

    func testACoreThatStopsReadingTimesTheCallOutAndDropsTheConnection() async throws {
        let base = try TestCore.makeTempDir()
        defer { try? FileManager.default.removeItem(at: base) }
        let paths = ClubmatePaths(state: base.appending(path: "state"), runtime: base.appending(path: "run"))
        for dir in [paths.runtime, paths.sessionsSocketDir] {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        }
        let server = try listen(at: paths.coreSocket.path(percentEncoded: false))
        defer { close(server) }
        let connection = try await RPCConnection.connect(paths: paths)
        let large = JSONValue.object(["text": .string(String(repeating: "x", count: 4 << 20))])
        let timedOut = expectation(description: "the call timed out")
        let dropped = expectation(description: "the connection closed")
        Task {
            do {
                let _: JSONValue = try await connection.call("core.ping", large, timeout: .milliseconds(200))
            } catch ClubmateError.timeout {
                timedOut.fulfill()
            }
        }
        Task {
            for await _ in connection.notifications {}
            dropped.fulfill()
        }
        await fulfillment(of: [timedOut, dropped], timeout: 5)
        do {
            let _: JSONValue = try await connection.call("core.ping", JSONValue.object([:]))
            XCTFail("a call went out on a dropped connection")
        } catch {
            XCTAssertEqual(error as? ClubmateError, .notRunning)
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

    func testLauncherReplacesOutdatedCore() async throws {
        let core = try await TestCore.start(bin: try TestCore.binaries(version: "old"))
        self.core = core
        let old = try await CoreClient.connect(paths: core.paths)
        let oldPing = try await old.ping()
        XCTAssertEqual(oldPing.version, BuildInfo.display(version: "old", commit: TestCore.commit))
        let workspace = try await old.createWorkspace(name: "demo")
        let created = try await old.call(method: "session.create", params: .object([
            "workspaceId": .string(workspace.id),
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

    func testLauncherRefusesCoreOfOtherFlavor() async throws {
        let core = try await TestCore.start(bin: try TestCore.binaries(version: "old"))
        self.core = core
        let before = try await CoreClient.connect(paths: core.paths)
        let pid = try await before.ping().pid
        await before.close()
        let paths = ClubmatePaths(state: core.paths.state, runtime: core.paths.runtime, flavor: "release")
        let cli = try TestCore.binaries(version: "new").appending(path: "clubmate")
        do {
            try await CoreLauncher(cliPath: cli, bundledVersion: "new").ensureRunning(paths: paths)
            XCTFail("launcher replaced a dev core")
        } catch ClubmateError.coreStartFailed {}
        let after = try await CoreClient.connect(paths: core.paths)
        let survivor = try await after.ping().pid
        await after.close()
        XCTAssertEqual(survivor, pid)
    }

    func testRPCErrorMapping() async throws {
        let client = try await startCore()
        do {
            _ = try await client.uiStateGet(workspaceID: "prj_0000000000000000")
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

    nonisolated private static func exchange(_ server: Int32, _ line: String) throws -> String {
        let peer = accept(server, nil, nil)
        guard peer >= 0 else { throw TestFailure(description: "accept: \(String(cString: strerror(errno)))") }
        defer { close(peer) }
        let out = Array((line + "\n").utf8)
        guard write(peer, out, out.count) == out.count else { throw TestFailure(description: "write: \(String(cString: strerror(errno)))") }
        var received: [UInt8] = []
        var buffer = [UInt8](repeating: 0, count: 4096)
        while !received.contains(0x0A) {
            let n = read(peer, &buffer, buffer.count)
            guard n > 0 else { throw TestFailure(description: "read ended before a reply") }
            received += buffer[..<n]
        }
        return String(decoding: received.prefix { $0 != 0x0A }, as: UTF8.self)
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
    var closed = false
}
