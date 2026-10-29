import Foundation

public struct ConfirmRequest: Decodable, Sendable, Equatable {
    static let textLimit = 200
    static let fieldLimit = 10
    static let tooLong = "args too long to display"
    static let timedOut = "approval timed out"
    static let margin = 5000

    public let title: String
    public let workspace: String
    public let command: String
    public let args: JSONValue
    public let timeoutMs: Int?
    public let session: ConfirmSession?

    public init(title: String, workspace: String, command: String, args: JSONValue, timeoutMs: Int? = nil, session: ConfirmSession? = nil) {
        self.title = title
        self.workspace = workspace
        self.command = command
        self.args = args
        self.timeoutMs = timeoutMs
        self.session = session
    }

    public var deadline: Duration? {
        timeoutMs.map { .milliseconds(max($0 - Self.margin, 0)) }
    }

    public var message: String {
        let title = Self.capped(title)
        return "Allow agent to \(title.prefix(1).lowercased() + title.dropFirst())?"
    }

    public var detail: String {
        var lines = ["Workspace: \(workspace.isEmpty ? "none" : Self.capped(workspace))", "Command: \(Self.capped(command))"]
        if let session {
            let title = session.title.isEmpty ? "untitled" : Self.capped(session.title)
            let place = session.workspace.isEmpty ? "" : " in \(Self.capped(session.workspace))"
            lines.append("Session: \(title) (\(Self.capped(session.id)))\(place)")
        }
        switch args {
        case .null:
            break
        case .object(let fields):
            lines += fields.keys.sorted().map { "\(Self.clean($0)): \(fields[$0].map(Self.summary) ?? "")" }
        default:
            lines.append("Args (not an object): \(Self.summary(args))")
        }
        return lines.joined(separator: "\n")
    }

    public var fitsDisplay: Bool {
        guard let fields = args.objectValue else { return true }
        return fields.count <= Self.fieldLimit && fields.values.allSatisfy { Self.rendered($0).count <= Self.textLimit }
    }

    @MainActor
    public static func answer(_ request: ConfirmRequest, ask: @MainActor (ConfirmRequest, ConfirmExpiry) async -> Bool) async -> ConfirmReply {
        guard request.fitsDisplay else { return ConfirmReply(approved: false, reason: tooLong) }
        let expiry = ConfirmExpiry()
        let timer = request.deadline.map { deadline in
            Task { @MainActor in
                guard (try? await Task.sleep(for: deadline)) != nil else { return }
                expiry.expire()
            }
        }
        defer { timer?.cancel() }
        let approved = await ask(request, expiry)
        if expiry.expired { return ConfirmReply(approved: false, reason: timedOut) }
        return ConfirmReply(approved: approved, reason: nil)
    }

    static func capped(_ text: String) -> String {
        let out = clean(text)
        return out.count > textLimit ? String(out.prefix(textLimit)) + "…" : out
    }

    static func clean(_ text: String) -> String {
        var out = ""
        for scalar in text.unicodeScalars {
            switch scalar {
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
    private var actions: [@MainActor () -> Void] = []

    public init() {}

    public func onExpire(_ action: @escaping @MainActor () -> Void) {
        if expired { return action() }
        actions.append(action)
    }

    func expire() {
        guard !expired else { return }
        expired = true
        let pending = actions
        actions = []
        pending.forEach { $0() }
    }
}

public struct ConfirmSession: Decodable, Sendable, Equatable {
    public let id: String
    public let title: String
    public let workspace: String

    public init(id: String, title: String, workspace: String) {
        self.id = id
        self.title = title
        self.workspace = workspace
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
