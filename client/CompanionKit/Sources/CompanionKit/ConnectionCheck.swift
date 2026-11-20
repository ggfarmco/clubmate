import Foundation
import Observation

public enum ResolveResult: Sendable, Equatable {
    case addresses([String])
    case failed(String)
}

public enum ConnectResult: Sendable, Equatable {
    case open
    case refused
    case timedOut
    case localNetworkDenied
    case failed(String)
}

public protocol NetworkProbe: Sendable {
    func tailnetAddresses() -> [String]
    func resolve(_ name: String) async -> ResolveResult
    func connect(host: String, port: Int, timeout: Duration) async -> ConnectResult
}

public struct CheckStep: Identifiable, Sendable, Equatable {
    public enum Verdict: Sendable, Equatable {
        case running
        case pass
        case fail
        case skipped
    }

    public let id: String
    public var title: String
    public var verdict: Verdict
    public var detail: String?
    public var hint: String?

    public init(id: String, title: String, verdict: Verdict = .running, detail: String? = nil, hint: String? = nil) {
        self.id = id
        self.title = title
        self.verdict = verdict
        self.detail = detail
        self.hint = hint
    }
}

@MainActor
@Observable
public final class ConnectionCheck {
    public struct Relay: Sendable, Equatable {
        public let url: URL
        public let macKey: Data
        public let kid: String
        public let ticket: String
        public let computer: String

        public init(url: URL, macKey: Data, kid: String, ticket: String, computer: String) {
            self.url = url
            self.macKey = macKey
            self.kid = kid
            self.ticket = ticket
            self.computer = computer
        }
    }

    public static let connectTimeout: Duration = .seconds(4)
    public static let streamTimeout: Duration = .seconds(5)
    public static let resolveTimeout: Duration = .seconds(5)

    public private(set) var steps: [CheckStep] = []
    public private(set) var running = false
    public private(set) var finishedAt: Date?

    @ObservationIgnored private let hosts: [String]
    @ObservationIgnored private let port: Int
    @ObservationIgnored private let token: String?
    @ObservationIgnored private let transport: any HTTPTransport
    @ObservationIgnored private let policy: HostPolicy
    @ObservationIgnored private let probe: any NetworkProbe
    @ObservationIgnored private let relay: Relay?
    @ObservationIgnored private let macName: String
    @ObservationIgnored private let first: NetworkPath
    @ObservationIgnored private let passed: @MainActor () -> Void

    public init(hosts: [String], port: Int, token: String?, transport: any HTTPTransport, policy: HostPolicy, probe: any NetworkProbe, relay: Relay? = nil, macName: String = "The Mac", first: NetworkPath = .tailnet, passed: @escaping @MainActor () -> Void = {}) {
        self.hosts = hosts
        self.port = port
        self.token = token
        self.transport = transport
        self.policy = policy
        self.probe = probe
        self.relay = relay
        self.macName = macName
        self.first = first
        self.passed = passed
    }

    public var failed: Bool {
        steps.contains { $0.verdict == .fail }
    }

    public func run() async {
        guard !running else { return }
        running = true
        finishedAt = nil
        steps = []
        defer {
            running = false
            finishedAt = .now
        }
        var answered = false
        for path in first == .relay ? [NetworkPath.relay, .tailnet] : [.tailnet, .relay] {
            let start = steps.count
            switch path {
            case .tailnet:
                guard !hosts.isEmpty || relay == nil else { continue }
                await checkTailnet()
            case .relay:
                guard let relay else { continue }
                await checkRelay(relay)
            }
            if !steps[start...].contains(where: { $0.verdict == .fail }) { answered = true }
        }
        if !Task.isCancelled, answered { passed() }
    }

