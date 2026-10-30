import Foundation

public enum NotificationKind: String, Sendable, CaseIterable {
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
}

public struct HubNotification: Sendable, Equatable {
    public static let topic = "notify.posted"

    public let id: String
    public let workspace: String
    public let session: String?
    public let owner: String?
    public let title: String
    public let message: String
    public let kind: NotificationKind

    public init(id: String, workspace: String, session: String? = nil, owner: String? = nil, title: String = "", message: String, kind: NotificationKind = .info) {
        self.id = id
        self.workspace = workspace
        self.session = session
        self.owner = owner
        self.title = title
        self.message = message
        self.kind = kind
    }

    public init(_ alert: AgentAlert) {
        self.init(id: "\(alert.session)/\(alert.request)", workspace: alert.workspace, session: alert.session, owner: AgentNotifier.section, title: alert.title, message: alert.body, kind: .permission)
    }

    public static func posted(_ payload: JSONValue) -> HubNotification? {
        guard let fields = payload.objectValue, let id = fields["id"]?.stringValue, let workspace = fields["workspace"]?.stringValue,
              let message = fields["message"]?.stringValue else { return nil }
        let nonEmpty = { (key: String) in fields[key]?.stringValue.flatMap { $0.isEmpty ? nil : $0 } }
        return HubNotification(
            id: id,
            workspace: workspace,
            session: nonEmpty("session"),
            owner: nonEmpty("owner"),
            title: fields["title"]?.stringValue ?? "",
            message: message,
            kind: fields["kind"]?.stringValue.flatMap(NotificationKind.init(rawValue:)) ?? .info
        )
    }

    public func headline(workspaceName: String?) -> String {
        let what = title.isEmpty ? kind.label : title
        guard let workspaceName, !workspaceName.isEmpty else { return what }
        return "\(workspaceName) · \(what)"
    }
}

public struct HubFocus: Sendable, Equatable {
    public let appActive: Bool
    public let workspace: String?
    public let sidebar: SDUINode?
    public let main: SDUINode?

    public init(appActive: Bool, workspace: String?, sidebar: SDUINode? = nil, main: SDUINode? = nil) {
        self.appActive = appActive
        self.workspace = workspace
        self.sidebar = sidebar
        self.main = main
    }

    public var front: String? {
        appActive ? workspace : nil
    }

    public func shows(workspace: String, session: String?) -> Bool {
        guard front == workspace else { return false }
        guard let session else { return true }
        return main?.surfaceSessionID == session
            || main?.contains(idPrefix: session + "/") == true
            || sidebar?.selects(session) == true
    }
}

public struct HubReveal: Sendable, Equatable {
    public let section: String
    public let event: UIEvent?
    public let focusMain: Bool
}

public struct NotificationHub: Sendable {
    public private(set) var unread: [String: Int] = [:]

    public init() {}

    public var total: Int {
        unread.values.reduce(0, +)
    }

    public mutating func receive(_ notification: HubNotification, focus: HubFocus) -> Bool {
        if focus.shows(workspace: notification.workspace, session: notification.session) { return false }
        if focus.front != notification.workspace { unread[notification.workspace, default: 0] += 1 }
        return true
    }

    public mutating func seen(_ workspace: String) {
        unread[workspace] = nil
    }

    public mutating func keep(_ workspaces: Set<String>) {
        unread = unread.filter { workspaces.contains($0.key) }
    }

    public static func reveal(workspace: String, session: String, owner: String?) -> HubReveal? {
        let section = owner ?? (session.hasPrefix(agentSessionPrefix) ? AgentNotifier.section : nil)
        switch section {
        case nil:
            return nil
        case AgentNotifier.section:
            return HubReveal(section: AgentNotifier.section, event: AgentNotifier.selectEvent(workspace: workspace, session: session), focusMain: false)
        case TerminalsSection.id:
            return HubReveal(section: TerminalsSection.id, event: TerminalsSection.selectEvent(workspace: workspace, session: session), focusMain: true)
        case let section?:
            return HubReveal(section: section, event: nil, focusMain: true)
        }
    }

    private static let agentSessionPrefix = "agt_"
}

public enum ClaudeHooks {
    public static func snippet(command: String) -> String {
        let hooks: [String: [[String: [[String: String]]]]] = [
            "Notification": [["hooks": [["type": "command", "command": "jq -r .message | \(command) notify --kind needs-input --title Claude -"]]]],
            "Stop": [["hooks": [["type": "command", "command": "\(command) notify --kind done --title Claude Finished"]]]],
        ]
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        guard let data = try? encoder.encode(["hooks": hooks]) else { return "" }
        return String(decoding: data, as: UTF8.self)
    }
}

extension SDUINode {
    func contains(idPrefix: String) -> Bool {
        if id?.hasPrefix(idPrefix) == true { return true }
        return children.contains { $0.contains(idPrefix: idPrefix) }
    }

    func selects(_ value: String) -> Bool {
        if type == "list" || type == "tree", string("selected") == value { return true }
        return children.contains { $0.selects(value) }
    }
}
