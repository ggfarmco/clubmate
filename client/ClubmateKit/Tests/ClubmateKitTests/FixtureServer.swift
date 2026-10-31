import Foundation
import Network
import Synchronization

struct FixtureResponse: Sendable {
    var status: Int
    var headers: [String: String]
    var body: String
    var delay: Duration
    var responds: Bool

    init(status: Int = 200, headers: [String: String] = [:], body: String = "", delay: Duration = .zero) {
        self.status = status
        self.headers = headers
        self.body = body
        self.delay = delay
        self.responds = true
    }

    static let never: FixtureResponse = {
        var response = FixtureResponse()
        response.responds = false
        return response
    }()
}

final class FixtureServer: Sendable {
    typealias Handler = @Sendable (_ path: String, _ headers: [String: String]) -> FixtureResponse

    let port: UInt16
    private let listener: NWListener
    private let pool: FixturePool

    convenience init(routes: [String: FixtureResponse]) async throws {
        try await self.init { path, _ in routes[path] ?? FixtureResponse(status: 404, body: "not found") }
    }

    init(handler: @escaping Handler) async throws {
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        let listener = try NWListener(using: parameters)
        let pool = FixturePool(handler: handler)
        listener.newConnectionHandler = { pool.serve($0) }
        port = try await withCheckedThrowingContinuation { continuation in
            listener.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    listener.stateUpdateHandler = nil
                    continuation.resume(returning: listener.port?.rawValue ?? 0)
                case .failed(let error):
                    listener.stateUpdateHandler = nil
                    continuation.resume(throwing: error)
                default:
                    break
                }
            }
            listener.start(queue: pool.queue)
        }
        self.listener = listener
        self.pool = pool
    }

    func url(_ path: String) -> URL {
        URL(string: "http://127.0.0.1:\(port)\(path)")!
    }

    func stop() {
        listener.cancel()
        pool.cancelAll()
    }
}

private final class FixturePool: Sendable {
    let queue = DispatchQueue(label: "FixtureServer")
    private let handler: FixtureServer.Handler
    private let connections = Mutex<[ObjectIdentifier: NWConnection]>([:])

    init(handler: @escaping FixtureServer.Handler) {
        self.handler = handler
    }

    func cancelAll() {
        let open = connections.withLock { open in
            defer { open.removeAll() }
            return Array(open.values)
        }
        open.forEach { $0.cancel() }
    }

    func serve(_ connection: NWConnection) {
        let id = ObjectIdentifier(connection)
        connections.withLock { $0[id] = connection }
        connection.stateUpdateHandler = { [self] state in
            switch state {
            case .failed, .cancelled:
                connections.withLock { _ = $0.removeValue(forKey: id) }
            default:
                break
            }
        }
        connection.start(queue: queue)
        receive(connection, buffer: Data())
    }

    private func receive(_ connection: NWConnection, buffer: Data) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 65536) { [self] data, _, complete, error in
            var buffer = buffer
            if let data { buffer.append(data) }
            if let end = buffer.range(of: Data("\r\n\r\n".utf8)) {
                respond(connection, head: String(decoding: buffer[..<end.lowerBound], as: UTF8.self))
            } else if complete || error != nil {
                connection.cancel()
            } else {
                receive(connection, buffer: buffer)
            }
        }
    }

    private func respond(_ connection: NWConnection, head: String) {
        let lines = head.components(separatedBy: "\r\n")
        let target = lines.first?.split(separator: " ").dropFirst().first.map(String.init) ?? "/"
        let path = String(target.split(separator: "?", maxSplits: 1, omittingEmptySubsequences: false).first ?? "/")
        var headers: [String: String] = [:]
        for line in lines.dropFirst() {
            guard let colon = line.firstIndex(of: ":") else { continue }
            headers[line[..<colon].lowercased()] = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
        }
        let response = handler(path, headers)
        guard response.responds else { return }
        let body = Data(response.body.utf8)
        var fields = ["Content-Type": "text/html; charset=utf-8"].merging(response.headers) { $1 }
        fields["Content-Length"] = String(body.count)
        fields["Connection"] = "close"
        let text = "HTTP/1.1 \(response.status) Fixture\r\n" + fields.map { "\($0.key): \($0.value)\r\n" }.joined() + "\r\n"
        let payload = Data(text.utf8) + body
        queue.asyncAfter(deadline: .now() + .nanoseconds(Int(response.delay / .nanoseconds(1)))) {
            connection.send(content: payload, completion: .contentProcessed { _ in connection.cancel() })
        }
    }
}
