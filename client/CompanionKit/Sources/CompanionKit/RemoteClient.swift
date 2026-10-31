import Foundation

public enum RemoteError: Error, Equatable, Sendable {
    case unauthorized
    case pairingFailed(String)
    case server(code: String, message: String)
    case http(Int)
    case unreachable(String)
    case badResponse
    case hostNotAllowed

    public var message: String {
        switch self {
        case .unauthorized: "Access revoked — pair again."
        case .pairingFailed(let message): message.isEmpty ? "Pairing failed. Ask Clubmate for a new code." : message
        case .server(_, let message): message
        case .http(let status): "The Mac answered with HTTP \(status)."
        case .unreachable(let message): message
        case .badResponse: "The Mac sent a response this app doesn't understand."
        case .hostNotAllowed: "None of this Mac's addresses is on your Tailscale network."
        }
    }
}

public struct ComputerInfo: Codable, Sendable, Equatable {
    public let id: String
    public let name: String

    public init(id: String, name: String) {
        self.id = id
        self.name = name
    }
}

public struct Hello: Codable, Sendable, Equatable {
    public let computer: ComputerInfo
    public let version: String
    public let commands: [String]
    public let topics: [String]

    public init(computer: ComputerInfo, version: String, commands: [String], topics: [String]) {
        self.computer = computer
        self.version = version
        self.commands = commands
        self.topics = topics
    }
}

public struct Workspace: Codable, Sendable, Equatable, Identifiable, Hashable {
    public let id: String
    public let name: String
    public let roots: [String]

    public init(id: String, name: String, roots: [String]) {
        self.id = id
        self.name = name
        self.roots = roots
    }
}

public enum EventStreamItem: Sendable, Equatable {
    case alive
    case event(BusEvent)
}

public struct PairResult: Sendable, Equatable {
    public let token: String
    public let computer: Computer
}

