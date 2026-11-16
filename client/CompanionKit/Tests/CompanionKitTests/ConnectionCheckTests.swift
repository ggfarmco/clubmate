import Foundation
import XCTest

import CompanionKit

private struct FakeProbe: NetworkProbe {
    var mine: [String] = ["100.64.0.9"]
    var names: [String: ResolveResult] = [:]
    var ports: [String: ConnectResult] = [:]

    func tailnetAddresses() -> [String] { mine }
    func resolve(_ name: String) async -> ResolveResult { names[name] ?? .failed("unknown") }
    func connect(host: String, port: Int, timeout: Duration) async -> ConnectResult { ports[host] ?? .open }
}

@MainActor
final class ConnectionLogTests: XCTestCase {
    func testFailedConnectKeepsEachHostsErrorThenTheSuccessAndSurvivesARelaunch() async throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: dir) }
        let computer = Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1", "fd7a:115c:a1e0::1"], port: 47621)
        let refused = URLError(.cannotConnectToHost, userInfo: [NSUnderlyingErrorKey: NSError(domain: NSPOSIXErrorDomain, code: 61)])
        let up = Recorder<Bool>()
        let transport = FakeTransport(reply: { request in
            guard up.values.count > 0 else { throw request.url?.host() == "100.64.0.1" ? refused : URLError(.timedOut) }
            return (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let connection = ComputerConnection(computer: computer, token: "secret-token", transport: transport, cache: ListCache(dir: dir), sleep: { _ in up.add(true) })
        connection.start()
        try await waitUntil { connection.state == .online }
        connection.stop()

        let attempts = connection.log.attempts
        XCTAssertEqual(attempts.map(\.title), ["Failed at hello", "Connected"])
        XCTAssertEqual(attempts[0].tries.map(\.host), ["100.64.0.1", "fd7a:115c:a1e0::1"])
        XCTAssertTrue(attempts[0].tries[0].outcome.hasPrefix("URLError -1004"), attempts[0].tries[0].outcome)
        XCTAssertTrue(attempts[0].tries[0].outcome.hasSuffix("POSIX 61 · Connection refused"), attempts[0].tries[0].outcome)
        XCTAssertTrue(attempts[0].tries[1].outcome.hasPrefix("URLError -1001"), attempts[0].tries[1].outcome)
        XCTAssertEqual(attempts[0].streak, 1)
        XCTAssertEqual(attempts[1].host, "100.64.0.1")
        XCTAssertFalse(connection.log.text.contains("secret-token"))

        let relaunched = ComputerConnection(computer: computer, token: "secret-token", transport: transport, cache: ListCache(dir: dir))
        XCTAssertEqual(relaunched.log.attempts.map(\.id), attempts.map(\.id))
        XCTAssertEqual(relaunched.log.attempts.map(\.tries), attempts.map(\.tries))
    }

    func testAStreamThatDropsIsLoggedWithHowLongItLived() async throws {
        let clock = TestClock()
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
        }, stream: { _ in
            (200, AsyncThrowingStream {
                $0.yield(Data(": connected\n\n".utf8))
                held.add($0)
            })
        })
        let connection = ComputerConnection(computer: Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621), token: "t", transport: transport, sleep: { _ in try await Task.sleep(for: .seconds(60)) }, now: clock.now)
        connection.start()
        try await waitUntil { connection.state == .online && held.values.count == 1 }
        clock.advance(.seconds(125))
        held.values[0].finish(throwing: URLError(.networkConnectionLost))

        try await waitUntil { connection.log.attempts.count == 2 }
        XCTAssertEqual(connection.log.attempts[1].title, "Lost after 2 min")
        XCTAssertTrue(connection.log.attempts[1].tries.last?.outcome.hasPrefix("URLError -1005") == true, "\(connection.log.attempts[1].tries)")
        connection.stop()
    }

    func testRepeatedFailuresFoldIntoOneEntryAndTheRingKeepsTheNewest() {
        var log = ConnectionLog()
        let failure = ConnectionAttempt(at: .now, step: .hello, ok: false, milliseconds: 4000, error: "URLError -1001", tries: [HostTry(host: "100.64.0.1", milliseconds: 4000, outcome: "URLError -1001", ok: false)])
        for _ in 0..<3 { log.record(failure) }
        XCTAssertEqual(log.attempts.count, 1)
        XCTAssertEqual(log.attempts[0].repeats, 3)
        XCTAssertEqual(log.attempts[0].streak, 3)

        for index in 0..<60 {
            log.record(ConnectionAttempt(at: .now, step: .hello, ok: index.isMultiple(of: 2), milliseconds: index))
        }
        XCTAssertEqual(log.attempts.count, ConnectionLog.capacity)
        XCTAssertEqual(log.attempts.last?.milliseconds, 59)
    }
}

