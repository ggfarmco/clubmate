import Foundation

public enum NotificationKind: String, Sendable, Hashable, CaseIterable, Codable {
    case needsInput = "needs-input"
    case permission
    case done
    case stuck
    case info

    public var label: String {
        switch self {
        case .needsInput: "Needs input"
        case .permission: "Permission needed"
        case .done: "Done"
        case .stuck: "Stuck"
        case .info: "Info"
        }
    }

    public var symbol: String {
        switch self {
        case .needsInput: "questionmark.bubble"
        case .permission: "hand.raised"
        case .done: "checkmark.circle"
        case .stuck: "exclamationmark.triangle"
        case .info: "info.circle"
        }
    }
}

public enum NotificationPolicy: String, FallbackEnum {
    case all
    case needsInput = "needs-input"
    case muted

    public static let fallback = Self.all

    public init(session: String, in sessions: JSONValue) {
        self = sessions.arrayValue?.first { $0["id"]?.stringValue == session }?["notifyEffective"]?.stringValue.flatMap(Self.init(rawValue:)) ?? .all
    }

    public func allows(_ kind: NotificationKind) -> Bool {
        switch self {
        case .all: true
        case .needsInput: kind == .needsInput || kind == .permission
        case .muted: false
        }
    }
}

public struct NotifyPosted: Sendable, Equatable {
    public let id: String
    public let workspace: String?
    public let session: String?
    public let owner: String?
    public let title: String
    public let message: String
    public let kind: NotificationKind
    public let viaAgent: Bool
    public let date: Date?

    public init?(_ payload: JSONValue) {
        guard let id = payload["id"]?.stringValue, let message = payload["message"]?.stringValue else { return nil }
        let nonEmpty = { (key: String) in payload[key]?.stringValue.flatMap { $0.isEmpty ? nil : $0 } }
        self.id = id
        self.message = message
        workspace = nonEmpty("workspace")
        session = nonEmpty("session")
        owner = nonEmpty("owner")
        title = nonEmpty("title") ?? ""
        kind = nonEmpty("kind").flatMap(NotificationKind.init(rawValue:)) ?? .info
        viaAgent = payload["agent"] == .bool(true)
        date = nonEmpty("ts").flatMap(Date.parseRFC3339)
    }
}