public actor RemoteClient {
    public nonisolated let hosts: [String]
    public nonisolated let port: Int
    private let token: String?
    private let transport: any HTTPTransport
    private let policy: HostPolicy
    public private(set) var preferredHost: String?

    private static let rpcTimeout: TimeInterval = 20
    public static let probeTimeout: TimeInterval = 4
    private static let streamIdleTimeout: TimeInterval = 45
    private static let unsentErrors: Set<URLError.Code> = [
        .cannotFindHost, .cannotConnectToHost, .dnsLookupFailed, .notConnectedToInternet,
        .appTransportSecurityRequiresSecureConnection, .secureConnectionFailed, .badURL,
    ]

    public init(hosts: [String], port: Int, token: String?, transport: any HTTPTransport, policy: HostPolicy = .tailnet, preferredHost: String? = nil) {
        self.hosts = policy.filter(hosts)
        self.policy = policy
        self.port = port
        self.token = token
        self.transport = transport
        self.preferredHost = preferredHost
    }

    public static let mismatchMessage = "The Mac that answered isn't the one in the pairing link."

    public static func pair(_ link: PairingLink, device: DeviceIdentity, transport: any HTTPTransport, policy: HostPolicy = .tailnet, now: Date = .now) async throws -> PairResult {
        let client = RemoteClient(hosts: link.hosts, port: link.port, token: nil, transport: transport, policy: policy)
        let body = try JSONEncoder().encode(PairRequest(code: link.code, device: device))
        let (data, response, host) = try await client.exchange(path: "/v1/pair", method: "POST", body: body, probe: true)
        switch response.statusCode {
        case 200:
            guard let reply = try? JSONDecoder.companion.decode(PairReply.self, from: data) else { throw RemoteError.badResponse }
            guard reply.computer.id == link.computerID else { throw RemoteError.pairingFailed(mismatchMessage) }
            let hosts = [host] + policy.filter(link.hosts).filter { $0 != host }
            return PairResult(token: reply.token, computer: Computer(id: reply.computer.id, name: reply.computer.name, hosts: hosts, port: link.port, pairedAt: now))
        case 403:
            throw RemoteError.pairingFailed((try? JSONDecoder().decode(Envelope.self, from: data))?.error?.message ?? "")
        default:
            throw RemoteError.http(response.statusCode)
        }
    }

    public func rpc(_ method: String, params: JSONValue? = nil) async throws -> JSONValue {
        let body = try JSONEncoder().encode(RPCRequest(method: method, params: params))
        let (data, response, _) = try await exchange(path: "/v1/rpc", method: "POST", body: body, probe: method == "hello")
        if response.statusCode == 401 { throw RemoteError.unauthorized }
        guard let envelope = try? JSONDecoder().decode(Envelope.self, from: data) else {
            throw response.statusCode == 200 ? RemoteError.badResponse : RemoteError.http(response.statusCode)
        }
        if let error = envelope.error { throw RemoteError.server(code: error.code, message: error.message) }
        guard response.statusCode == 200 else { throw RemoteError.http(response.statusCode) }
        return envelope.result ?? .null
    }

    public func hello() async throws -> Hello {
        try await rpc("hello").decode(Hello.self)
    }

    public func workspaces() async throws -> [Workspace] {
        try await rpc("workspaces").decode([Workspace].self)
    }

    public func command(_ id: String, workspace: String?, args: JSONValue? = nil) async throws -> JSONValue {
        var params: [String: JSONValue] = ["id": .string(id)]
        if let workspace { params["workspace"] = .string(workspace) }
        if let args { params["args"] = args }
        return try await rpc("command", params: .object(params))
    }

    public func events(after seq: Int?) async throws -> AsyncThrowingStream<EventStreamItem, any Error> {
        var (response, chunks): (HTTPURLResponse?, AsyncThrowingStream<Data, any Error>?) = (nil, nil)
        var lastError: (any Error)?
        for host in orderedHosts() {
            var request = URLRequest(url: try url(host: host, path: "/v1/events"), timeoutInterval: Self.streamIdleTimeout)
            request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
            authorize(&request)
            if let seq { request.setValue(String(seq), forHTTPHeaderField: "Last-Event-ID") }
            do {
                (response, chunks) = try await transport.stream(request)
                preferredHost = host
                break
            } catch {
                lastError = error
                guard canFallBack(error) else { throw Self.mapped(error) }
            }
        }
        guard let response, let chunks else { throw Self.mapped(lastError ?? RemoteError.hostNotAllowed) }
        if response.statusCode == 401 { throw RemoteError.unauthorized }
        guard response.statusCode == 200 else { throw RemoteError.http(response.statusCode) }
        return AsyncThrowingStream { continuation in
            let task = Task {
                var parser = SSEParser()
                let decoder = JSONDecoder.companion
                var alive = false
                do {
                    for try await chunk in chunks {
                        if !alive, !chunk.isEmpty {
                            alive = true
                            continuation.yield(.alive)
                        }
                        for event in parser.feed(chunk) where event.event == "bus" {
                            if let bus = try? decoder.decode(BusEvent.self, from: Data(event.data.utf8)) {
                                continuation.yield(.event(bus))
                            }
                        }
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: Self.mapped(error))
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func exchange(path: String, method: String, body: Data, probe: Bool = false) async throws -> (Data, HTTPURLResponse, String) {
        var lastError: (any Error)?
        let hosts = orderedHosts()
        for (index, host) in hosts.enumerated() {
            let timeout = probe && index < hosts.count - 1 ? Self.probeTimeout : Self.rpcTimeout
            var request = URLRequest(url: try url(host: host, path: path), timeoutInterval: timeout)
            request.httpMethod = method
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            authorize(&request)
            do {
                let (data, response) = try await transport.send(request)
                preferredHost = host
                return (data, response, host)
            } catch {
                lastError = error
                guard canFallBack(error, probe: probe) else { throw Self.mapped(error) }
            }
        }
        throw Self.mapped(lastError ?? RemoteError.hostNotAllowed)
    }

    private func orderedHosts() -> [String] {
        guard let preferredHost, hosts.contains(preferredHost) else { return hosts }
        return [preferredHost] + hosts.filter { $0 != preferredHost }
    }

    private func canFallBack(_ error: any Error, probe: Bool = false) -> Bool {
        guard let code = (error as? URLError)?.code else { return false }
        return Self.unsentErrors.contains(code) || (code == .timedOut && (probe || preferredHost == nil))
    }

    private func authorize(_ request: inout URLRequest) {
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
    }

    private nonisolated func url(host: String, path: String) throws -> URL {
        var components = URLComponents()
        components.scheme = "http"
        components.host = host.contains(":") ? "[\(host)]" : host
        components.port = port
        components.path = path
        guard let url = components.url, url.host(percentEncoded: false) == host, url.port == port, url.user == nil else { throw RemoteError.hostNotAllowed }
        return url
    }

    private static func mapped(_ error: any Error) -> any Error {
        if error is RemoteError || error is CancellationError { return error }
        if let url = error as? URLError {
            if url.code == .cancelled { return CancellationError() }
            return RemoteError.unreachable(url.localizedDescription)
        }
        return error
    }
}

private struct PairRequest: Encodable {
    let code: String
    let device: DeviceIdentity
}

private struct PairReply: Decodable {
    let token: String
    let computer: ComputerInfo
}

private struct RPCRequest: Encodable {
    let method: String
    let params: JSONValue?
}

private struct Envelope: Decodable {
    struct Failure: Decodable {
        let code: String
        let message: String
    }

    let result: JSONValue?
    let error: Failure?
}