    private func checkTailnet() async {
        checkTailscale()
        await checkNames()
        let open = await checkPorts()
        if let host = open.first(where: policy.allows) {
            update("tailscale") { step in
                guard step.verdict == .fail else { return }
                step.verdict = .pass
                step.detail = "\(host) answered over Tailscale"
                step.hint = nil
            }
        }
        guard let host = open.first else {
            skip("hello", "Clubmate answers", because: "Skipped — no address answered")
            skip("events", "Live updates", because: "Skipped — no address answered")
            return
        }
        let client = RemoteClient(hosts: [host], port: port, token: token, transport: transport, policy: policy)
        guard await checkHello(client, id: "hello", via: host, hint: "Something answered on port \(port) but not as this app expects — update Clubmate on the Mac, or check that nothing else uses the port.") else {
            skip("events", "Live updates", because: "Skipped — Clubmate didn't answer")
            return
        }
        await checkEvents(client, id: "events", hint: "The Mac answered but the live stream broke — another VPN or a proxy may cut long-lived connections.")
    }

    private func checkRelay(_ relay: Relay) async {
        let host = relay.url.host() ?? relay.url.absoluteString
        steps.append(CheckStep(id: "relay", title: "Reach the relay"))
        var health = URLRequest(url: relay.url.appending(path: "healthz"), timeoutInterval: TimeInterval(Self.connectTimeout.components.seconds))
        health.httpMethod = "GET"
        let reachable: Bool
        do {
            let (_, response) = try await transport.send(health)
            reachable = response.statusCode == 200
            update("relay") { step in
                step.verdict = reachable ? .pass : .fail
                step.detail = reachable ? host + " answered" : "\(host) answered HTTP \(response.statusCode)"
                if !reachable { step.hint = Self.relayDownHint }
            }
        } catch {
            reachable = false
            update("relay") { $0.verdict = .fail; $0.detail = ConnectionAttempt.describe(error); $0.hint = "Check this phone's internet connection. " + Self.relayDownHint }
        }
        let macStep = macName + " at the relay"
        guard reachable else {
            for (id, title) in [("relay-mac", macStep), ("relay-key", "The Mac's key"), ("relay-hello", "Clubmate answers through the relay"), ("relay-events", "Live updates through the relay")] {
                skip(id, title, because: "Skipped — the relay didn't answer")
            }
            return
        }
        steps.append(CheckStep(id: "relay-mac", title: macStep))
        let tunnel = TunnelTransport(relay: relay.url, macKey: relay.macKey, ticket: relay.ticket, outer: transport)
        let opened = await tunnel.checkOpen()
        switch opened {
        case .open, .badKey:
            update("relay-mac") { $0.verdict = .pass; $0.detail = macName + " is connected to the relay" }
        case .macOffline:
            update("relay-mac") { $0.verdict = .fail; $0.detail = macName + " is not connected to the relay: is Clubmate running?"; $0.hint = "Wake the Mac and check that Clubmate runs there with the relay on (remote.status shows tunnel connected)." }
        case .relayUnreachable:
            update("relay-mac") { $0.verdict = .fail; $0.detail = TunnelTransport.relayUnreachable; $0.hint = Self.relayDownHint }
        case .revoked:
            update("relay-mac") { $0.verdict = .fail; $0.detail = RemoteError.relayRevokedMessage; $0.hint = Self.pairHint }
        case .failed(let reason):
            update("relay-mac") { $0.verdict = .fail; $0.detail = reason; $0.hint = Self.relayDownHint }
        }
        switch opened {
        case .open:
            steps.append(CheckStep(id: "relay-key", title: "The Mac's key", verdict: .pass, detail: "The Mac holds the key " + relay.kid))
        case .badKey:
            steps.append(CheckStep(id: "relay-key", title: "The Mac's key", verdict: .fail, detail: "The Mac didn't prove the key " + relay.kid, hint: Self.pairHint))
        default:
            skip("relay-key", "The Mac's key", because: "Skipped — the Mac isn't at the relay")
        }
        guard case .open = opened else {
            skip("relay-hello", "Clubmate answers through the relay", because: "Skipped — no channel to the Mac")
            skip("relay-events", "Live updates through the relay", because: "Skipped — no channel to the Mac")
            return
        }
        let client = RemoteClient(route: .relay(tunnel, computer: relay.computer), token: token, transport: transport, policy: policy)
        guard await checkHello(client, id: "relay-hello", via: "the relay", title: "Clubmate answers through the relay", hint: "The relay reached the Mac but Clubmate didn't answer as this app expects — update Clubmate on the Mac.") else {
            skip("relay-events", "Live updates through the relay", because: "Skipped — Clubmate didn't answer")
            return
        }
        await checkEvents(client, id: "relay-events", title: "Live updates through the relay", hint: "The Mac answered but the live stream through the relay broke — check the relay and remote.status on the Mac.")
    }

