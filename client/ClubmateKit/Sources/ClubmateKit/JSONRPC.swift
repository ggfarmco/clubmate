import Darwin
import Foundation

public enum JSONValue: Codable, Sendable, Equatable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    public init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([JSONValue].self) {
            self = .array(value)
        } else {
            self = .object(try container.decode([String: JSONValue].self))
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .null: try container.encodeNil()
        case .bool(let value): try container.encode(value)
        case .number(let value): try container.encode(value)
        case .string(let value): try container.encode(value)
        case .array(let value): try container.encode(value)
        case .object(let value): try container.encode(value)
        }
    }
}

public struct RPCNotification: Sendable, Equatable {
    public let method: String
    public let params: JSONValue
}

public actor RPCConnection {
    public nonisolated let notifications: AsyncStream<RPCNotification>

    private let fd: Int32
    private let notify: AsyncStream<RPCNotification>.Continuation
    private var nextID = 1
    private var pending: [Int: CheckedContinuation<Data, any Error>] = [:]
    private var closed = false

    private init(fd: Int32) {
        self.fd = fd
        (notifications, notify) = AsyncStream.makeStream(of: RPCNotification.self)
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

    public func call<P: Encodable & Sendable, R: Decodable & Sendable>(_ method: String, _ params: P) async throws -> R {
        guard !closed else { throw ClubmateError.notRunning }
        let id = nextID
        nextID += 1
        var line = try JSONEncoder().encode(Request(id: id, method: method, params: params))
        line.append(0x0A)
        let response: Data = try await withCheckedThrowingContinuation { continuation in
            pending[id] = continuation
            do {
                try write(line)
            } catch {
                pending.removeValue(forKey: id)?.resume(throwing: error)
            }
        }
        do {
            return try JSONDecoder.clubmate.decode(Response<R>.self, from: response).result
        } catch {
            throw ClubmateError.protocolError("\(method): \(error)")
        }
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
            guard header.id == nil else { return }
            notify.yield(RPCNotification(method: method, params: header.params ?? .null))
            return
        }
        guard let id = header.id, let continuation = pending.removeValue(forKey: id) else { return }
        if let error = header.error {
            continuation.resume(throwing: ClubmateError.rpc(code: error.code, message: error.message))
        } else {
            continuation.resume(returning: line)
        }
    }

    private func finish() {
        closed = true
        let waiting = pending
        pending = [:]
        for continuation in waiting.values {
            continuation.resume(throwing: ClubmateError.notRunning)
        }
        notify.finish()
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
    var buffer = Data()
    var chunk = [UInt8](repeating: 0, count: 64 * 1024)
    while true {
        let n = chunk.withUnsafeMutableBytes { Darwin.read(fd, $0.baseAddress, $0.count) }
        if n < 0, errno == EINTR { continue }
        if n <= 0 { break }
        buffer.append(contentsOf: chunk[0..<n])
        while let newline = buffer.firstIndex(of: 0x0A) {
            let line = buffer[buffer.startIndex..<newline]
            buffer.removeSubrange(buffer.startIndex...newline)
            if !line.isEmpty { sink.yield(Data(line)) }
        }
    }
    sink.finish()
}

private struct Request<P: Encodable>: Encodable {
    let jsonrpc = "2.0"
    let id: Int
    let method: String
    let params: P
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
