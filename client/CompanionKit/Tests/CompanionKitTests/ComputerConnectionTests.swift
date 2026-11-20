import Foundation
import Synchronization
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
        XCTAssertEqual(transport.requests.last?.value(forHTTPHeaderField: "Last-Event-ID"), "3")
        XCTAssertEqual(signals.values, [.online, .event(BusEvent(seq: 5, topic: "agents.changed", workspace: "w")), .event(BusEvent(seq: 3, topic: "agents.changed", workspace: "w")), .online])
        XCTAssertEqual(connection.workspaces.map(\.name), ["shop"])
        XCTAssertEqual(connection.computer.name, "Mac Studio")
        connection.stop()
    }

    func testResetMovesTheCursorWithoutAnEvent() async throws {
        let transport = FakeTransport(reply: { request in
            (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\nevent: reset\ndata: {\"seq\":42}\n\n".utf8)) }) })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, lastEventID: 3, sleep: { _ in })
        let signals = Recorder<ConnectionSignal>()
        connection.observe { signals.add($0) }
        connection.start()

        try await waitUntil { connection.lastEventID == 42 }
        XCTAssertEqual(signals.values, [.online])
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
        XCTAssertNil(connection.lastSeen)

        held.values[0].yield(Data(": connected\n\n".utf8))

        try await waitUntil { connection.state == .online }
        XCTAssertEqual(signals.values, [.online])
        XCTAssertNotNil(connection.lastSeen)
        connection.stop()
        XCTAssertTrue(connection.presence.hasPrefix("offline since "), connection.presence)
    }

    func testPluginChangedRefetchesHello() async throws {
        let hellos = Recorder<Int>()
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            if FakeTransport.rpc(request) == "workspaces" { return (FakeTransport.result(workspacesJSON), 200) }
            hellos.add(1)
            return (FakeTransport.result(hellos.values.count == 1 ? helloJSON : #"{"computer":{"id":"mac1","name":"Mac Studio"},"version":"0.0.13","commands":[],"topics":[]}"#), 200)
        }, stream: { _ in
            (200, AsyncThrowingStream {
                $0.yield(Data(": connected\n\n".utf8))
                held.add($0)
            })
        })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .online && held.values.count == 1 }
        XCTAssertTrue(connection.supports("agents.sessions"))

        held.values[0].yield(Data("id: 9\nevent: bus\ndata: {\"seq\":9,\"topic\":\"plugin.changed\",\"payload\":{\"id\":\"agents\",\"running\":false}}\n\n".utf8))

        try await waitUntil { !connection.supports("agents.sessions") }
        connection.stop()
    }

    func testVoiceFollowsTheMacsSettings() async throws {
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case "voice.settings": (FakeTransport.result(#"{"enabled":true,"model":"nemotron","models":[]}"#), 200)
            default: (FakeTransport.result(#"{"computer":{"id":"mac1","name":"Mac Studio"},"version":"0.0.16","commands":["voice.start","voice.settings"],"topics":[]}"#), 200)
            }
        }, stream: { _ in
            (200, AsyncThrowingStream {
                $0.yield(Data(": connected\n\n".utf8))
                held.add($0)
            })
        })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .online && held.values.count == 1 }
        XCTAssertTrue(connection.voiceAvailable)

        held.values[0].yield(Data("id: 4\nevent: bus\ndata: {\"seq\":4,\"topic\":\"voice.settings-changed\",\"payload\":{\"enabled\":false,\"model\":\"nemotron\",\"models\":[]}}\n\n".utf8))

        try await waitUntil { !connection.voiceAvailable }
        connection.stop()
    }

    func testVoiceIsOnForAMacWithoutVoiceSettings() async throws {
        let transport = FakeTransport(reply: { request in
            (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : #"{"computer":{"id":"mac1","name":"Mac Studio"},"version":"0.0.15","commands":["voice.start"],"topics":[]}"#), 200)
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .online }
        XCTAssertTrue(connection.voiceAvailable)
        XCTAssertFalse(transport.requests.contains { FakeTransport.rpc($0) == "voice.settings" })
        connection.stop()
    }

    func testAwakeFollowsTheMac() async throws {
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case "awake.state": (FakeTransport.result(awakeHoursJSON), 200)
            default: (FakeTransport.result(#"{"computer":{"id":"mac1","name":"Mac Studio"},"version":"0.0.71","commands":["awake.state","awake.set"],"topics":[]}"#), 200)
            }
        }, stream: { _ in
            (200, AsyncThrowingStream {
                $0.yield(Data(": connected\n\n".utf8))
                held.add($0)
            })
        })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })
        connection.start()
        try await waitUntil { connection.awake != nil && held.values.count == 1 }
        XCTAssertEqual(connection.awake?.text, "Awake until 18:00 +1")
        XCTAssertEqual(connection.awake?.agentHolds, [AwakeHold(kind: "agent", title: "release build", trailing: "Ship 2.4 release · ≤ 20:40", hold: "ah_1", session: "agt_1", workspace: "prj_1")])
        XCTAssertEqual(connection.awake?.labels?.hours.options.map(\.hours), [1, 2, 4, 8, 24])
        XCTAssertEqual(connection.awake?.labels?.hours.options.first?.short, "1 h")
        XCTAssertEqual(connection.awake?.ends, "until 18:00")

        held.values[0].yield(Data("id: 4\nevent: bus\ndata: {\"seq\":4,\"topic\":\"awake.changed\",\"payload\":\(awakeOffJSON)}\n\n".utf8))

        try await waitUntil { connection.awake?.on == false }
        XCTAssertEqual(connection.awake?.value, "Off")
        connection.stop()
    }

    func testAwakeIsHiddenForAMacWithoutTheAwakePlugin() async throws {
        let transport = FakeTransport(reply: { request in
            (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .online }
        XCTAssertNil(connection.awake)
        XCTAssertFalse(transport.requests.contains { FakeTransport.rpc($0) == "awake.state" })
        connection.stop()
    }

    func testSetAwakeSendsTheChoiceAndShowsTheReply() async throws {
        let transport = FakeTransport(reply: { _ in (FakeTransport.result(awakeHoursJSON), 200) })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })

        try await connection.setAwake(mode: "hours", hours: 4)

        let body = try JSONDecoder().decode(JSONValue.self, from: XCTUnwrap(transport.requests.last?.httpBody))
        XCTAssertEqual(body["params"]?["id"]?.stringValue, "awake.set")
        XCTAssertEqual(body["params"]?["args"], .object(["mode": .string("hours"), "hours": .number(4)]))
        XCTAssertEqual(connection.awake?.value, "Until 18:00")
    }

    func testSetAwakeReplyNeverOverwritesAChangeThatArrivedMeanwhile() async throws {
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let gate = AsyncStream<Void>.makeStream()
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": return (FakeTransport.result(workspacesJSON), 200)
            case "awake.state": return (FakeTransport.result(awakeOffJSON), 200)
            case "awake.set":
                for await _ in gate.stream { break }
                return (FakeTransport.result(awakeHoursJSON), 200)
            default: return (FakeTransport.result(#"{"computer":{"id":"mac1","name":"Mac Studio"},"version":"0.0.71","commands":["awake.state","awake.set"],"topics":[]}"#), 200)
            }
        }, stream: { _ in
            (200, AsyncThrowingStream {
                $0.yield(Data(": connected\n\n".utf8))
                held.add($0)
            })
        })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in })
        connection.start()
        try await waitUntil { connection.awake?.value == "Off" && held.values.count == 1 }

        let setting = Task { try await connection.setAwake(mode: "hours", hours: 4) }
        try await waitUntil { transport.requests.contains { FakeTransport.rpc($0) == "awake.set" } }
        held.values[0].yield(Data("id: 5\nevent: bus\ndata: {\"seq\":5,\"topic\":\"awake.changed\",\"payload\":{\"on\":true,\"text\":\"Awake · 2 agents\",\"mode\":\"agents\",\"value\":\"While agents work\"}}\n\n".utf8))
        try await waitUntil { connection.awake?.mode == "agents" }
        gate.continuation.yield()
        try await setting.value

        XCTAssertEqual(connection.awake?.mode, "agents")
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

    func testRequestsTheSystemCancelsAreRetried() async throws {
        let hellos = Recorder<Int>()
        let streams = Recorder<Int>()
        let transport = FakeTransport(reply: { request in
            if FakeTransport.rpc(request) == "workspaces" { return (FakeTransport.result(workspacesJSON), 200) }
            hellos.add(1)
            if hellos.values.count == 1 { throw URLError(.cancelled) }
            return (FakeTransport.result(helloJSON), 200)
        }, stream: { _ in
            streams.add(1)
            return (200, AsyncThrowingStream {
                $0.yield(Data(": connected\n\n".utf8))
                if streams.values.count == 1 { $0.finish(throwing: URLError(.cancelled)) }
            })
        })
        let delays = Recorder<Duration>()
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { delays.add($0) })
        connection.start()
        try await waitUntil { streams.values.count == 2 && connection.state == .online }
        XCTAssertEqual(delays.values, [.seconds(1), .seconds(1)])
        connection.stop()
    }

    func testNetworkReturningCutsTheBackoffShortButLeavesAStoppedConnectionAlone() async throws {
        let hellos = Recorder<Int>()
        let transport = FakeTransport(reply: { request in
            if FakeTransport.rpc(request) == "workspaces" { return (FakeTransport.result(workspacesJSON), 200) }
            hellos.add(1)
            if hellos.values.count == 1 { throw URLError(.cannotConnectToHost) }
            return (FakeTransport.result(helloJSON), 200)
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in try await Task.sleep(for: .seconds(60)) })
        connection.start()
        try await waitUntil { connection.state == .offline(URLError(.cannotConnectToHost).localizedDescription) }

        connection.networkReturned()

        try await waitUntil { connection.state == .online }
        connection.stop()
        let sent = transport.requests.count
        connection.networkReturned()
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(transport.requests.count, sent)
        XCTAssertEqual(connection.state, .offline(nil))
    }

    func testReconnectNowRestartsAStreamThatWentQuiet() async throws {
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
        }, stream: { _ in
            (200, AsyncThrowingStream {
                $0.yield(Data(": connected\n\n".utf8))
                held.add($0)
            })
        })
        let clock = TestClock()
        let connection = ComputerConnection(computer: computer, token: "t", transport: transport, sleep: { _ in }, now: clock.now)
        let signals = Recorder<ConnectionSignal>()
        connection.observe { signals.add($0) }
        connection.start()
        try await waitUntil { connection.state == .online && held.values.count == 1 }

        clock.advance(.seconds(25))
        let seen = connection.lastSeen
        held.values[0].yield(Data(": ping\n\n".utf8))
        try await waitUntil { connection.lastSeen != seen }
        XCTAssertEqual(signals.values, [.online])
        clock.advance(.seconds(25))
        connection.reconnectNow()
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(held.values.count, 1)

        clock.advance(.seconds(10))
        connection.reconnectNow()

        XCTAssertEqual(connection.state, .online)
        try await waitUntil { held.values.count == 2 && signals.values.count == 2 }
        XCTAssertEqual(signals.values, [.online, .online])
        connection.stop()
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