    private static let relayDownHint = "The Clubmate relay may be down — try again in a minute, or connect over Tailscale."
    private static let pairHint = "Pair again: on the Mac open Settings › Remote › Pair a device."

    public static func answers(_ client: RemoteClient) async -> Bool {
        guard (try? await client.hello(quick: true)) != nil else { return false }
        return (try? await firstLine(client, timeout: streamTimeout).get()) == true
    }

    private static func firstLine(_ client: RemoteClient, timeout: Duration) async -> Result<Bool, any Error> {
        await withTaskGroup(of: Result<Bool, any Error>?.self) { group in
            group.addTask {
                do {
                    for try await item in try await client.events(after: nil, probe: true) where item == .alive {
                        return .success(true)
                    }
                    return .success(false)
                } catch {
                    return .failure(error)
                }
            }
            group.addTask {
                try? await Task.sleep(for: timeout)
                return nil
            }
            let first = await group.next() ?? nil
            group.cancelAll()
            return first ?? .success(false)
        }
    }

    public var text: String {
        steps.map { step in
            let mark = switch step.verdict {
            case .running: "…"
            case .pass: "✓"
            case .fail: "✗"
            case .skipped: "–"
            }
            return ([mark + " " + step.title] + [step.detail, step.hint].compactMap { $0.map { "  " + $0 } }).joined(separator: "\n")
        }.joined(separator: "\n")
    }

    private func checkTailscale() {
        let mine = probe.tailnetAddresses()
        if mine.isEmpty {
            steps.append(CheckStep(id: "tailscale", title: "Tailscale on this phone", verdict: .fail, detail: "This phone has no Tailscale address", hint: "Tailscale isn't on on this phone — open the Tailscale app and connect."))
        } else {
            steps.append(CheckStep(id: "tailscale", title: "Tailscale on this phone", verdict: .pass, detail: "This phone is " + mine.joined(separator: ", ")))
        }
    }

    private func checkNames() async {
        let names = hosts.filter(Self.isName)
        guard !names.isEmpty else {
            steps.append(CheckStep(id: "dns", title: "MagicDNS name", verdict: .skipped, detail: "No MagicDNS name saved for this Mac"))
            return
        }
        for name in names {
            let id = "dns:" + name
            steps.append(CheckStep(id: id, title: "Find " + name))
            switch await resolve(name) {
            case .addresses(let found) where !found.isEmpty:
                update(id) { $0.verdict = .pass; $0.detail = "Points to " + found.joined(separator: ", ") }
            case .addresses:
                update(id) { $0.verdict = .fail; $0.detail = "No address"; $0.hint = Self.dnsHint }
            case .failed(let reason):
                update(id) { $0.verdict = .fail; $0.detail = reason; $0.hint = Self.dnsHint }
            }
        }
    }

    private func resolve(_ name: String) async -> ResolveResult {
        await withTaskGroup(of: ResolveResult?.self) { group in
            group.addTask { [probe] in await probe.resolve(name) }
            group.addTask {
                try? await Task.sleep(for: Self.resolveTimeout)
                return nil
            }
            let first = await group.next() ?? nil
            group.cancelAll()
            return first ?? .failed("No answer from DNS in \(Int(Self.resolveTimeout.components.seconds)) s")
        }
    }

    private static let dnsHint = "Turn on MagicDNS (Use Tailscale DNS) in the Tailscale app, or check that the Mac is still on your tailnet."

