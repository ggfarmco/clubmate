import Foundation

public enum RemoteError: Error, Equatable, Sendable {
    case unauthorized
    case pairingFailed(String)
    case server(code: String, message: String)
    case http(Int)
    case unreachable(String)
    case notSent(String)
    case badResponse
    case hostNotAllowed
    case relayRevoked

    public static let relayRevokedMessage = "This phone's relay access was revoked: pair again."

    public var message: String {
        switch self {
        case .unauthorized: "Access revoked — pair again."
        case .pairingFailed(let message): message.isEmpty ? "Pairing failed. Ask Clubmate for a new code." : message
        case .server(_, let message): message
        case .http(let status): "The Mac answered with HTTP \(status)."
        case .unreachable(let message), .notSent(let message): message
        case .badResponse: "The Mac sent a response this app doesn't understand."
        case .hostNotAllowed: "None of this Mac's addresses is on your Tailscale network."
        case .relayRevoked: RemoteError.relayRevokedMessage
        }
    }
}

public struct ComputerInfo: Codable, Sendable, Equatable {
    public let id: String
    public let name: String
    public let colour: String?

    public init(id: String, name: String, colour: String? = nil) {
        self.id = id
        self.name = name
        self.colour = colour
    }
}

public struct Hello: Codable, Sendable, Equatable {
    public let computer: ComputerInfo
    public let version: String
    public let commands: [String]
    public let topics: [String]
    public let ui: Bool
    public let requests: Bool
    public let reports: Bool
    public let tunnel: RelayRoute?

    public init(computer: ComputerInfo, version: String, commands: [String], topics: [String], ui: Bool = false, requests: Bool = false, reports: Bool = false, tunnel: RelayRoute? = nil) {
        self.computer = computer
        self.version = version
        self.commands = commands
        self.topics = topics
        self.ui = ui
        self.requests = requests
        self.reports = reports
        self.tunnel = tunnel
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        computer = try c.decode(ComputerInfo.self, forKey: .computer)
        version = try c.decode(String.self, forKey: .version)
        commands = try c.decode([String].self, forKey: .commands)
        topics = try c.decode([String].self, forKey: .topics)
        ui = try c.decodeIfPresent(Bool.self, forKey: .ui) ?? false
        requests = try c.decodeIfPresent(Bool.self, forKey: .requests) ?? false
        reports = try c.decodeIfPresent(Bool.self, forKey: .reports) ?? false
        tunnel = try? c.decodeIfPresent(RelayRoute.self, forKey: .tunnel)
    }
}

public struct Workspace: Codable, Sendable, Equatable, Identifiable, Hashable {
    public struct Folder: Codable, Sendable, Equatable, Hashable {
        public let path: String
        public var seen: Bool?

        public init(path: String, seen: Bool? = nil) {
            self.path = path
            self.seen = seen
        }
    }

    public let id: String
    public let name: String
    public let folders: [Folder]?
    public let home: Bool?

    public init(id: String, name: String, folders: [Folder]? = nil, home: Bool? = nil) {
        self.id = id
        self.name = name
        self.folders = folders
        self.home = home
    }

    public static func homeFirst(_ workspaces: [Workspace]) -> [Workspace] {
        workspaces.filter { $0.home == true } + workspaces.filter { $0.home != true }
    }

    public var foldersLine: String? {
        guard let count = folders?.count, count > 1 else { return nil }
        return "\(count) folders"
    }

    public var newFolders: String? {
        guard let count = folders?.count(where: { $0.seen == false }), count > 0 else { return nil }
        return "+\(count)"
    }
}

public enum EventStreamItem: Sendable, Equatable {
    case alive
    case ping
    case event(BusEvent)
    case stream(String)
    case ui(SlotView)
    case reset(Int)
}

public struct PairResult: Sendable, Equatable {
    public let token: String
    public let relayTicket: String?
    public let computer: Computer
}

