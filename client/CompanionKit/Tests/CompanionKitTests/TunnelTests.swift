import CryptoKit
import Foundation
import Synchronization
import XCTest

@testable import CompanionKit

final class TunnelTests: XCTestCase {
    func testHelloCommandAndEventsGoThroughOneChannel() async throws {
        let relay = FakeRelay()
        let tunnel = relay.tunnel()
        let client = RemoteClient(route: .relay(tunnel, computer: "mac1"), token: "cmt_x", transport: FakeTransport())
        let trace = HostTrace()

        let hello = try await client.hello(trace: trace)
        XCTAssertEqual(hello.computer.id, "mac1")
        let reply = try await client.command("agents.sessions", workspace: "w")
        XCTAssertEqual(reply, .object(["ok": .bool(true)]))
        var items: [EventStreamItem] = []
        for try await item in try await client.events(after: 5) { items.append(item) }

        XCTAssertEqual(items, [.alive, .ping, .event(BusEvent(seq: 7, topic: "agents.changed", payload: .object([:]))), .ping])
        XCTAssertEqual(trace.values.map(\.host), ["relay relay.example"])
        let seen = relay.seen
        XCTAssertEqual(seen.map(\.path), ["/v1/rpc", "/v1/rpc", "/v1/events"])
        XCTAssertEqual(seen.map { $0.header?["Authorization"] }, Array(repeating: "Bearer cmt_x", count: 3))
        XCTAssertEqual(seen.last?.header?["Last-Event-ID"], "5")
        XCTAssertEqual(relay.opens, 1)

        _ = try await client.hello()
        XCTAssertEqual(relay.opens, 2)
        XCTAssertEqual(relay.ticketHashes, Array(repeating: Tunnel.ticketHash("cmk_t"), count: 2))
        let outer = relay.transport.requests
        XCTAssertEqual(outer.map { $0.url?.absoluteString }, ["open", "call", "call", "stream", "open", "call"].map { "https://relay.example/v1/d/\($0)" })
        XCTAssertTrue(outer.allSatisfy { $0.value(forHTTPHeaderField: TunnelTransport.ticketHeader) == "cmk_t" && $0.value(forHTTPHeaderField: "Authorization") == nil })
    }

    func testGoneReopensAndResendsOnce() async throws {
        let relay = FakeRelay()
        let client = RemoteClient(route: .relay(relay.tunnel(), computer: "mac1"), token: "t", transport: FakeTransport())
        relay.fail("/v1/d/call", [.status(410)])
        _ = try await client.hello()
        XCTAssertEqual(relay.opens, 2)
        XCTAssertEqual(relay.seen.count, 1)

        relay.fail("/v1/d/call", [.status(410), .status(410)])
        await XCTAssertThrowsAsync(try await client.hello(), RemoteError.notSent(TunnelTransport.channelLost))
        XCTAssertEqual(relay.transport.requests.filter { $0.url?.lastPathComponent == "call" }.count, 4)
    }

    func testBadGatewayMayHaveRun() async throws {
        let relay = FakeRelay()
        let client = RemoteClient(route: .relay(relay.tunnel(), computer: "mac1"), token: "t", transport: FakeTransport())
        for outcome in [FakeRelay.Outcome.status(502), .cut] {
            relay.fail("/v1/d/call", [outcome])
            await XCTAssertThrowsAsync(try await client.command("agents.prompt", workspace: "w"), RemoteError.unreachable(URLError(.networkConnectionLost).localizedDescription))
        }
        XCTAssertEqual(relay.transport.requests.filter { $0.url?.lastPathComponent == "call" }.count, 2)
    }

