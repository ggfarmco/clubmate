import Darwin
import Foundation

@_exported import ClubmateProtocol

public struct RPCNotification: Sendable, Equatable {
    public let method: String
    public let params: JSONValue

    public init(method: String, params: JSONValue) {
        self.method = method
        self.params = params
    }
}

public struct RPCRequest: Sendable, Equatable {
    public let id: Int
    public let method: String
    public let params: JSONValue
}

public actor RPCConnection {
    public nonisolated let notifications: AsyncStream<RPCNotification>
    public nonisolated let requests: AsyncStream<RPCRequest>

    private let fd: Int32
    private let notify: AsyncStream<RPCNotification>.Continuation
    private let request: AsyncStream<RPCRequest>.Continuation
    private var nextID = 1
    private var pending: [Int: CheckedContinuation<Data, any Error>] = [:]
    private var timers: [Int: Task<Void, Never>] = [:]
    private var closed = false

    private init(fd: Int32) {
        self.fd = fd
        (notifications, notify) = AsyncStream.makeStream(of: RPCNotification.self)
        (requests, request) = AsyncStream.makeStream(of: RPCRequest.self)
    }

    public static func connect(paths: ClubmatePaths) async throws -> RPCConnection {
        try Security.checkRuntime(paths)
        let fd = try openSocket(paths.coreSocket.fileSystemPath)
        do {
            try Security.verifyPeer(fd: fd)
        } catch {
            Darwin.close(fd)
            throw error
        }
        let connection = RPCConnection(fd: fd)
        await connection.start()
        return connection
    }

    public func call<P: Encodable & Sendable, R: Decodable & Sendable>(_ method: String, _ params: P, timeout: Duration = .seconds(10)) async throws -> R {
        guard !closed else { throw ClubmateError.notRunning }
        let id = nextID
        nextID += 1
        var line = try JSONEncoder().encode(Request(id: id, method: method, params: params))
        line.append(0x0A)
        let response: Data = try await withCheckedThrowingContinuation { continuation in
            pending[id] = continuation
            timers[id] = Task {
                guard (try? await Task.sleep(for: timeout)) != nil else { return }
                expire(id)
            }
            do {
                try write(line)
            } catch {
                resolve(id)?.resume(throwing: error)
            }
        }
        do {
            return try JSONDecoder.clubmate.decode(Response<R>.self, from: response).result
        } catch {
            throw ClubmateError.protocolError("\(method): \(error)")
        }
    }

    public func reply<R: Encodable & Sendable>(id: Int, result: R) throws {
        guard !closed else { throw ClubmateError.notRunning }
        var line = try JSONEncoder().encode(Reply(id: id, result: result))
        line.append(0x0A)
        try write(line)
    }

    public func replyError(id: Int, code: Int, message: String) throws {
        guard !closed else { throw ClubmateError.notRunning }
        var line = try JSONEncoder().encode(ErrorReply(id: id, error: .init(code: code, message: message)))
        line.append(0x0A)
        try write(line)
    }

    public func close() {
        guard !closed else { return }
        closed = true
        shutdown(fd, SHUT_RDWR)
    }

    private func start() {
        let (lines, sink) = AsyncStream.makeStream(of: Data.self)
        let thread = Thread { [fd] in readLines(fd: fd, into: sink) }
        thread.name = "clubmate.rpc.reader"
        thread.start()
        Task {
            for await line in lines {
                receive(line)
            }
            finish()
        }
    }

    private func receive(_ line: Data) {
        guard let header = try? JSONDecoder.clubmate.decode(Header.self, from: line) else { return }
        if let method = header.method {
            if let id = header.id {
                request.yield(RPCRequest(id: id, method: method, params: header.params ?? .null))
            } else {
                notify.yield(RPCNotification(method: method, params: header.params ?? .null))
            }
            return
        }
        guard let id = header.id, let continuation = resolve(id) else { return }
        if let error = header.error {
            continuation.resume(throwing: ClubmateError.rpc(code: error.code, message: error.message))
        } else {
            continuation.resume(returning: line)
        }
    }

    private func resolve(_ id: Int) -> CheckedContinuation<Data, any Error>? {
        timers.removeValue(forKey: id)?.cancel()
        return pending.removeValue(forKey: id)
    }

    private func expire(_ id: Int) {
        resolve(id)?.resume(throwing: ClubmateError.timeout)
    }

    private func finish() {
        closed = true
        let waiting = pending
        pending = [:]
        timers.values.forEach { $0.cancel() }
        timers = [:]
        for continuation in waiting.values {
            continuation.resume(throwing: ClubmateError.notRunning)
        }
        notify.finish()
        request.finish()
        Darwin.close(fd)
    }

    private func write(_ data: Data) throws {
        try data.withUnsafeBytes { raw in
            guard let base = raw.baseAddress else { return }
            var offset = 0
            while offset < raw.count {
                let n = Darwin.write(fd, base + offset, raw.count - offset)
                if n < 0 {
                    if errno == EINTR { continue }
                    throw ClubmateError.notRunning
                }
                offset += n
            }
        }
    }

    private static func openSocket(_ path: String) throws -> Int32 {
        var addr = sockaddr_un()
        let capacity = MemoryLayout.size(ofValue: addr.sun_path)
        let bytes = Array(path.utf8)
        guard bytes.count < capacity else { throw ClubmateError.protocolError("socket path too long: \(path)") }
        withUnsafeMutableBytes(of: &addr.sun_path) { buf in
            buf.copyBytes(from: bytes)
        }
        addr.sun_family = sa_family_t(AF_UNIX)
        addr.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw ClubmateError.protocolError("socket: \(String(cString: strerror(errno)))") }
        var on: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &on, socklen_t(MemoryLayout<Int32>.size))
        let rc = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard rc == 0 else {
            let code = errno
            Darwin.close(fd)
            if code == ENOENT || code == ECONNREFUSED { throw ClubmateError.notRunning }
            throw ClubmateError.protocolError("connect \(path): \(String(cString: strerror(code)))")
        }
        return fd
    }
}