@MainActor
final class ConnectionCheckTests: XCTestCase {
    private let token = "t"

    private func stream() -> FakeTransport {
        FakeTransport(reply: { request in
            request.value(forHTTPHeaderField: "Authorization") == "Bearer t" ? (FakeTransport.result(helloJSON), 200) : (Data(), 401)
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
    }

    func testEveryStepPassesAndThePassReconnects() async {
        let reconnects = Recorder<Bool>()
        let probe = FakeProbe(names: ["mac.tail1.ts.net": .addresses(["100.64.0.1"])])
        let check = ConnectionCheck(hosts: ["mac.tail1.ts.net", "100.64.0.1"], port: 47621, token: token, transport: stream(), policy: .tailnet, probe: probe) { reconnects.add(true) }
        await check.run()

        XCTAssertEqual(check.steps.map(\.id), ["tailscale", "dns:mac.tail1.ts.net", "tcp:mac.tail1.ts.net", "tcp:100.64.0.1", "hello", "events"])
        XCTAssertEqual(check.steps.map(\.verdict), Array(repeating: .pass, count: 6))
        XCTAssertEqual(check.steps.first { $0.id == "hello" }?.detail, "Mac Studio · Clubmate 0.0.13 via mac.tail1.ts.net")
        XCTAssertEqual(reconnects.values, [true])
    }

    func testRefusedAndSilentHostsGetTheirOwnHintsAndSkipTheRest() async {
        let probe = FakeProbe(mine: [], ports: ["100.64.0.1": .refused, "100.64.0.2": .timedOut])
        let check = ConnectionCheck(hosts: ["100.64.0.1", "100.64.0.2"], port: 47621, token: token, transport: stream(), policy: .tailnet, probe: probe)
        await check.run()

        let byID = Dictionary(uniqueKeysWithValues: check.steps.map { ($0.id, $0) })
        XCTAssertEqual(byID["tailscale"]?.hint, "Tailscale isn't on on this phone — open the Tailscale app and connect.")
        XCTAssertEqual(byID["dns"]?.verdict, .skipped)
        XCTAssertTrue(byID["tcp:100.64.0.1"]?.hint?.contains("is Clubmate running with companion access on") == true)
        XCTAssertTrue(byID["tcp:100.64.0.2"]?.hint?.contains("asleep") == true)
        XCTAssertEqual(byID["hello"]?.verdict, .skipped)
        XCTAssertEqual(byID["events"]?.verdict, .skipped)
    }

    func testAnAnsweringTailnetHostOverridesAMissingPhoneAddress() async {
        let check = ConnectionCheck(hosts: ["100.64.0.1"], port: 47621, token: token, transport: stream(), policy: .tailnet, probe: FakeProbe(mine: []))
        await check.run()

        XCTAssertEqual(check.steps.first?.verdict, .pass)
        XCTAssertFalse(check.failed)
    }

    func testARefusedTokenSaysPairAgain() async {
        let check = ConnectionCheck(hosts: ["100.64.0.1"], port: 47621, token: "old", transport: stream(), policy: .tailnet, probe: FakeProbe())
        await check.run()

        XCTAssertEqual(check.steps.first { $0.id == "hello" }?.hint, "Pair again: on the Mac open Settings › Remote › Pair a device.")
        XCTAssertEqual(check.steps.last?.verdict, .skipped)
    }
}