    func testReplyTooLargeForTheRelayIsAClearErrorNotMayHaveRun() async throws {
        let relay = FakeRelay()
        let client = RemoteClient(route: .relay(relay.tunnel(), computer: "mac1"), token: "t", transport: FakeTransport())
        let message = "This reply is too large for the relay path."
        relay.answer("/v1/rpc", #"{"error":{"code":"too_large","message":"\#(message)"}}"#, status: 413)
        await XCTAssertThrowsAsync(try await client.command("agents.sessions", workspace: "w"), RemoteError.server(code: "too_large", message: message))
        XCTAssertEqual(relay.transport.requests.filter { $0.url?.lastPathComponent == "call" }.count, 1)
    }

    func testReplyBoundToAnotherRequestIsRefused() async throws {
        let relay = FakeRelay()
        let client = RemoteClient(route: .relay(relay.tunnel(), computer: "mac1"), token: "t", transport: FakeTransport())
        _ = try await client.hello()
        relay.rebind { $0 % 2 == 0 ? $0 + 1 : $0 - 1 }
        await XCTAssertThrowsAsync(try await client.command("agents.prompt", workspace: "w"), RemoteError.badResponse)
        await XCTAssertThrowsAsync(try await client.command("agents.prompt", workspace: "w"), RemoteError.badResponse)
        relay.rebind { _ in 1 }
        await XCTAssertThrowsAsync(try await client.command("agents.prompt", workspace: "w"), RemoteError.badResponse)
        XCTAssertEqual(relay.seen.count, 4)
    }

    func testStreamWithADroppedChunkFails() async throws {
        let relay = FakeRelay()
        relay.dropChunk(1)
        let client = RemoteClient(route: .relay(relay.tunnel(), computer: "mac1"), token: "t", transport: FakeTransport())
        var items: [EventStreamItem] = []
        do {
            for try await item in try await client.events(after: nil) { items.append(item) }
            XCTFail("the stream ended without an error")
        } catch {
            XCTAssertEqual(error as? RemoteError, .badResponse)
        }
        XCTAssertEqual(items, [.alive, .ping])
    }

    func testRelayRefusingTheTicketIsRevoked() async throws {
        let relay = FakeRelay()
        let client = RemoteClient(route: .relay(relay.tunnel(), computer: "mac1"), token: "t", transport: FakeTransport())
        relay.fail("/v1/d/open", [.status(401)])
        await XCTAssertThrowsAsync(try await client.hello(), RemoteError.relayRevoked)
    }

    func testMacOfflineAndProxyErrorsCountAsNotSent() async throws {
        let relay = FakeRelay()
        let client = RemoteClient(route: .relay(relay.tunnel(), computer: "mac1"), token: "t", transport: FakeTransport())
        relay.fail("/v1/d/open", [.status(503)])
        await XCTAssertThrowsAsync(try await client.command("agents.prompt", workspace: "w"), RemoteError.notSent(TunnelTransport.macOffline))
        relay.fail("/v1/d/open", [.status(502)])
        await XCTAssertThrowsAsync(try await client.command("agents.prompt", workspace: "w"), RemoteError.notSent(TunnelTransport.channelLost))
        relay.fail("/v1/d/call", [.proxy(503)])
        await XCTAssertThrowsAsync(try await client.command("agents.prompt", workspace: "w"), RemoteError.notSent(TunnelTransport.relayUnreachable))
        relay.fail("/v1/d/call", [.proxy(200)])
        await XCTAssertThrowsAsync(try await client.command("agents.prompt", workspace: "w"), RemoteError.notSent(TunnelTransport.relayUnreachable))
        XCTAssertTrue(relay.seen.isEmpty)
    }

    func testRequestsPastTheLimitWaitForASlot() async throws {
        let relay = FakeRelay(endStreams: false)
        let tunnel = relay.tunnel(maxInFlight: 1)
        let client = RemoteClient(route: .relay(tunnel, computer: "mac1"), token: "t", transport: FakeTransport())
        let events = try await client.events(after: nil)
        let call = Task { try await client.command("agents.sessions", workspace: "w") }
        try await waitAsync { await tunnel.queued > 0 }
        XCTAssertEqual(relay.seen.count, 1)
        relay.closeStreams()
        for try await _ in events {}
        _ = try await call.value
        XCTAssertEqual(relay.seen.map(\.path), ["/v1/events", "/v1/rpc"])
    }

    func testStreamHeadWaitEndsOnDeadlineAndOnCancel() async throws {
        let relay = FakeRelay()
        relay.silence(2)
        let timed = relay.tunnel(maxInFlight: 1, headTimeout: .milliseconds(50))
        let client = RemoteClient(route: .relay(timed, computer: "mac1"), token: "t", transport: FakeTransport())
        await XCTAssertThrowsAsync(try await client.events(after: nil), RemoteError.unreachable(URLError(.timedOut).localizedDescription))
        try await waitAsync { await timed.busy == 0 }

        let held = relay.tunnel(maxInFlight: 1)
        let request = URLRequest(url: URL(string: "clubmate-relay://mac1/v1/events")!)
        let waiting = Task { try await held.stream(request) }
        try await waitAsync { relay.transport.requests.filter { $0.url?.lastPathComponent == "stream" }.count == 2 }
        waiting.cancel()
        do {
            _ = try await waiting.value
            XCTFail("the stream opened")
        } catch {
            XCTAssertTrue(error is CancellationError, "\(error)")
        }
        try await waitAsync { await held.busy == 0 }
    }

    func testInnerHeadDropsAcceptEncodingAndSkipsDeflateForMedia() throws {
        var request = URLRequest(url: URL(string: "clubmate-relay://mac1/v1/events?probe=1")!)
        request.setValue("gzip", forHTTPHeaderField: "Accept-Encoding")
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        XCTAssertEqual(TunnelTransport.head(request), TunnelRequestHead(method: "GET", path: "/v1/events?probe=1", header: ["Accept": "text/event-stream"]))

        let bodies: [(String, Bool)] = [
            (#"{"method":"report","params":{}}"#, false),
            (#"{"method":"command","params":{"id":"voice.audio"}}"#, false),
            (#"{"method":"command","params":{"id":"agents.image"}}"#, false),
            (#"{"method":"command","params":{"id":"agents.prompt","args":{"attachments":[{"name":"a"}]}}}"#, false),
            (#"{"method":"command","params":{"id":"agents.prompt","args":{"attachments":[]}}}"#, true),
            (#"{"method":"hello"}"#, true),
        ]
        for (body, deflate) in bodies {
            XCTAssertEqual(TunnelTransport.compressible(Data(body.utf8)), deflate, body)
        }
    }
}

final class FakeRelay: Sendable {
    enum Outcome: Sendable {
        case status(Int)
        case proxy(Int)
        case cut
    }

    struct State {
        var opens = 0
        var ticketHashes: [String?] = []
        var channels: [String: TunnelChannel] = [:]
        var seen: [TunnelRequestHead] = []
        var bodies: [Data] = []
        var held: [Held] = []
        var failures: [String: [Outcome]] = [:]
        var headers: [String: String] = [:]
        var streams: [AsyncThrowingStream<Data, any Error>.Continuation] = []
        var silent = 0
        var answers: [String: (String, Int)] = [:]
        var rebind: @Sendable (UInt32) -> UInt32 = { $0 }
        var dropChunk: Int?
    }

    struct Held {
        var stream: AsyncThrowingStream<Data, any Error>.Continuation
        var channel: TunnelChannel
        var re: UInt32
        var seq: UInt32
    }

    static let events = [": connected\n\n", "event: bus\ndata: {\"seq\":7,\"topic\":\"agents.changed\",\"payload\":{}}\n\n", ": ping\n\n"]

    let privateKey = Curve25519.KeyAgreement.PrivateKey()
    var key: Data { privateKey.rawRepresentation }
    let endStreams: Bool
    let state = Mutex(State())
    let transport: FakeTransport

    init(endStreams: Bool = true) {
        self.endStreams = endStreams
        let box = Mutex<FakeRelay?>(nil)
        transport = FakeTransport(reply: { try box.withLock { $0! }.reply($0) }, stream: { try box.withLock { $0! }.stream($0) }, headers: { _ in box.withLock { $0! }.state.withLock { $0.headers } })
        box.withLock { $0 = self }
    }

    func tunnel(maxInFlight: Int = TunnelTransport.defaultMaxInFlight, headTimeout: Duration = TunnelTransport.headTimeout) -> TunnelTransport {
        let pub = try! Curve25519.KeyAgreement.PrivateKey(rawRepresentation: key).publicKey.rawRepresentation
        return TunnelTransport(relay: URL(string: "https://relay.example")!, macKey: pub, ticket: "cmk_t", outer: transport, maxInFlight: maxInFlight, headTimeout: headTimeout)
    }

    var opens: Int { state.withLock { $0.opens } }
    var ticketHashes: [String?] { state.withLock { $0.ticketHashes } }
    var seen: [TunnelRequestHead] { state.withLock { $0.seen } }
    var bodies: [Data] { state.withLock { $0.bodies } }

    func fail(_ path: String, _ outcomes: [Outcome]) {
        state.withLock { $0.failures[path] = outcomes }
    }

    func answer(_ key: String, _ body: String, status: Int = 200) {
        state.withLock { $0.answers[key] = (body, status) }
    }

    func rebind(_ fn: @escaping @Sendable (UInt32) -> UInt32) {
        state.withLock { $0.rebind = fn }
    }

    func dropChunk(_ index: Int) {
        state.withLock { $0.dropChunk = index }
    }

    func silence(_ count: Int) {
        state.withLock { $0.silent = count }
    }

    func closeStreams() {
        let streams = state.withLock { s in
            defer { s.streams = []; s.held = [] }
            return s.streams
        }
        for stream in streams { stream.finish() }
    }

    func push(_ text: String) throws {
        let held = state.withLock { s in
            defer { for i in s.held.indices { s.held[i].seq += 1 } }
            return s.held
        }
        for h in held {
            let (plain, flags) = try TunnelPack.pack(head: nil, body: Data(text.utf8), dict: h.channel.dict, deflate: false)
            h.stream.yield(Self.frame(try h.channel.sealAnswer(type: .chunk, flags: flags, re: h.re, seq: h.seq, plain: plain)))
        }
    }

    static func frame(_ sealed: Data) -> Data {
        Data("data: \(sealed.base64EncodedString())\n".utf8)
    }

    private func failure(_ request: URLRequest) -> Outcome? {
        state.withLock { s in
            guard let path = request.url?.path(), var queue = s.failures[path], !queue.isEmpty else {
                s.headers = [TunnelTransport.relayHeader: "1"]
                return nil
            }
            let outcome = queue.removeFirst()
            s.failures[path] = queue
            s.headers = outcome.proxied ? [:] : [TunnelTransport.relayHeader: "1"]
            return outcome
        }
    }

    private func inner(_ request: URLRequest) throws -> (TunnelChannel, TunnelRequestHead, Data, UInt32) {
        let ch = try XCTUnwrap(state.withLock { $0.channels[request.value(forHTTPHeaderField: TunnelTransport.channelHeader) ?? ""] })
        let message = try XCTUnwrap(ch.readMessages(request.httpBody ?? Data()).first)
        let (head, body) = try TunnelPack.unpack(message, dict: ch.dict)
        let decoded = try JSONDecoder().decode(TunnelRequestHead.self, from: XCTUnwrap(head))
        state.withLock {
            $0.seen.append(decoded)
            $0.bodies.append(body)
        }
        return (ch, decoded, body, state.withLock { $0.rebind }(message.msg))
    }

    func reply(_ request: URLRequest) throws -> (Data, Int) {
        guard request.url?.host() == "relay.example" else { throw URLError(.cannotConnectToHost) }
        let outcome = failure(request)
        if let code = outcome?.code { return (Data(#"{"error":"x"}"#.utf8), code) }
        switch request.url?.path() {
        case "/healthz":
            return (Data("ok".utf8), 200)
        case "/v1/d/open":
            let id = state.withLock { s in
                s.opens += 1
                s.headers[TunnelTransport.channelHeader] = "ch_\(s.opens)"
                return "ch_\(s.opens)"
            }
            let (mac, msg2) = try TunnelTestResponder.respond(channel: id, msg1: request.httpBody ?? Data(), statics: [Curve25519.KeyAgreement.PrivateKey(rawRepresentation: key)], onTicketHash: { th in state.withLock { $0.ticketHashes.append(th) } })
            state.withLock { $0.channels[id] = mac }
            return (msg2, 200)
        case "/v1/d/call":
            let (ch, head, body, re) = try inner(request)
            let method = FakeTransport.rpc(body: body)
            let answer = state.withLock { $0.answers[head.path] ?? $0.answers[method] }
            let result = answer.map { Data($0.0.utf8) } ?? FakeTransport.result(method == "hello" ? helloJSON : method == "workspaces" ? workspacesJSON : #"{"ok":true}"#)
            let (plain, flags) = try TunnelPack.packReply(TunnelReplyHead(status: answer?.1 ?? 200, header: ["Content-Type": "application/json"]), body: result, dict: ch.dict, deflate: true)
            let sealed = try ch.sealAnswer(type: .reply, flags: flags, re: re, seq: 0, plain: plain)
            return (outcome == nil ? sealed : sealed.prefix(sealed.count / 2), 200)
        default:
            return (Data(), 404)
        }
    }

    func stream(_ request: URLRequest) throws -> (Int, AsyncThrowingStream<Data, any Error>) {
        if let code = failure(request)?.code { return (code, AsyncThrowingStream { $0.finish() }) }
        let (stream, continuation) = AsyncThrowingStream<Data, any Error>.makeStream()
        if state.withLock({ s in s.silent > 0 && { s.silent -= 1; return true }() }) {
            continuation.yield(Data(": ping\n".utf8))
            state.withLock { $0.streams.append(continuation) }
            return (200, stream)
        }
        let (ch, _, _, re) = try inner(request)
        let drop = state.withLock { $0.dropChunk }
        let (head, headFlags) = try TunnelPack.packReply(TunnelReplyHead(status: 200, header: ["Content-Type": "text/event-stream"]), body: Data(), dict: ch.dict, deflate: false)
        continuation.yield(Self.frame(try ch.sealAnswer(type: .reply, flags: headFlags, re: re, seq: 0, plain: head)))
        var seq: UInt32 = 1
        for (i, text) in Self.events.enumerated() {
            let (plain, flags) = try TunnelPack.pack(head: nil, body: Data(text.utf8), dict: ch.dict, deflate: true)
            let sealed = try ch.sealAnswer(type: .chunk, flags: flags, re: re, seq: seq, plain: plain)
            seq += 1
            if i != drop { continuation.yield(Self.frame(sealed)) }
            continuation.yield(Data("\n".utf8))
            if i == 0 { continuation.yield(Data(": ping\n".utf8)) }
        }
        if endStreams {
            let (plain, flags) = try TunnelPack.pack(head: nil, body: Data(), dict: ch.dict, deflate: false)
            continuation.yield(Self.frame(try ch.sealAnswer(type: .end, flags: flags, re: re, seq: seq, plain: plain)))
            continuation.finish()
        } else {
            state.withLock {
                $0.streams.append(continuation)
                $0.held.append(Held(stream: continuation, channel: ch, re: re, seq: seq))
            }
        }
        return (200, stream)
    }
}

extension FakeRelay.Outcome {
    var code: Int? {
        switch self {
        case .status(let code), .proxy(let code): code
        case .cut: nil
        }
    }

    var proxied: Bool {
        if case .proxy = self { return true }
        return false
    }
}

final class RoutedTransport: HTTPTransport {
    let relay: FakeRelay
    let tailnet: FakeTransport

    init(relay: FakeRelay, tailnet: FakeTransport) {
        self.relay = relay
        self.tailnet = tailnet
    }

    private func pick(_ request: URLRequest) -> FakeTransport {
        request.url?.host() == "relay.example" ? relay.transport : tailnet
    }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        try await pick(request).send(request)
    }

    func stream(_ request: URLRequest) async throws -> (HTTPURLResponse, AsyncThrowingStream<Data, any Error>) {
        try await pick(request).stream(request)
    }
}
