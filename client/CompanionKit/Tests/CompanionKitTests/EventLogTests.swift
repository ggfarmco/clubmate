import Foundation
import XCTest

@testable import CompanionKit

@MainActor
final class EventLogTests: XCTestCase {
    private let computer = Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621)

    func testRingKeepsTheNewest500() {
        var log = EventLog()
        for i in 0..<(EventLog.capacity + 20) {
            log.add(.command, "line \(i)")
        }

        XCTAssertEqual(log.lines.count, EventLog.capacity)
        XCTAssertEqual(log.lines.first?.text, "line 20")
        XCTAssertEqual(log.lines.last?.text, "line \(EventLog.capacity + 19)")
    }

    func testRepeatedLinesFoldSoTheRingKeepsItsHistory() {
        var log = EventLog()
        log.add(.state, "offline")
        for _ in 0..<(EventLog.capacity + 10) {
            log.add(.command, "failed id=agents.transcript code=unreachable")
        }

        XCTAssertEqual(log.lines.map(\.text), ["offline", "failed id=agents.transcript code=unreachable"])
        XCTAssertEqual(log.lines.last?.repeats, EventLog.capacity + 10)
    }

    func testStateChangesAreLoggedAndSurviveARelaunch() async throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: dir) }
        let up = Recorder<Bool>()
        let transport = FakeTransport(reply: { request in
            guard up.values.count > 2 else { throw URLError(.cannotConnectToHost) }
            return (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\nevent: reset\ndata: {\"seq\":42}\n\n".utf8)) }) })
        let connection = ComputerConnection(computer: computer, token: "secret-token", transport: transport, cache: ListCache(dir: dir), sleep: { _ in up.add(true) })
        connection.start()
        try await waitUntil { connection.state == .online && connection.lastEventID == 42 }
        connection.stop()

        let texts = connection.events.lines.map { "\($0.kind.rawValue) \($0.text)" }
        XCTAssertEqual(texts.filter { $0.hasPrefix("state offline err=") }.count, 1, "\(texts)")
        XCTAssertEqual(texts.last, "state offline stopped")
        XCTAssertEqual(texts.filter { $0 == "state online path=tailnet" }.count, 1, "\(texts)")
        XCTAssertTrue(texts.contains("stream reset seq=42"), "\(texts)")
        XCTAssertFalse(connection.events.text.contains("secret-token"))

        let relaunched = ComputerConnection(computer: computer, token: "secret-token", transport: transport, cache: ListCache(dir: dir))
        XCTAssertEqual(relaunched.events.text, connection.events.text)
    }

    func testOutboxEventsCarryNoText() async throws {
        let queue = ActionQueue(file: nil)
        let lines = Recorder<String>()
        queue.onEvent = { _, line in lines.add(line) }
        let runner = OutboxRunner(failures: [RemoteError.server(code: ServerReason.stale, message: "task SECRET changed")])
        let store = BacklogStore(workspace: "w", runner: runner, queue: queue, computer: "m")

        _ = try? await store.update(7, ["title": .string("SECRET")])

        let text = lines.values.joined(separator: "\n")
        XCTAssertTrue(text.contains("queued cmd=backlog.update target=task/7 req="), text)
        XCTAssertTrue(text.contains("failed code=stale cmd=backlog.update"), text)
        XCTAssertFalse(text.contains("SECRET"), text)
    }

    func testAFailedCommandIsLoggedWithItsCode() async throws {
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case "hello": (FakeTransport.result(helloJSON), 200)
            default: (Data(#"{"error":{"code":"refused","message":"no such task SECRET"}}"#.utf8), 200)
            }
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .online }

        _ = try? await connection.command("backlog.update", workspace: "w", args: .object(["title": .string("SECRET")]))
        connection.stop()

        XCTAssertTrue(connection.events.text.contains("command failed id=backlog.update err=server refused"), connection.events.text)
        XCTAssertFalse(connection.events.text.contains("SECRET"), connection.events.text)
    }
}