    private func checkPorts() async -> [String] {
        guard !hosts.isEmpty else {
            steps.append(CheckStep(id: "tcp", title: "Reach the Mac", verdict: .fail, detail: "No Tailscale address saved for this Mac", hint: "Pair again from Clubmate on the Mac: Settings › Remote › Pair a device."))
            return []
        }
        for host in hosts {
            steps.append(CheckStep(id: "tcp:" + host, title: "Reach " + host))
        }
        let results = await withTaskGroup(of: (String, ConnectResult).self) { group in
            for host in hosts {
                group.addTask { [probe, port] in (host, await probe.connect(host: host, port: port, timeout: Self.connectTimeout)) }
            }
            var results: [String: ConnectResult] = [:]
            for await (host, result) in group {
                results[host] = result
                update("tcp:" + host) { Self.describe(result, host: host, port: port, into: &$0) }
            }
            return results
        }
        return hosts.filter { results[$0] == .open }
    }

    private static func describe(_ result: ConnectResult, host: String, port: Int, into step: inout CheckStep) {
        switch result {
        case .open:
            step.verdict = .pass
            step.detail = "Answers on port \(port)"
        case .refused:
            step.verdict = .fail
            step.detail = "Refused port \(port)"
            step.hint = "The Mac is awake but nothing listens on \(port) — is Clubmate running with companion access on? If the port was changed, pair again."
        case .timedOut:
            step.verdict = .fail
            step.detail = "No answer on port \(port)"
            step.hint = "The Mac may be asleep or off your tailnet — wake it and check Tailscale on the Mac."
        case .localNetworkDenied:
            step.verdict = .fail
            step.detail = "Blocked by Local Network access"
            step.hint = "Turn on Local Network for Clubmate in Settings › Privacy & Security › Local Network."
        case .failed(let reason):
            step.verdict = .fail
            step.detail = reason
            step.hint = "Couldn't open a connection to \(host) — check that Tailscale is connected on this phone and on the Mac."
        }
    }

    private func checkHello(_ client: RemoteClient, id: String, via: String, title: String = "Clubmate answers", hint: String) async -> Bool {
        steps.append(CheckStep(id: id, title: title))
        do {
            let hello = try await client.hello()
            update(id) { $0.verdict = .pass; $0.detail = "\(hello.computer.name) · Clubmate \(hello.version) via \(via)" }
            return true
        } catch RemoteError.unauthorized {
            update(id) { $0.verdict = .fail; $0.detail = "The Mac refused this phone's token (HTTP 401)"; $0.hint = Self.pairHint }
        } catch RemoteError.relayRevoked {
            update(id) { $0.verdict = .fail; $0.detail = RemoteError.relayRevokedMessage; $0.hint = Self.pairHint }
        } catch {
            update(id) { $0.verdict = .fail; $0.detail = ConnectionAttempt.describe(error); $0.hint = hint }
        }
        return false
    }

    private func checkEvents(_ client: RemoteClient, id: String, title: String = "Live updates", hint: String) async {
        steps.append(CheckStep(id: id, title: title))
        let timeout = Self.streamTimeout
        switch await Self.firstLine(client, timeout: timeout) {
        case .success(true):
            update(id) { $0.verdict = .pass; $0.detail = "The event stream said “connected”" }
        case .success(false):
            update(id) { $0.verdict = .fail; $0.detail = "Nothing on the event stream in \(Int(timeout.components.seconds)) s"; $0.hint = "Something between the phone and the Mac holds back long-lived connections — another VPN or a proxy." }
        case .failure(RemoteError.unauthorized):
            update(id) { $0.verdict = .fail; $0.detail = "The Mac refused this phone's token (HTTP 401)"; $0.hint = Self.pairHint }
        case .failure(RemoteError.relayRevoked):
            update(id) { $0.verdict = .fail; $0.detail = RemoteError.relayRevokedMessage; $0.hint = Self.pairHint }
        case .failure(let error):
            update(id) { $0.verdict = .fail; $0.detail = ConnectionAttempt.describe(error); $0.hint = hint }
        }
    }

    private func skip(_ id: String, _ title: String, because reason: String) {
        steps.append(CheckStep(id: id, title: title, verdict: .skipped, detail: reason))
    }

    private func update(_ id: String, _ change: (inout CheckStep) -> Void) {
        guard let index = steps.firstIndex(where: { $0.id == id }) else { return }
        change(&steps[index])
    }

    static func isName(_ host: String) -> Bool {
        !host.contains(":") && host.split(separator: ".").contains { $0.contains(where: \.isLetter) }
    }
}