public actor RemoteClient {
    public enum Route: Sendable {
        case tailnet(hosts: [String], port: Int)
        case relay(TunnelTransport, computer: String)
    }

    public nonisolated let route: Route
    public nonisolated let hosts: [String]
    public nonisolated let port: Int
    private let token: String?
    private let transport: any HTTPTransport
    private let policy: HostPolicy
    public private(set) var preferredHost: String?

    private static let rpcTimeout: TimeInterval = 20
    private static let archiveTimeout: TimeInterval = 120
    private static let reportTimeout: TimeInterval = 180
    public static let probeTimeout: TimeInterval = 4
    private static let streamIdleTimeout: TimeInterval = 45
    private static let unsentErrors: Set<URLError.Code> = [
        .cannotFindHost, .cannotConnectToHost, .dnsLookupFailed, .notConnectedToInternet,
        .appTransportSecurityRequiresSecureConnection, .secureConnectionFailed, .badURL,
    ]
    private static let lostErrors: Set<POSIXErrorCode> = [.ECONNRESET, .ECONNABORTED, .ENOTCONN, .ENETDOWN, .ENETUNREACH, .EHOSTUNREACH, .EPIPE]

    public init(route: Route, token: String?, transport: any HTTPTransport, policy: HostPolicy = .tailnet, preferredHost: String? = nil) {
        self.route = route
        switch route {
        case .tailnet(let hosts, let port):
            self.hosts = policy.filter(hosts)
            self.port = port
            self.transport = transport
        case .relay(let tunnel, let computer):
            hosts = [computer]
            port = 0
            self.transport = tunnel
        }
        self.policy = policy
        self.token = token
        self.preferredHost = preferredHost
    }

    public init(hosts: [String], port: Int, token: String?, transport: any HTTPTransport, policy: HostPolicy = .tailnet, preferredHost: String? = nil) {
        self.init(route: .tailnet(hosts: hosts, port: port), token: token, transport: transport, policy: policy, preferredHost: preferredHost)
    }

    public static let mismatchMessage = "The Mac that answered isn't the one in the pairing link."
    public static let mayHavePairedMessage = "The Mac didn't answer over Tailscale in time and may have paired this phone. Show a new pairing code on the Mac and scan it again."
    public static let noRelayTicketMessage = "The Mac paired without relay access. Update Clubmate on the Mac, then pair again."
    public static let pairingTicketMessage = "The relay refused this pairing code. Show a new pairing code on the Mac and scan it again."

    public static func pair(_ link: PairingLink, device: DeviceIdentity, transport: any HTTPTransport, policy: HostPolicy = .tailnet, now: Date = .now) async throws -> PairResult {
        let body = try JSONEncoder().encode(PairRequest(code: link.code, device: PairDevice(id: device.id, name: device.name, model: device.model)))
        let relay = link.relay.flatMap { route in
            route.macKey.flatMap { key in link.pairingTicket.map { (route, key, $0) } }
        }
        var answer: (Data, HTTPURLResponse, String?)?
        var mayHavePaired = false
        let tailnet = policy.filter(link.hosts)
        if !tailnet.isEmpty || relay == nil {
            let client = RemoteClient(hosts: tailnet, port: link.port, token: nil, transport: transport, policy: policy)
            do {
                let (data, response, host) = try await client.exchange(path: "/v1/pair", method: "POST", body: body, probe: true, quick: relay != nil)
                answer = (data, response, host)
            } catch let error as RemoteError where relay != nil {
                switch error {
                case .notSent, .hostNotAllowed: break
                case .unreachable: mayHavePaired = true
                default: throw error
                }
            }
        }
        if answer == nil, case let (route, key, ticket)? = relay {
            guard policy.allowsRelay(route.url) else { throw RemoteError.hostNotAllowed }
            let tunnel = TunnelTransport(relay: route.url, macKey: key, ticket: ticket, outer: transport)
            let client = RemoteClient(route: .relay(tunnel, computer: link.computerID), token: nil, transport: transport, policy: policy)
            do {
                let (data, response, _) = try await client.exchange(path: "/v1/pair", method: "POST", body: body, probe: true)
                answer = (data, response, nil)
            } catch RemoteError.relayRevoked {
                throw RemoteError.pairingFailed(mayHavePaired ? mayHavePairedMessage : pairingTicketMessage)
            }
        }
        guard case let (data, response, host)? = answer else { throw RemoteError.hostNotAllowed }
        switch response.statusCode {
        case 200:
            guard let reply = try? JSONDecoder.clubmate.decode(PairReply.self, from: data) else { throw RemoteError.badResponse }
            guard reply.computer.id == link.computerID else { throw RemoteError.pairingFailed(mismatchMessage) }
            if host == nil, reply.relayTicket?.isEmpty ?? true { throw RemoteError.pairingFailed(noRelayTicketMessage) }
            let hosts = host.map { first in [first] + tailnet.filter { $0 != first } } ?? tailnet
            let computer = Computer(id: reply.computer.id, name: reply.computer.name, hosts: hosts, port: link.port, pairedAt: now, colour: reply.computer.colour, relay: link.relay)
            return PairResult(token: reply.token, relayTicket: reply.relayTicket, computer: computer)
        case 403 where host == nil && mayHavePaired:
            throw RemoteError.pairingFailed(mayHavePairedMessage)
        case 403:
            throw RemoteError.pairingFailed((try? JSONDecoder().decode(Envelope<JSONValue>.self, from: data))?.error?.message ?? "")
        default:
            throw RemoteError.http(response.statusCode)
        }
    }

    public func rpc(_ method: String, params: JSONValue? = nil) async throws -> JSONValue {
        try await rpc(method, params: params, as: JSONValue.self) ?? .null
    }

    public func rpc<T: Decodable & Sendable>(_ method: String, params: JSONValue? = nil, as type: T.Type, trace: HostTrace? = nil, quick: Bool = false) async throws -> T? {
        let body = try JSONEncoder().encode(RPCRequest(method: method, params: params))
        let slow = params?["id"]?.stringValue == TaskCommand.archive
        let timeout = method == "report" ? Self.reportTimeout : slow ? Self.archiveTimeout : Self.rpcTimeout
        let (data, response, _) = try await exchange(path: "/v1/rpc", method: "POST", body: body, probe: method == "hello", quick: quick, timeout: timeout, trace: trace)
        if response.statusCode == 401 { throw RemoteError.unauthorized }
        guard let envelope = try? JSONDecoder.clubmate.decode(Envelope<T>.self, from: data) else {
            if let failure = (try? JSONDecoder().decode(Envelope<JSONValue>.self, from: data))?.error {
                throw RemoteError.server(code: failure.code, message: failure.message)
            }
            throw response.statusCode == 200 ? RemoteError.badResponse : RemoteError.http(response.statusCode)
        }
        if let error = envelope.error { throw RemoteError.server(code: error.code, message: error.message) }
        guard response.statusCode == 200 else { throw RemoteError.http(response.statusCode) }
        return envelope.result
    }

    public func hello(trace: HostTrace? = nil, quick: Bool = false) async throws -> Hello {
        guard let hello = try await rpc("hello", as: Hello.self, trace: trace, quick: quick) else { throw RemoteError.badResponse }
        return hello
    }

    public func workspaces(trace: HostTrace? = nil) async throws -> [Workspace] {
        try await rpc("workspaces", as: [Workspace].self, trace: trace) ?? []
    }

    public func macs(_ macs: [MacTag]) async throws {
        _ = try await rpc("macs", params: .object(["macs": try JSONValue.encoding(macs)]))
    }

    public func pushToken(_ token: PushToken) async throws -> Bool {
        try await rpc("pushToken", params: .object(["token": .string(token.token), "environment": .string(token.environment)]))["push"]?.boolValue ?? false
    }

    public func setIdentityColour(_ colour: String) async throws -> ComputerInfo {
        try await rpc("setIdentityColour", params: .object(["colour": .string(colour)])).decode(IdentityReply.self).computer
    }

    public func command(_ id: String, workspace: String?, args: JSONValue? = nil, requestId: String? = nil) async throws -> JSONValue {
        try await rpc("command", params: Self.commandParams(id, workspace: workspace, args: args, requestId: requestId))
    }

    public func command<T: Decodable & Sendable>(_ id: String, workspace: String?, args: JSONValue? = nil, as type: T.Type) async throws -> T {
        guard let value = try await rpc("command", params: Self.commandParams(id, workspace: workspace, args: args), as: type) else { throw RemoteError.badResponse }
        return value
    }

    public func report(text: String, mac: Bool, files: [(name: String, data: Data)], requestId: String, device: DeviceIdentity?, appVersion: String) async throws -> String {
        var params: [String: JSONValue] = [
            "requestId": .string(requestId),
            "text": .string(text),
            "appVersion": .string(appVersion),
            "mac": .bool(mac),
            "files": .array(files.map { .object(["name": .string($0.name), "data": .string($0.data.base64EncodedString())]) }),
        ]
        if let device {
            params["device"] = .string(device.name)
            params["model"] = .string(device.model)
        }
        guard let id = try await rpc("report", params: .object(params))["id"]?.stringValue else { throw RemoteError.badResponse }
        return id
    }

    public func uiSections() async throws -> [SectionInfo] {
        try await rpc("uiSections", as: [SectionInfo].self) ?? []
    }

    public func uiGet(workspace: String, slots: [String]) async throws -> [SlotView] {
        let params = JSONValue.object(["workspace": .string(workspace), "slots": .array(slots.map(JSONValue.string))])
        return (try await rpc("uiGet", params: params, as: [UIFrame].self) ?? []).map(\.view)
    }

    public func uiWatch(stream: String, workspace: String, slots: [String]) async throws {
        _ = try await rpc("uiWatch", params: .object(["stream": .string(stream), "workspace": .string(workspace), "slots": .array(slots.map(JSONValue.string))]))
    }

    public func uiEvent(workspace: String, slot: String, nodeId: String, action: String, value: JSONValue?) async throws -> JSONValue {
        try await rpc("uiEvent", params: .object(["workspace": .string(workspace), "slot": .string(slot), "nodeId": .string(nodeId), "action": .string(action), "value": value ?? .null, "requestId": .string(UUID().uuidString)]))
    }

    private static func commandParams(_ id: String, workspace: String?, args: JSONValue?, requestId: String? = nil) -> JSONValue {
        var params: [String: JSONValue] = ["id": .string(id)]
        if let workspace { params["workspace"] = .string(workspace) }
        if let args { params["args"] = args }
        if let requestId { params["requestId"] = .string(requestId) }
        return .object(params)
    }

    public func events(after seq: Int?, probe: Bool = false, trace: HostTrace? = nil) async throws -> AsyncThrowingStream<EventStreamItem, any Error> {
        var (response, chunks): (HTTPURLResponse?, AsyncThrowingStream<Data, any Error>?) = (nil, nil)
        var lastError: (any Error)?
        for host in orderedHosts() {
            var request = URLRequest(url: try url(host: host, path: "/v1/events", query: probe ? "probe=1" : nil), timeoutInterval: Self.streamIdleTimeout)
            request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
            authorize(&request)
            if let seq { request.setValue(String(seq), forHTTPHeaderField: "Last-Event-ID") }
            let started = ContinuousClock.now
            do {
                (response, chunks) = try await transport.stream(request)
                trace?.add(Self.tried(label(host), since: started, status: response?.statusCode ?? 0))
                preferredHost = host
                break
            } catch {
                trace?.add(Self.tried(label(host), since: started, error: error))
                lastError = error
                guard canFallBack(error, probe: false) || Self.lost(error) else { throw Self.mapped(error, reached: !Self.unsent(error)) }
            }
        }
        guard let response, let chunks else { throw Self.mapped(lastError ?? RemoteError.hostNotAllowed) }
        if response.statusCode == 401 { throw RemoteError.unauthorized }
        guard response.statusCode == 200 else { throw RemoteError.http(response.statusCode) }
        let opened = ContinuousClock.now
        let host = label(preferredHost ?? "")
        return AsyncThrowingStream { continuation in
            let task = Task {
                var parser = SSEParser()
                let decoder = JSONDecoder.clubmate
                var alive = false
                do {
                    for try await chunk in chunks {
                        let events = parser.feed(chunk)
                        if !chunk.isEmpty {
                            if !alive {
                                alive = true
                                continuation.yield(.alive)
                            } else if events.isEmpty, chunk.first == UInt8(ascii: ":") {
                                continuation.yield(.ping)
                            }
                        }
                        for event in events {
                            let data = Data(event.data.utf8)
                            switch event.event {
                            case "bus":
                                if let bus = try? decoder.decode(BusEvent.self, from: data) { continuation.yield(.event(bus)) }
                            case "stream":
                                if let frame = try? decoder.decode(StreamFrame.self, from: data) { continuation.yield(.stream(frame.id)) }
                            case "ui":
                                if let frame = try? decoder.decode(UIFrame.self, from: data) { continuation.yield(.ui(frame.view)) }
                            case "reset":
                                if let frame = try? decoder.decode(ResetFrame.self, from: data) { continuation.yield(.reset(frame.seq)) }
                            default:
                                break
                            }
                        }
                    }
                    continuation.finish()
                } catch {
                    trace?.add(Self.tried(host, since: opened, error: error))
                    continuation.finish(throwing: Self.mapped(error))
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func exchange(path: String, method: String, body: Data, probe: Bool = false, quick: Bool = false, timeout rpc: TimeInterval = rpcTimeout, trace: HostTrace? = nil) async throws -> (Data, HTTPURLResponse, String) {
        var lastError: (any Error)?
        var reached = false
        let hosts = orderedHosts()
        for (index, host) in hosts.enumerated() {
            let timeout = probe && (quick || index < hosts.count - 1) ? Self.probeTimeout : rpc
            var request = URLRequest(url: try url(host: host, path: path), timeoutInterval: timeout)
            request.httpMethod = method
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            authorize(&request)
            let started = ContinuousClock.now
            do {
                let (data, response) = try await transport.send(request)
                trace?.add(Self.tried(label(host), since: started, status: response.statusCode))
                preferredHost = host
                return (data, response, host)
            } catch {
                trace?.add(Self.tried(label(host), since: started, error: error))
                lastError = error
                if !Self.unsent(error) { reached = true }
                guard canFallBack(error, probe: probe) else { throw Self.mapped(error, reached: reached) }
            }
        }
        throw Self.mapped(lastError ?? RemoteError.hostNotAllowed, reached: reached)
    }

    private func orderedHosts() -> [String] {
        guard case .tailnet = route, let preferredHost, hosts.contains(preferredHost) else { return hosts }
        return [preferredHost] + hosts.filter { $0 != preferredHost }
    }

    private func canFallBack(_ error: any Error, probe: Bool) -> Bool {
        guard case .tailnet = route else { return false }
        if probe, Self.lost(error) { return true }
        guard let code = (error as? URLError)?.code else { return false }
        return Self.unsentErrors.contains(code) || (code == .timedOut && (probe || preferredHost == nil))
    }

    private func authorize(_ request: inout URLRequest) {
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
    }

    private nonisolated func label(_ host: String) -> String {
        guard case .relay(let tunnel, _) = route else { return host }
        return "relay \(tunnel.relayHost)"
    }

    private nonisolated func url(host: String, path: String, query: String? = nil) throws -> URL {
        if case .relay = route {
            var components = URLComponents()
            components.scheme = "clubmate-relay"
            components.host = host
            components.path = path
            components.query = query
            guard let url = components.url, url.host(percentEncoded: false) == host, url.port == nil, url.user == nil else { throw RemoteError.hostNotAllowed }
            return url
        }
        var components = URLComponents()
        components.scheme = "http"
        components.host = host.contains(":") ? "[\(host)]" : host
        components.port = port
        components.path = path
        components.query = query
        guard let url = components.url, url.host(percentEncoded: false) == host, url.port == port, url.user == nil else { throw RemoteError.hostNotAllowed }
        return url
    }

    private static func tried(_ host: String, since start: ContinuousClock.Instant, status: Int) -> HostTry {
        HostTry(host: host, milliseconds: HostTrace.milliseconds(since: start), outcome: "HTTP \(status)", ok: (200..<300).contains(status))
    }

    private static func tried(_ host: String, since start: ContinuousClock.Instant, error: any Error) -> HostTry {
        HostTry(host: host, milliseconds: HostTrace.milliseconds(since: start), outcome: ConnectionAttempt.describe(error), ok: false)
    }

    private static func lost(_ error: any Error) -> Bool {
        if (error as? URLError)?.code == .networkConnectionLost { return true }
        return (error as? POSIXError).map { lostErrors.contains($0.code) } ?? false
    }

    private static func unsent(_ error: any Error) -> Bool {
        (error as? URLError).map { unsentErrors.contains($0.code) } ?? false
    }

    private static func mapped(_ error: any Error, reached: Bool = true) -> any Error {
        if error is RemoteError || error is CancellationError { return error }
        if let url = error as? URLError {
            if url.code == .cancelled { return CancellationError() }
            if !reached, unsent(url) { return RemoteError.notSent(url.localizedDescription) }
            return RemoteError.unreachable(url.localizedDescription)
        }
        return error
    }
}

private struct PairRequest: Encodable {
    let code: String
    let device: PairDevice
}

private struct PairDevice: Encodable {
    let id: String
    let name: String
    let model: String
}

private struct IdentityReply: Decodable {
    let computer: ComputerInfo
}

private struct PairReply: Decodable {
    let token: String
    let computer: ComputerInfo
    let relayTicket: String?
}

private struct StreamFrame: Decodable {
    let id: String
}

private struct ResetFrame: Decodable {
    let seq: Int
}

private struct UIFrame: Decodable {
    let workspace: String?
    let workspaceId: String?
    let slot: String
    let owner: String?
    let rev: Int64
    let tree: SDUINode

    var view: SlotView {
        SlotView(workspaceId: workspace ?? workspaceId ?? "", slot: slot, owner: owner ?? "", rev: rev, tree: tree)
    }
}

private struct RPCRequest: Encodable {
    let method: String
    let params: JSONValue?
}

private struct Envelope<Result: Decodable>: Decodable {
    struct Failure: Decodable {
        let code: String
        let message: String
    }

    let result: Result?
    let error: Failure?
}
