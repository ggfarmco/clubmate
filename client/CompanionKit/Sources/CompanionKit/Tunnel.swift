import Foundation
import Synchronization

public actor TunnelTransport: HTTPTransport {
    public static let defaultMaxInFlight = 90
    public static let livenessTimeout: TimeInterval = 55
    public static let openTimeout: TimeInterval = 15
    public static let headTimeout: Duration = .seconds(livenessTimeout)
    public static let channelLifetime: Duration = .seconds(24 * 60 * 60)
    public static let relayHeader = "X-Clubmate-Relay"
    public static let ticketHeader = "X-Clubmate-Ticket"
    public static let channelHeader = "X-Clubmate-Channel"
    public static let relayUnreachable = "The Clubmate relay is unreachable."
    public static let macOffline = "The Mac is not connected to the Clubmate relay."
    public static let channelLost = "The Clubmate relay lost the channel to the Mac."
    static let skipDeflate: Set<String> = ["agents.image", "voice.audio"]

    public nonisolated let relay: URL
    public nonisolated let macKey: Data
    private let ticket: String
    private let outer: any HTTPTransport
    private let maxInFlight: Int
    private let headTimeout: Duration
    private let now: @Sendable () -> ContinuousClock.Instant
    private var current: (channel: TunnelChannel, opened: ContinuousClock.Instant)?
    private var opening: Task<TunnelChannel, any Error>?
    private var inFlight = 0
    private var waiters: [CheckedContinuation<Void, Never>] = []

    public init(relay: URL, macKey: Data, ticket: String, outer: any HTTPTransport = URLSessionTransport(), maxInFlight: Int = defaultMaxInFlight, headTimeout: Duration = headTimeout, now: @escaping @Sendable () -> ContinuousClock.Instant = { .now }) {
        self.relay = relay
        self.macKey = macKey
        self.ticket = ticket
        self.outer = outer
        self.maxInFlight = maxInFlight
        self.headTimeout = headTimeout
        self.now = now
    }

    public nonisolated var relayHost: String { relay.host() ?? relay.absoluteString }

    public func rekey() {
        current = nil
    }

    var queued: Int { waiters.count }
    var busy: Int { inFlight }

    public func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        await acquire()
        defer { release() }
        do {
            return try await call(request)
        } catch is Gone {
            do {
                return try await call(request)
            } catch is Gone {
                throw Self.unsent(Self.channelLost)
            }
        }
    }

    public func stream(_ request: URLRequest) async throws -> (HTTPURLResponse, AsyncThrowingStream<Data, any Error>) {
        await acquire()
        do {
            do {
                return try await openStream(request)
            } catch is Gone {
                do {
                    return try await openStream(request)
                } catch is Gone {
                    throw Self.unsent(Self.channelLost)
                }
            }
        } catch let failed as PumpFailed {
            throw failed.error
        } catch {
            release()
            throw error
        }
    }

    private func call(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let ch = try await channel()
        let (sent, re) = try outerRequest("/v1/d/call", inner: request, channel: ch, timeout: request.timeoutInterval)
        let (data, response) = try await outer.send(sent)
        try check(response, channel: ch)
        guard let url = request.url else { throw RemoteError.badResponse }
        guard let messages = try? ch.readMessages(data), let message = messages.first else { throw URLError(.networkConnectionLost) }
        guard messages.count == 1, message.type == .reply, message.answers(re, seq: 0) else { throw RemoteError.badResponse }
        let (head, body) = try Self.reply(message, channel: ch, url: url)
        return (body, head)
    }

    private func openStream(_ request: URLRequest) async throws -> (HTTPURLResponse, AsyncThrowingStream<Data, any Error>) {
        let ch = try await channel()
        var (outerRequest, re) = try outerRequest("/v1/d/stream", inner: request, channel: ch, timeout: Self.livenessTimeout)
        outerRequest.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        let (response, lines) = try await outer.stream(outerRequest)
        try check(response, channel: ch)
        guard let url = request.url else { throw RemoteError.badResponse }
        let (out, continuation) = AsyncThrowingStream<Data, any Error>.makeStream()
        let pump = PumpHandle()
        let deadline = Task { [headTimeout] in
            try? await Task.sleep(for: headTimeout)
            if !Task.isCancelled { pump.stop() }
        }
        defer { deadline.cancel() }
        do {
            let head: HTTPURLResponse = try await withTaskCancellationHandler {
                try await withCheckedThrowingContinuation { start in
                    let task = Task {
                        let end = await Self.pump(lines, channel: ch, re: re, url: url, start: start, out: continuation)
                        await self.streamEnded(ch)
                        continuation.finish(throwing: end)
                    }
                    continuation.onTermination = { _ in task.cancel() }
                    pump.set(task)
                    if Task.isCancelled { task.cancel() }
                }
            } onCancel: {
                pump.stop()
            }
            return (head, out)
        } catch {
            throw PumpFailed(error: Task.isCancelled ? CancellationError() : error)
        }
    }

    private func streamEnded(_ ch: TunnelChannel) {
        drop(ch)
        release()
    }

    private static func pump(_ lines: AsyncThrowingStream<Data, any Error>, channel ch: TunnelChannel, re: UInt32, url: URL, start: CheckedContinuation<HTTPURLResponse, any Error>, out: AsyncThrowingStream<Data, any Error>.Continuation) async -> (any Error)? {
        var started = false
        var seq: UInt32 = 0
        func begin(_ status: Int, _ header: [String: String]? = nil) {
            guard !started else { return }
            started = true
            start.resume(returning: HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: header)!)
        }
        var reader = TunnelMessageReader(channel: ch)
        do {
            for try await line in lines {
                let text = String(decoding: line, as: UTF8.self).trimmingCharacters(in: .newlines)
                if text.hasPrefix(":") {
                    if started { out.yield(Data(": ping\n".utf8)) }
                    continue
                }
                guard text.hasPrefix("data:") else { continue }
                guard let frame = Data(base64Encoded: text.dropFirst(5).trimmingCharacters(in: .whitespaces)) else { throw RemoteError.badResponse }
                let messages: [TunnelMessage]
                do { messages = try reader.feed(frame) } catch { throw RemoteError.badResponse }
                for m in messages {
                    guard m.answers(re, seq: seq) else { throw RemoteError.badResponse }
                    seq += 1
                    switch m.type {
                    case .reply:
                        let (head, _) = try reply(m, channel: ch, url: url)
                        begin(head.statusCode, head.allHeaderFields as? [String: String])
                        if head.statusCode != 200 { return nil }
                    case .chunk:
                        begin(200)
                        guard let unpacked = try? TunnelPack.unpack(m, dict: ch.dict) else { throw RemoteError.badResponse }
                        for piece in splitLines(unpacked.body) { out.yield(piece) }
                    case .end:
                        begin(200)
                        return nil
                    case .request:
                        throw RemoteError.badResponse
                    }
                }
            }
            guard started else { throw URLError(Task.isCancelled ? .timedOut : .networkConnectionLost) }
            return nil
        } catch {
            guard started else {
                started = true
                start.resume(throwing: error)
                return nil
            }
            return error
        }
    }

    static func reply(_ m: TunnelMessage, channel ch: TunnelChannel, url: URL) throws -> (HTTPURLResponse, Data) {
        guard let unpacked = try? TunnelPack.unpack(m, dict: ch.dict),
              let rawHead = unpacked.head,
              let head = try? JSONDecoder().decode(TunnelReplyHead.self, from: rawHead),
              let response = HTTPURLResponse(url: url, statusCode: head.status, httpVersion: "HTTP/1.1", headerFields: head.header)
        else { throw RemoteError.badResponse }
        return (response, unpacked.body)
    }

    static func splitLines(_ body: Data) -> [Data] {
        var out: [Data] = []
        var start = body.startIndex
        while let nl = body[start...].firstIndex(of: 0x0A) {
            out.append(Data(body[start...nl]))
            start = nl + 1
        }
        if start < body.endIndex { out.append(Data(body[start...])) }
        return out
    }

    private func channel() async throws -> TunnelChannel {
        if let current, now() - current.opened < Self.channelLifetime { return current.channel }
        current = nil
        if let opening { return try await opening.value }
        let task = Task { try await handshake() }
        opening = task
        defer { opening = nil }
        let ch = try await task.value
        current = (ch, now())
        return ch
    }

    private func drop(_ ch: TunnelChannel) {
        if current?.channel === ch { current = nil }
    }

    public enum OpenCheck: Sendable, Equatable {
        case open(channel: String)
        case relayUnreachable
        case macOffline
        case revoked
        case badKey
        case failed(String)
    }

    public func checkOpen() async -> OpenCheck {
        do {
            let (hs, msg1) = try TunnelHandshake.initiate(macKey: macKey, ticketHash: Tunnel.ticketHash(ticket))
            let (data, response) = try await outer.send(openRequest(msg1))
            guard response.value(forHTTPHeaderField: Self.relayHeader) == "1" else { return .relayUnreachable }
            switch response.statusCode {
            case 200: break
            case 401: return .revoked
            case 503: return .macOffline
            default: return .failed("HTTP \(response.statusCode)")
            }
            guard let id = response.value(forHTTPHeaderField: Self.channelHeader), !id.isEmpty else { return .failed("no channel id") }
            guard (try? hs.finish(channel: id, msg2: data)) != nil else { return .badKey }
            return .open(channel: id)
        } catch {
            return .failed(ConnectionAttempt.describe(error))
        }
    }

    private func openRequest(_ msg1: Data) -> URLRequest {
        var request = URLRequest(url: endpoint("/v1/d/open"), timeoutInterval: Self.openTimeout)
        request.httpMethod = "POST"
        request.httpBody = msg1
        request.setValue(ticket, forHTTPHeaderField: Self.ticketHeader)
        request.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
        return request
    }

    private func handshake() async throws -> TunnelChannel {
        let (hs, msg1) = try TunnelHandshake.initiate(macKey: macKey, ticketHash: Tunnel.ticketHash(ticket))
        let (data, response) = try await outer.send(openRequest(msg1))
        do {
            try Self.check(response)
        } catch is Gone {
            throw RemoteError.http(410)
        } catch let error as URLError {
            throw error.code == .cannotConnectToHost ? error : Self.unsent(Self.channelLost)
        }
        guard let id = response.value(forHTTPHeaderField: Self.channelHeader), !id.isEmpty else { throw RemoteError.badResponse }
        return try hs.finish(channel: id, msg2: data)
    }

    private func outerRequest(_ path: String, inner: URLRequest, channel ch: TunnelChannel, timeout: TimeInterval) throws -> (URLRequest, UInt32) {
        let head = Self.head(inner)
        let (plain, flags) = try TunnelPack.packRequest(head, body: inner.httpBody ?? Data(), dict: ch.dict)
        let wire: Data
        let re: UInt32
        do {
            (wire, re) = try ch.sealRequest(flags: flags, plain: plain)
        } catch TunnelError.exhausted {
            drop(ch)
            throw Gone()
        }
        var request = URLRequest(url: endpoint(path), timeoutInterval: timeout)
        request.httpMethod = "POST"
        request.httpBody = wire
        request.setValue(ticket, forHTTPHeaderField: Self.ticketHeader)
        request.setValue(ch.id, forHTTPHeaderField: Self.channelHeader)
        request.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
        return (request, re)
    }

    private func check(_ response: HTTPURLResponse, channel ch: TunnelChannel) throws {
        do {
            try Self.check(response)
        } catch let gone as Gone {
            drop(ch)
            throw gone
        }
    }

    static func check(_ response: HTTPURLResponse) throws {
        guard response.value(forHTTPHeaderField: relayHeader) == "1" else { throw unsent(relayUnreachable) }
        switch response.statusCode {
        case 200: return
        case 410: throw Gone()
        case 401: throw RemoteError.relayRevoked
        case 503: throw unsent(macOffline)
        case 502: throw URLError(.networkConnectionLost)
        default: throw RemoteError.http(response.statusCode)
        }
    }

    static func unsent(_ message: String) -> URLError {
        URLError(.cannotConnectToHost, userInfo: [NSLocalizedDescriptionKey: message])
    }

    static func head(_ request: URLRequest) -> TunnelRequestHead {
        var path = request.url?.path(percentEncoded: true) ?? "/"
        if path.isEmpty { path = "/" }
        if let query = request.url?.query(percentEncoded: true) { path += "?" + query }
        let header = (request.allHTTPHeaderFields ?? [:]).filter { $0.key.caseInsensitiveCompare("Accept-Encoding") != .orderedSame }
        return TunnelRequestHead(method: request.httpMethod ?? "GET", path: path, header: header.isEmpty ? nil : header, z: compressible(request.httpBody) ? nil : 0)
    }

    static func compressible(_ body: Data?) -> Bool {
        guard let body, let value = try? JSONDecoder().decode(JSONValue.self, from: body) else { return true }
        switch value["method"]?.stringValue {
        case "report":
            return false
        case "command":
            let params = value["params"]
            if let id = params?["id"]?.stringValue, skipDeflate.contains(id) { return false }
            if case .array(let items)? = params?["args"]?["attachments"], !items.isEmpty { return false }
            return true
        default:
            return true
        }
    }

    private nonisolated func endpoint(_ path: String) -> URL {
        relay.appending(path: path)
    }

    private func acquire() async {
        if inFlight < maxInFlight {
            inFlight += 1
            return
        }
        await withCheckedContinuation { waiters.append($0) }
    }

    private func release() {
        if waiters.isEmpty {
            inFlight -= 1
        } else {
            waiters.removeFirst().resume()
        }
    }
}

struct Gone: Error {}

final class PumpHandle: Sendable {
    private let state = Mutex<(task: Task<Void, Never>?, stopped: Bool)>((nil, false))

    func set(_ task: Task<Void, Never>) {
        if state.withLock({ $0.task = task; return $0.stopped }) { task.cancel() }
    }

    func stop() {
        state.withLock { $0.stopped = true; return $0.task }?.cancel()
    }
}

struct PumpFailed: Error {
    let error: any Error
}
