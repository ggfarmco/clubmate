import Foundation

public struct ConfirmRequest: Decodable, Sendable, Equatable {
    static let textLimit = 200
    static let bodyLimit = 64 << 10
    static let fieldLimit = 10
    static let tooLong = "args too long to display"
    static let timedOut = "approval timed out"
    static let withdrawn = "approval withdrawn"
    static let margin = 5000

    public let title: String
    public let workspaceName: String
    public let command: String
    public let args: JSONValue
    public let body: String?
    public let plugin: Bool?
    public let timeoutMs: Int?
    public let session: ConfirmSession?
    public let from: ConfirmSession?
    public let id: String?

    public init(title: String, workspaceName: String, command: String, args: JSONValue, body: String? = nil, plugin: Bool? = nil, timeoutMs: Int? = nil, session: ConfirmSession? = nil, from: ConfirmSession? = nil, id: String? = nil) {
        self.title = title
        self.workspaceName = workspaceName
        self.command = command
        self.args = args
        self.body = body
        self.plugin = plugin
        self.timeoutMs = timeoutMs
        self.session = session
        self.from = from
        self.id = id
    }

    public var deadline: Duration? {
        timeoutMs.map { .milliseconds(max($0 - Self.margin, 0)) }
    }

    public var message: String {
        let title = Self.capped(title)
        if plugin == true { return "\(title)?" }
        return "Allow agent to \(title.prefix(1).lowercased() + title.dropFirst())?"
    }

    public var detail: String {
        var lines = ["Workspace: \(workspaceName.isEmpty ? "none" : Self.capped(workspaceName))", "Command: \(Self.capped(command))"]
        if let session {
            let title = session.title.isEmpty ? "untitled" : Self.capped(session.title)
            let place = session.workspaceName.isEmpty ? "" : " in \(Self.capped(session.workspaceName))"
            lines.append("Session: \(title) (\(Self.capped(session.id)))\(place)")
        }
        if let from {
            lines.append(from.title.isEmpty ? "From: \(Self.capped(from.id))" : "From: \(Self.capped(from.title)) (\(Self.capped(from.id)))")
        }
        return lines.joined(separator: "\n")
    }

    public var facts: String {
        switch args {
        case .null:
            ""
        case .object(let fields):
            fields.keys.sorted().map { "\(Self.clean($0)): \(fields[$0].map(Self.summary) ?? "")" }.joined(separator: "\n")
        default:
            "Args (not an object): \(Self.summary(args))"
        }
    }

    public var displayBody: String? {
        body.map { Self.clean($0, lines: true) }
    }

    public var fitsDisplay: Bool {
        guard (body?.unicodeScalars.count ?? 0) <= Self.bodyLimit else { return false }
        guard let fields = args.objectValue else { return true }
        return fields.count <= Self.fieldLimit && fields.values.allSatisfy { Self.rendered($0).count <= Self.textLimit }
    }

    @MainActor
    public static func answer(_ request: ConfirmRequest, expiry: ConfirmExpiry? = nil, ask: @MainActor (ConfirmRequest, ConfirmExpiry) async -> Bool) async -> ConfirmReply {
        guard request.fitsDisplay else { return ConfirmReply(approved: false, reason: tooLong) }
        let expiry = expiry ?? ConfirmExpiry()
        if expiry.expired { return ConfirmReply(approved: false, reason: expiry.reason) }
        let timer = request.deadline.map { deadline in
            Task { @MainActor in
                guard (try? await Task.sleep(for: deadline)) != nil else { return }
                expiry.expire()
            }
        }
        defer { timer?.cancel() }
        let approved = await ask(request, expiry)
        if expiry.expired { return ConfirmReply(approved: false, reason: expiry.reason) }
        return ConfirmReply(approved: approved, reason: nil)
    }

    static func capped(_ text: String) -> String {
        let out = clean(text)
        return out.count > textLimit ? String(out.prefix(textLimit)) + "…" : out
    }

    static func clean(_ text: String, lines: Bool = false) -> String {
        var out = ""
        for scalar in text.unicodeScalars {
            switch scalar {
            case "\n" where lines, "\t" where lines: out.unicodeScalars.append(scalar)
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            case _ where [.control, .format, .lineSeparator, .paragraphSeparator].contains(scalar.properties.generalCategory):
                out += "\\u{\(String(scalar.value, radix: 16))}"
            default:
                out.unicodeScalars.append(scalar)
            }
        }
        return out
    }

    private static func summary(_ value: JSONValue) -> String {
        clean(rendered(value))
    }

    private static func rendered(_ value: JSONValue) -> String {
        switch value {
        case .null: return "null"
        case .bool(let value): return String(value)
        case .number(let value): return value == value.rounded() && abs(value) < 1e15 ? String(Int64(value)) : String(value)
        case .string(let value): return value
        case .array, .object:
            guard let data = try? JSONEncoder().encode(value) else { return "" }
            return String(decoding: data, as: UTF8.self)
        }
    }
}

@MainActor
public final class ConfirmExpiry {
    public private(set) var expired = false
    public private(set) var reason = ConfirmRequest.timedOut
    private var actions: [@MainActor () -> Void] = []

    public init() {}

    public func onExpire(_ action: @escaping @MainActor () -> Void) {
        if expired { return action() }
        actions.append(action)
    }

    func expire(_ reason: String = ConfirmRequest.timedOut) {
        guard !expired else { return }
        expired = true
        self.reason = reason
        let pending = actions
        actions = []
        pending.forEach { $0() }
    }
}

@MainActor
struct Withdrawals {
    static let earlyCap = 32
    static let earlyAge: Duration = .seconds(30)

    private var open: [String: (expiry: ConfirmExpiry, reason: String)] = [:]
    private var early: [(id: String, at: ContinuousClock.Instant)] = []

    mutating func track(_ id: String?, reason: String, now: ContinuousClock.Instant = .now) -> ConfirmExpiry {
        let expiry = ConfirmExpiry()
        guard let id else { return expiry }
        prune(now)
        if let index = early.firstIndex(where: { $0.id == id }) {
            early.remove(at: index)
            expiry.expire(reason)
        }
        open[id] = (expiry, reason)
        return expiry
    }

    mutating func finish(_ id: String?) {
        if let id { open[id] = nil }
    }

    mutating func withdraw(_ id: String, now: ContinuousClock.Instant = .now) {
        if let entry = open[id] { return entry.expiry.expire(entry.reason) }
        prune(now)
        early.append((id, now))
        if early.count > Self.earlyCap { early.removeFirst(early.count - Self.earlyCap) }
    }

    private mutating func prune(_ now: ContinuousClock.Instant) {
        early.removeAll { now - $0.at > Self.earlyAge }
    }
}

public struct ConfirmSession: Decodable, Sendable, Equatable {
    public let id: String
    public let title: String
    public let workspaceName: String

    public init(id: String, title: String, workspaceName: String) {
        self.id = id
        self.title = title
        self.workspaceName = workspaceName
    }
}

public struct ConfirmReply: Encodable, Sendable, Equatable {
    public let approved: Bool
    public let reason: String?

    public init(approved: Bool, reason: String?) {
        self.approved = approved
        self.reason = reason
    }
}
