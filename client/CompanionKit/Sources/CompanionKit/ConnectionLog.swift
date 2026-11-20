import Foundation
import Synchronization

public struct HostTry: Codable, Sendable, Equatable {
    public let host: String
    public let milliseconds: Int
    public let outcome: String
    public let ok: Bool

    public init(host: String, milliseconds: Int, outcome: String, ok: Bool) {
        self.host = host
        self.milliseconds = milliseconds
        self.outcome = outcome
        self.ok = ok
    }
}

public final class HostTrace: Sendable {
    private let tries = Mutex<[HostTry]>([])

    public init() {}

    public func add(_ entry: HostTry) {
        tries.withLock { $0.append(entry) }
    }

    public var values: [HostTry] {
        tries.withLock { $0 }
    }

    static func milliseconds(since start: ContinuousClock.Instant) -> Int {
        let elapsed = ContinuousClock.now - start
        return Int(elapsed.components.seconds * 1000 + elapsed.components.attoseconds / 1_000_000_000_000_000)
    }
}

public struct ConnectionAttempt: Codable, Sendable, Equatable, Identifiable {
    public enum Step: String, Codable, Sendable {
        case hello
        case workspaces
        case events
    }

    public var id: UUID
    public var at: Date
    public var last: Date
    public var step: Step
    public var ok: Bool
    public var milliseconds: Int
    public var error: String?
    public var tries: [HostTry]
    public var streak: Int
    public var repeats: Int
    public var background: Bool
    public var lived: Int?
    public var cancelled: Bool?
    public var path: NetworkPath?

    public init(id: UUID = UUID(), at: Date, step: Step, ok: Bool, milliseconds: Int, error: String? = nil, tries: [HostTry] = [], streak: Int = 0, background: Bool = false, lived: Int? = nil, cancelled: Bool = false, path: NetworkPath? = nil) {
        self.id = id
        self.at = at
        last = at
        self.step = step
        self.ok = ok
        self.milliseconds = milliseconds
        self.error = error
        self.tries = tries
        self.streak = streak
        repeats = 1
        self.background = background
        self.lived = lived
        self.cancelled = cancelled ? true : nil
        self.path = path
    }

    public var host: String? {
        tries.last(where: \.ok)?.host
    }

    public var title: String {
        if ok { return "Connected" }
        if cancelled == true { return "Cancelled at " + step.rawValue }
        if step == .events, let lived { return "Lost after " + Self.span(lived) }
        return "Failed at " + step.rawValue
    }

    func repeats(_ other: ConnectionAttempt) -> Bool {
        ok == other.ok && cancelled == other.cancelled && path == other.path && step == other.step && error == other.error && background == other.background && (lived == nil) == (other.lived == nil)
            && tries.map { [$0.host, $0.outcome] } == other.tries.map { [$0.host, $0.outcome] }
    }

    public static func span(_ seconds: Int) -> String {
        switch seconds {
        case ..<60: "\(seconds) s"
        case ..<3600: "\(seconds / 60) min"
        default: "\(seconds / 3600) h \(seconds % 3600 / 60) min"
        }
    }

    public static func describe(_ error: any Error) -> String {
        switch error {
        case let remote as RemoteError:
            return describe(remote)
        case let url as URLError:
            var parts = ["URLError \(url.code.rawValue)", url.localizedDescription]
            if let posix = (url as NSError).userInfo[NSUnderlyingErrorKey] as? NSError {
                parts.append(underlying(posix))
            }
            return parts.joined(separator: " · ")
        case is CancellationError:
            return "cancelled by the system"
        case let posix as POSIXError:
            return posixLine(posix.code.rawValue)
        default:
            let ns = error as NSError
            return "\(ns.domain) \(ns.code) · \(ns.localizedDescription)"
        }
    }

    static func describe(_ remote: RemoteError) -> String {
        switch remote {
        case .unauthorized: "HTTP 401 · token refused"
        case .http(let status): "HTTP \(status)"
        case .server(let code, let message): "server \(code) · \(message)"
        case .badResponse: "unreadable response"
        case .hostNotAllowed: "no tailnet address saved"
        case .relayRevoked: "relay HTTP 401 · ticket refused"
        case .pairingFailed(let message), .unreachable(let message), .notSent(let message): message
        }
    }

    private static func underlying(_ error: NSError) -> String {
        if error.domain == NSPOSIXErrorDomain { return posixLine(Int32(error.code)) }
        if let inner = error.userInfo[NSUnderlyingErrorKey] as? NSError { return underlying(inner) }
        return "\(error.domain) \(error.code)"
    }

    private static func posixLine(_ code: Int32) -> String {
        "POSIX \(code) · " + String(cString: strerror(code))
    }
}

public struct ConnectionLog: Codable, Sendable, Equatable {
    public static let capacity = 50

    public private(set) var attempts: [ConnectionAttempt] = []

    public init(attempts: [ConnectionAttempt] = []) {
        self.attempts = attempts
    }

    public var streak: Int {
        attempts.last.map { $0.ok ? 0 : $0.streak } ?? 0
    }

    public mutating func record(_ attempt: ConnectionAttempt) {
        var attempt = attempt
        attempt.streak = attempt.ok ? 0 : attempt.cancelled == true ? streak : streak + 1
        if var previous = attempts.last, previous.repeats(attempt) {
            previous.last = attempt.at
            previous.milliseconds = attempt.milliseconds
            previous.streak = attempt.streak
            previous.lived = attempt.lived
            previous.repeats += 1
            attempts[attempts.count - 1] = previous
            return
        }
        attempts.append(attempt)
        if attempts.count > Self.capacity { attempts.removeFirst(attempts.count - Self.capacity) }
    }

    public var text: String {
        attempts.reversed().map { attempt in
            var lines = [[ComputerConnection.clock(attempt.at), attempt.title, attempt.repeats > 1 ? "\(attempt.repeats)× until " + ComputerConnection.clock(attempt.last) : nil, attempt.background ? "background" : nil, attempt.path.map { "path " + $0.rawValue }].compactMap { $0 }.joined(separator: " · ")]
            if let error = attempt.error { lines.append("  " + error) }
            lines += attempt.tries.map { "  \($0.host) · \($0.milliseconds) ms · \($0.outcome)" }
            return lines.joined(separator: "\n")
        }.joined(separator: "\n")
    }
}