private let awakeHoursJSON = #"{"on":true,"armed":false,"text":"Awake until 18:00 +1","detail":"Kept awake by you until 18:00 and by an agent's release build.","mode":"hours","hours":4,"until":1794870000,"value":"Until 18:00","ends":"until 18:00","holds":[{"kind":"you","title":"You","trailing":"for 4 hours · until 18:00"},{"kind":"agent","id":"ah_1","title":"release build","trailing":"Ship 2.4 release · ≤ 20:40","session":"agt_1","workspace":"prj_1"}],"labels":{"section":"Keep Awake","picker":"Keep awake","hours":{"title":"For Hours","active":"For 4 Hours","options":[{"hours":1,"title":"1 Hour","short":"1 h"},{"hours":2,"title":"2 Hours"},{"hours":4,"title":"4 Hours"},{"hours":8,"title":"8 Hours"},{"hours":24,"title":"24 Hours"}]},"agents":{"title":"While Agents Work","subtitle":"Running or waiting for you"},"on":{"title":"Until Turned Off"},"off":{"title":"Turn Off"}},"laptop":false,"onBattery":false}"#

private let awakeOffJSON = #"{"on":false,"text":"","detail":"The Mac sleeps as usual.","mode":"","value":"Off","holds":[]}"#

@MainActor
final class ComputerConnectionPathTests: XCTestCase {
    private final class Tailnet: Sendable {
        let up = Mutex(true)
        let held = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport: FakeTransport