private func readLines(fd: Int32, into sink: AsyncStream<Data>.Continuation) {
    var lines = LineBuffer()
    var chunk = [UInt8](repeating: 0, count: 64 * 1024)
    while true {
        let n = chunk.withUnsafeMutableBytes { Darwin.read(fd, $0.baseAddress, $0.count) }
        if n < 0, errno == EINTR { continue }
        if n <= 0 { break }
        chunk.withUnsafeBytes { lines.feed(UnsafeRawBufferPointer(rebasing: $0[0..<n])) { sink.yield($0) } }
    }
    sink.finish()
}

public struct LineBuffer: Sendable {
    private var pending = Data()

    public init() {}

    public mutating func feed(_ bytes: UnsafeRawBufferPointer, _ emit: (Data) -> Void) {
        guard let base = bytes.baseAddress else { return }
        var start = 0
        while start < bytes.count, let found = memchr(base + start, 0x0A, bytes.count - start) {
            let end = base.distance(to: UnsafeRawPointer(found))
            pending.append(contentsOf: UnsafeRawBufferPointer(rebasing: bytes[start..<end]))
            if !pending.isEmpty {
                emit(pending)
                pending = Data()
            }
            start = end + 1
        }
        pending.append(contentsOf: UnsafeRawBufferPointer(rebasing: bytes[start...]))
    }
}

private struct Request<P: Encodable>: Encodable {
    let jsonrpc = "2.0"
    let id: Int
    let method: String
    let params: P
}

private struct Reply<R: Encodable>: Encodable {
    let jsonrpc = "2.0"
    let id: Int
    let result: R
}

private struct ErrorReply: Encodable {
    struct Body: Encodable {
        let code: Int
        let message: String
    }

    let jsonrpc = "2.0"
    let id: Int
    let error: Body
}

private struct Response<R: Decodable>: Decodable {
    let result: R
}

private struct Header: Decodable {
    struct ErrorBody: Decodable {
        let code: Int
        let message: String
    }

    let id: Int?
    let method: String?
    let params: JSONValue?
    let error: ErrorBody?
}

extension JSONValue {
    func decode<T: Decodable>(_ type: T.Type) throws -> T {
        try decode(type, using: .clubmate)
    }
}

extension JSONDecoder {
    static var clubmate: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let text = try container.decode(String.self)
            if let date = try? Date(text, strategy: .iso8601.year().month().day().time(includingFractionalSeconds: true).timeZone(separator: .omitted)) {
                return date
            }
            if let date = try? Date(text, strategy: .iso8601) {
                return date
            }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "invalid date: \(text)")
        }
        return decoder
    }
}
