import Foundation
import XCTest

import CompanionKit

@MainActor
final class ComputerConnectionTests: XCTestCase {
    private let computer = Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621)

    func testBacksOffThenResumesEventsAfterLastSeq() async throws {
        let hellos = Recorder<Int>()
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            if FakeTransport.rpc(request) == "workspaces" { return (FakeTransport.result(workspacesJSON), 200) }
            hellos.add(1)
            if hellos.values.count <= 2 { throw URLError(.cannotConnectToHost) }
            return (FakeTransport.result(helloJSON), 200)
        }, stream: { request in
            guard request.value(forHTTPHeaderField: "Last-Event-ID") == nil else {
                return (200, AsyncThrowingStream {
                    $0.yield(Data(": connected\n\n".utf8))
                    held.add($0)
                })
            }
            let wire = ["5", "3"].map { "id: \($0)\nevent: bus\ndata: {\"seq\":\($0),\"topic\":\"agents.changed\",\"workspace\":\"w\",\"payload\":{}}\n\n" }.joined()
            return (200, AsyncThrowingStream { $0.yield(Data(wire.utf8)); $0.finish() })
        })
        let delays = Recorder<Duration>()
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { delays.add($0) })
        let signals = Recorder<ConnectionSignal>()
        connection.observe { signals.add($0) }
        connection.start()
        try await waitUntil { held.values.count == 1 && connection.state == .online }
        XCTAssertEqual(delays.values, [.seconds(1), .seconds(2), .seconds(1)])
        XCTAssertEqual(transport.requests.last?.value(forHTTPHeaderField: "Last-Event-ID"), "5")
        XCTAssertEqual(signals.values, [.online, .event(BusEvent(seq: 5, topic: "agents.changed", workspace: "w")), .event(BusEvent(seq: 3, topic: "agents.changed", workspace: "w")), .online])
        XCTAssertEqual(connection.workspaces.map(\.name), ["shop"])
        XCTAssertEqual(connection.computer.name, "Mac Studio")
        connection.stop()
    }

    func testOnlineWaitsForTheEventStream() async throws {
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
        }, stream: { _ in (200, AsyncThrowingStream { held.add($0) }) })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })
        let signals = Recorder<ConnectionSignal>()
        connection.observe { signals.add($0) }
        connection.start()
        try await waitUntil { held.values.count == 1 }
        try await Task.sleep(for: .milliseconds(20))
        XCTAssertEqual(connection.state, .connecting)
        XCTAssertEqual(signals.values, [])

        held.values[0].yield(Data(": connected\n\n".utf8))

        try await waitUntil { connection.state == .online }
        XCTAssertEqual(signals.values, [.online])
        connection.stop()
    }

    func testCancelledRunLeavesTheNewerStateAlone() async throws {
        let hellos = Recorder<Int>()
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            if FakeTransport.rpc(request) == "workspaces" { return (FakeTransport.result(workspacesJSON), 200) }
            hellos.add(1)
            if hellos.values.count > 1 { try await Task.sleep(for: .seconds(30)) }
            return (FakeTransport.result(helloJSON), 200)
        }, stream: { _ in (200, AsyncThrowingStream { held.add($0) }) })
        let delays = Recorder<Duration>()
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { delays.add($0) })
        connection.start()
        try await waitUntil { held.values.count == 1 }

        connection.reconnectNow()

        try await waitUntil { hellos.values.count == 2 }
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(connection.state, .connecting)
        XCTAssertEqual(delays.values, [])
        connection.stop()
    }

    func testStreamFailuresBackOffWithoutResetting() async throws {
        let transport = FakeTransport(reply: { request in
            (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
        }, stream: { _ in throw URLError(.networkConnectionLost) })
        let delays = Recorder<Duration>()
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { delays.add($0) })
        connection.start()
        try await waitUntil { delays.values.count >= 4 }
        connection.stop()
        XCTAssertEqual(Array(delays.values.prefix(4)), [.seconds(1), .seconds(2), .seconds(5), .seconds(10)])
    }

    func testUnauthorizedStopsReconnecting() async throws {
        let transport = FakeTransport(reply: { _ in (Data(), 401) })
        let delays = Recorder<Duration>()
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { delays.add($0) })
        connection.start()
        try await waitUntil { connection.state == .unauthorized }
        connection.reconnectNow()
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(transport.requests.count, 1)
        XCTAssertEqual(delays.values, [])
    }

    func testRevokedCommandForgetsHelloAndWorkspaces() async throws {
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case "hello": (FakeTransport.result(helloJSON), 200)
            default: (Data(), 401)
            }
        }, stream: { _ in
            (200, AsyncThrowingStream {
                $0.yield(Data(": connected\n\n".utf8))
                held.add($0)
            })
        })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .online && connection.hello != nil }

        _ = try? await connection.command("agents.sessions", workspace: "w", args: nil)

        XCTAssertEqual(connection.state, .unauthorized)
        XCTAssertNil(connection.hello)
        XCTAssertEqual(connection.workspaces, [])
    }
}
