import Foundation
import Network
import Synchronization

public struct SystemProbe: NetworkProbe {
    public init() {}

    public func tailnetAddresses() -> [String] {
        var list: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&list) == 0, let first = list else { return [] }
        defer { freeifaddrs(list) }
        var found: [String] = []
        for entry in sequence(first: first, next: { $0.pointee.ifa_next }) {
            guard let address = entry.pointee.ifa_addr, let text = Self.text(address) else { continue }
            if HostPolicy.tailnet.allows(text), !found.contains(text) { found.append(text) }
        }
        return found
    }

    public func resolve(_ name: String) async -> ResolveResult {
        await Task.detached {
            var hints = addrinfo()
            hints.ai_socktype = SOCK_STREAM
            var result: UnsafeMutablePointer<addrinfo>?
            let status = getaddrinfo(name, nil, &hints, &result)
            guard status == 0, let first = result else { return .failed(String(cString: gai_strerror(status))) }
            defer { freeaddrinfo(result) }
            var found: [String] = []
            for entry in sequence(first: first, next: { $0.pointee.ai_next }) {
                if let address = entry.pointee.ai_addr, let text = Self.text(address), !found.contains(text) { found.append(text) }
            }
            return .addresses(found)
        }.value
    }

    public func connect(host: String, port: Int, timeout: Duration) async -> ConnectResult {
        guard let port = NWEndpoint.Port(rawValue: UInt16(clamping: port)) else { return .failed("Bad port") }
        let connection = NWConnection(host: NWEndpoint.Host(host), port: port, using: .tcp)
        let queue = DispatchQueue(label: "clubmate.probe")
        let once = Once()
        return await withCheckedContinuation { continuation in
            let finish: @Sendable (ConnectResult) -> Void = { result in
                guard once.claim() else { return }
                connection.cancel()
                continuation.resume(returning: result)
            }
            let lastError = Mutex<NWError?>(nil)
            connection.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    finish(.open)
                case .waiting(let error):
                    if connection.currentPath?.unsatisfiedReason == .localNetworkDenied { finish(.localNetworkDenied) }
                    else if error == .posix(.ECONNREFUSED) { finish(.refused) }
                    else { lastError.withLock { $0 = error } }
                case .failed(let error):
                    finish(Self.result(error))
                default:
                    break
                }
            }
            connection.start(queue: queue)
            queue.asyncAfter(deadline: .now() + .milliseconds(Int(timeout.components.seconds * 1000))) {
                finish(lastError.withLock { $0 }.map(Self.result) ?? .timedOut)
            }
        }
    }

    private static let silent: Set<POSIXErrorCode> = [.ETIMEDOUT, .ENETUNREACH, .EHOSTUNREACH, .EHOSTDOWN, .ENETDOWN]

    private static func result(_ error: NWError) -> ConnectResult {
        guard case .posix(let code) = error else { return .failed(error.localizedDescription) }
        if code == .ECONNREFUSED { return .refused }
        return silent.contains(code) ? .timedOut : .failed(error.localizedDescription)
    }

    private static func text(_ address: UnsafePointer<sockaddr>) -> String? {
        var buffer = [CChar](repeating: 0, count: Int(INET6_ADDRSTRLEN))
        switch Int32(address.pointee.sa_family) {
        case AF_INET:
            var v4 = address.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { $0.pointee.sin_addr }
            guard inet_ntop(AF_INET, &v4, &buffer, socklen_t(buffer.count)) != nil else { return nil }
        case AF_INET6:
            var v6 = address.withMemoryRebound(to: sockaddr_in6.self, capacity: 1) { $0.pointee.sin6_addr }
            guard inet_ntop(AF_INET6, &v6, &buffer, socklen_t(buffer.count)) != nil else { return nil }
        default:
            return nil
        }
        return String(cString: buffer)
    }
}

private final class Once: Sendable {
    private let done = Mutex(false)

    func claim() -> Bool {
        done.withLock { done in
            defer { done = true }
            return !done
        }
    }
}
