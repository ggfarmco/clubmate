import Foundation

public enum EventKind: String, Codable, Sendable {
    case state
    case stream
    case outbox
    case command
}

public struct EventLine: Codable, Sendable, Equatable {
    public let at: Date
    public let kind: EventKind
    public let text: String
    public var repeats: Int?
    public var last: Date?

    public init(at: Date, kind: EventKind, text: String) {
        self.at = at
        self.kind = kind
        self.text = text
    }
}

public struct EventLog: Codable, Sendable, Equatable {
    public static let capacity = 500

    public private(set) var lines: [EventLine] = []

    public init(lines: [EventLine] = []) {
        self.lines = lines
    }

    public mutating func add(_ kind: EventKind, _ text: String, at: Date = .now) {
        if let previous = lines.last, previous.kind == kind, previous.text == text {
            lines[lines.count - 1].repeats = (previous.repeats ?? 1) + 1
            lines[lines.count - 1].last = at
            return
        }
        lines.append(EventLine(at: at, kind: kind, text: text))
        if lines.count > Self.capacity { lines.removeFirst(lines.count - Self.capacity) }
    }

    public var text: String {
        lines.map { line in
            let times = line.repeats.map { " ×\($0) until " + (line.last ?? line.at).formatted(.iso8601) } ?? ""
            return line.at.formatted(.iso8601) + " " + line.kind.rawValue + " " + line.text + times
        }.joined(separator: "\n")
    }

    public static func code(_ error: any Error) -> String {
        switch error {
        case RemoteError.server(let code, _): "server " + code
        case let remote as RemoteError: ConnectionAttempt.describe(remote)
        default: ConnectionAttempt.describe(error)
        }
    }

    public static func shortCode(_ error: any Error) -> String {
        switch error as? RemoteError {
        case .server(let code, _)?: code
        case .unauthorized?: "unauthorized"
        case .http(let status)?: "http-\(status)"
        case .unreachable?: "unreachable"
        case .notSent?: "not-sent"
        case .badResponse?: "bad-response"
        case .hostNotAllowed?: "host-not-allowed"
        case .relayRevoked?: "relay-revoked"
        case .pairingFailed?: "pairing-failed"
        case nil: error is CancellationError ? "cancelled" : "error"
        }
    }

    static func requestTag(_ id: String) -> String {
        String(id.prefix(8))
    }
}
