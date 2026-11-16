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
    @ObservationIgnored private let passed: @MainActor () -> Void

    public init(hosts: [String], port: Int, token: String?, transport: any HTTPTransport, policy: HostPolicy, probe: any NetworkProbe, passed: @escaping @MainActor () -> Void = {}) {
        self.hosts = hosts
        self.port = port
        self.token = token
        self.transport = transport
        self.policy = policy
        self.probe = probe
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
        guard await checkHello(client, host: host) else {
            skip("events", "Live updates", because: "Skipped — Clubmate didn't answer")
            return
        }
        await checkEvents(client)
        if !Task.isCancelled, !failed { passed() }
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

    private func checkHello(_ client: RemoteClient, host: String) async -> Bool {
        steps.append(CheckStep(id: "hello", title: "Clubmate answers"))
        do {
            let hello = try await client.hello()
            update("hello") { $0.verdict = .pass; $0.detail = "\(hello.computer.name) · Clubmate \(hello.version) via \(host)" }
            return true
        } catch RemoteError.unauthorized {
            update("hello") { $0.verdict = .fail; $0.detail = "The Mac refused this phone's token (HTTP 401)"; $0.hint = "Pair again: on the Mac open Settings › Remote › Pair a device." }
        } catch {
            update("hello") { $0.verdict = .fail; $0.detail = ConnectionAttempt.describe(error); $0.hint = "Something answered on port \(port) but not as this app expects — update Clubmate on the Mac, or check that nothing else uses the port." }
        }
        return false
    }

    private func checkEvents(_ client: RemoteClient) async {
        steps.append(CheckStep(id: "events", title: "Live updates"))
        let timeout = Self.streamTimeout
        let result: Result<Bool, any Error> = await withTaskGroup(of: Result<Bool, any Error>?.self) { group in
            group.addTask {
                do {
                    for try await item in try await client.events(after: nil) where item == .alive {
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
        switch result {
        case .success(true):
            update("events") { $0.verdict = .pass; $0.detail = "The event stream said “connected”" }
        case .success(false):
            update("events") { $0.verdict = .fail; $0.detail = "Nothing on the event stream in \(Int(timeout.components.seconds)) s"; $0.hint = "Something between the phone and the Mac holds back long-lived connections — another VPN or a proxy." }
        case .failure(RemoteError.unauthorized):
            update("events") { $0.verdict = .fail; $0.detail = "The Mac refused this phone's token (HTTP 401)"; $0.hint = "Pair again: on the Mac open Settings › Remote › Pair a device." }
        case .failure(let error):
            update("events") { $0.verdict = .fail; $0.detail = ConnectionAttempt.describe(error); $0.hint = "The Mac answered but the live stream broke — another VPN or a proxy may cut long-lived connections." }
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