        init() {
            let box = Mutex<Tailnet?>(nil)
            transport = FakeTransport(reply: { request in
                guard box.withLock({ $0! }).up.withLock({ $0 }) else { throw URLError(.cannotConnectToHost) }
                if FakeTransport.rpc(request) == "voice.audio" { return (FakeTransport.result("{}"), 200) }
                return (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
            }, stream: { _ in
                let tailnet = box.withLock { $0! }
                guard tailnet.up.withLock({ $0 }) else { throw URLError(.cannotConnectToHost) }
                return (200, AsyncThrowingStream {
                    $0.yield(Data(": connected\n\n".utf8))
                    tailnet.held.add($0)
                })
            })
            box.withLock { $0 = self }
        }

        func drop() {
            up.withLock { $0 = false }
            for stream in held.values { stream.finish() }
        }

        var streams: [URLRequest] { transport.requests.filter { $0.url?.path() == "/v1/events" } }
    }

    private func both(_ relay: FakeRelay, id: String = "mac1") -> Computer {
        Computer(id: id, name: "Mac", hosts: ["100.64.0.1"], port: 47621, relay: relayRoute(relay.privateKey))
    }

    func testMacsOnTailscaleAndOnTheRelayAreOnlineAtOnceWithOneStreamEach() async throws {
        let relay = FakeRelay(endStreams: false)
        let tailnet = Tailnet()
        let transport = RoutedTransport(relay: relay, tailnet: tailnet.transport)
        let a = ComputerConnection(computer: Computer(id: "mac1", name: "A", hosts: ["100.64.0.1"], port: 47621), token: "t", transport: transport, sleep: { _ in })
        let b = ComputerConnection(computer: Computer(id: "mac2", name: "B", hosts: [], port: 47621, relay: relayRoute(relay.privateKey)), token: "t", transport: transport, relayTicket: "cmk_t", sleep: { _ in })
        a.start()
        b.start()
        try await waitUntil { a.state == .online && b.state == .online }

        XCTAssertEqual(a.activePath, .tailnet)
        XCTAssertEqual(b.activePath, .relay)
        XCTAssertEqual(tailnet.streams.count, 1)
        XCTAssertEqual(relay.transport.requests.filter { $0.url?.lastPathComponent == "stream" }.count, 1)
        XCTAssertEqual(b.events.lines.last { $0.kind == .state }?.text, "online path=relay")
        XCTAssertEqual(b.log.attempts.last?.path, .relay)
        XCTAssertEqual(a.pathChoices, [])
        XCTAssertEqual(b.pathChoices, [])
        a.stop()
        b.stop()
    }

    func testAutoFallsToTheRelayWhenTailscaleIsUnsentAndBackWhenTheRelayIs() async throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: dir) }
        let relay = FakeRelay(endStreams: false)
        let tailnet = Tailnet()
        tailnet.up.withLock { $0 = false }
        let transport = RoutedTransport(relay: relay, tailnet: tailnet.transport)
        let connection = ComputerConnection(computer: both(relay), token: "t", transport: transport, relayTicket: "cmk_t", cache: ListCache(dir: dir), sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .online && connection.activePath == .relay }

        XCTAssertEqual(tailnet.transport.requests.first?.timeoutInterval, RemoteClient.probeTimeout)
        let tries = try XCTUnwrap(connection.log.attempts.last?.tries.map(\.host))
        XCTAssertEqual(tries.first, "100.64.0.1")
        XCTAssertEqual(Set(tries.dropFirst()), ["relay relay.example"])
        XCTAssertEqual(ComputerConnection(computer: both(relay), token: "t", transport: transport, relayTicket: "cmk_t", cache: ListCache(dir: dir)).selector.order, [.relay, .tailnet])

        tailnet.up.withLock { $0 = true }
        relay.fail("/v1/d/call", [.status(503)])
        relay.closeStreams()
        try await waitUntil { connection.state == .online && connection.activePath == .tailnet }

        XCTAssertEqual(tailnet.streams.count, 1)
        XCTAssertEqual(ComputerConnection(computer: both(relay), token: "t", transport: transport, relayTicket: "cmk_t", cache: ListCache(dir: dir)).selector.order, [.tailnet, .relay])
        connection.stop()
    }

    func testAQueuedWriteLandsWithItsRequestIdAfterThePathChanges() async throws {
        let relay = FakeRelay(endStreams: false)
        let tailnet = Tailnet()
        let connection = ComputerConnection(computer: both(relay), token: "t", transport: RoutedTransport(relay: relay, tailnet: tailnet.transport), relayTicket: "cmk_t", sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .online && connection.activePath == .tailnet }
        tailnet.drop()
        let queue = ActionQueue(file: nil)
        let item = QueuedAction(computer: "mac1", workspace: "w", command: BacklogCommand.update, args: .object(["id": .number(7), "title": .string("A")]), target: "task/7", title: "Edit task")
        queue.enqueue(item)
        try await waitUntil { connection.state == .online && connection.activePath == .relay }

        _ = await queue.flush(computer: "mac1", runner: connection, supports: { _ in true })

        XCTAssertTrue(queue.items.isEmpty)
        let sent = try relay.bodies.filter { !$0.isEmpty }.map { try JSONDecoder().decode(JSONValue.self, from: $0) }.filter { $0["params"]?["id"]?.stringValue == BacklogCommand.update }
        XCTAssertEqual(sent.map { $0["params"]?["requestId"]?.stringValue }, [item.id])
        connection.stop()
    }

    func testAVoiceCallThatLostItsLinkIsNotResentOverTheOtherPath() async throws {
        let relay = FakeRelay(endStreams: false)
        let tailnet = Tailnet()
        let computer = Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621, relay: relayRoute(relay.privateKey), path: .relay)
        let connection = ComputerConnection(computer: computer, token: "t", transport: RoutedTransport(relay: relay, tailnet: tailnet.transport), relayTicket: "cmk_t", sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .online }
        relay.fail("/v1/d/call", [.status(502)])

        do {
            _ = try await connection.command("voice.audio", workspace: nil, args: nil)
            XCTFail("the voice call went through")
        } catch {
            XCTAssertEqual(error as? RemoteError, .unreachable(URLError(.networkConnectionLost).localizedDescription))
        }

        XCTAssertTrue(tailnet.transport.requests.isEmpty)
        connection.stop()
    }

    func testABackgroundProbeMovesARelayConnectionBackToTailscale() async throws {
        let relay = FakeRelay(endStreams: false)
        let tailnet = Tailnet()
        tailnet.up.withLock { $0 = false }
        let clock = TestClock()
        let connection = ComputerConnection(computer: both(relay), token: "t", transport: RoutedTransport(relay: relay, tailnet: tailnet.transport), relayTicket: "cmk_t", sleep: { _ in }, now: clock.now)
        connection.start()
        try await waitUntil { connection.state == .online && connection.activePath == .relay }
        tailnet.up.withLock { $0 = true }

        clock.advance(PathSelector.probeEvery)
        try relay.push(": ping\n\n")
        try await waitUntil { connection.events.lines.last { $0.kind == .state }?.text == "online path=tailnet" }

        XCTAssertEqual(tailnet.streams.map { $0.url?.query() }, ["probe=1", nil])
        XCTAssertEqual(connection.events.lines.filter { $0.kind == .state }.map(\.text).suffix(3), ["probe path=tailnet", "probe ok path=tailnet", "online path=tailnet"])
        connection.stop()
    }

    func testARevokedRelayTicketNeedsAPairAndNeverFallsToTailscale() async throws {
        let relay = FakeRelay(endStreams: false)
        let tailnet = Tailnet()
        let computer = Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621, relay: relayRoute(relay.privateKey))
        let dir = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: dir) }
        tailnet.up.withLock { $0 = false }
        let first = ComputerConnection(computer: computer, token: "t", transport: RoutedTransport(relay: relay, tailnet: tailnet.transport), relayTicket: "cmk_t", cache: ListCache(dir: dir), sleep: { _ in })
        first.start()
        try await waitUntil { first.activePath == .relay }
        first.stop()
        tailnet.up.withLock { $0 = true }
        relay.fail("/v1/d/open", [.status(401)])
        let sent = tailnet.transport.requests.count

        let connection = ComputerConnection(computer: computer, token: "t", transport: RoutedTransport(relay: relay, tailnet: tailnet.transport), relayTicket: "cmk_t", cache: ListCache(dir: dir), sleep: { _ in })
        connection.start()
        try await waitUntil { connection.state == .unauthorized }

        XCTAssertEqual(connection.revokedBy, .relayRevoked)
        XCTAssertEqual(connection.revokedTitle, "Relay access revoked")
        XCTAssertEqual(connection.pathChoices, PathChoice.allCases)
        XCTAssertEqual(tailnet.transport.requests.count, sent)
        XCTAssertEqual(connection.log.attempts.last?.error, "relay HTTP 401 · ticket refused")
    }
}
