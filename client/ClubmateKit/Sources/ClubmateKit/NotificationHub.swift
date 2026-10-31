import Foundation

public enum NotificationKind: String, Sendable, CaseIterable, Codable {
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

public enum NotificationPolicy: String, Sendable, Equatable {
    case all
    case needsInput = "needs-input"
    case muted

    public func allows(_ kind: NotificationKind) -> Bool {
        switch self {
        case .all: true
        case .needsInput: kind == .needsInput || kind == .permission
        case .muted: false
        }
    }
}

public struct HubNotification: Sendable, Equatable, Codable {
    public enum Source: Sendable, Equatable, Codable {
        case bus
        case agentsSection
    }

    public static let topic = "notify.posted"

    public let id: String
    public let workspace: String
    public let session: String?
    public let owner: String?
    public let title: String
    public let message: String
    public let kind: NotificationKind
    public let viaAgent: Bool
    public let source: Source
    public let date: Date?

    public init(id: String, workspace: String, session: String? = nil, owner: String? = nil, title: String = "", message: String, kind: NotificationKind = .info, viaAgent: Bool = false, source: Source = .bus, date: Date? = nil) {
        self.id = id
        self.workspace = workspace
        self.session = session
        self.owner = owner
        self.title = title
        self.message = message
        self.kind = kind
        self.viaAgent = viaAgent
        self.source = source
        self.date = date
    }

    public init(_ alert: AgentAlert) {
        self.init(id: alert.id, workspace: alert.workspace, session: alert.session, owner: AgentNotifier.section, title: alert.title, message: alert.body, kind: .permission, source: .agentsSection)
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
            kind: fields["kind"]?.stringValue.flatMap(NotificationKind.init(rawValue:)) ?? .info,
            viaAgent: fields["agent"] == .bool(true),
            date: fields["ts"]?.stringValue.flatMap(Self.parseDate)
        )
    }

    static func parseDate(_ text: String) -> Date? {
        let millis = text.replacingOccurrences(of: #"\.(\d{1,3})\d*"#, with: ".$1", options: .regularExpression)
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fractional.date(from: millis) ?? ISO8601DateFormatter().date(from: millis)
    }

    public var isAgentSession: Bool {
        session?.hasPrefix(NotificationHub.agentSessionPrefix) == true
    }

    public var what: String {
        title.isEmpty ? kind.label : title
    }

    public func source(workspaceName: String?) -> String {
        var parts = [workspaceName ?? workspace]
        if let session { parts.append(Self.sessionLabel(session: session, owner: owner)) }
        if viaAgent { parts.append("via agent") }
        return parts.joined(separator: " · ")
    }

    static func sessionLabel(session: String, owner: String?) -> String {
        if session.hasPrefix(NotificationHub.agentSessionPrefix) || owner == AgentNotifier.section { return "Agent" }
        switch owner {
        case TerminalsSection.id: return "Terminal"
        case CodeSection.id: return "Editor"
        case let owner? where !owner.isEmpty: return owner
        default: return "Session"
        }
    }

    public var subtitle: String {
        viaAgent ? "via agent" : ""
    }

    var coveredByAgentsSection: Bool {
        source == .bus && !viaAgent && (kind == .needsInput || kind == .permission) && isAgentSession
    }

    public func headline(workspaceName: String?) -> String {
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
        return main?.showsSurface(session) == true
            || sidebar?.showsSurface(session) == true
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
    public static let liveWindow: TimeInterval = 30

    public var inbox: NotificationInbox
    public var waiting = AgentWaiting()

    public init(inbox: NotificationInbox = NotificationInbox()) {
        self.inbox = inbox
    }

    public var bellCount: Int {
        inbox.unhandled.union(waiting.alerts.map(\.id)).count
    }

    public var unseenTotal: Int {
        inbox.unseenTotal
    }

    public func unseen(_ workspace: String) -> Int {
        inbox.unseen(workspace)
    }

    public var pinned: [InboxPin] {
        inbox.pinned(waiting: waiting.alerts)
    }

    public mutating func receive(_ notification: HubNotification, focus: HubFocus, policy: NotificationPolicy = .all, now: Date = Date()) -> Bool {
        if notification.coveredByAgentsSection { return false }
        let date = notification.date ?? now
        guard policy.allows(notification.kind) else {
            if notification.source == .bus { inbox.add(notification, at: date, handled: true, now: now) }
            return false
        }
        guard now.timeIntervalSince(date) <= Self.liveWindow else {
            inbox.add(notification, at: date, handled: false, now: now)
            return false
        }
        if focus.shows(workspace: notification.workspace, session: notification.session) {
            if notification.source == .bus { inbox.add(notification, at: date, handled: true, now: now) }
            return false
        }
        return inbox.add(notification, at: date, handled: false, now: now)
    }

    public mutating func focused(_ focus: HubFocus) {
        if let front = focus.front { inbox.markSeen(workspace: front) }
        inbox.handleShown(focus)
    }

    public mutating func keep(_ workspaces: Set<String>) {
        inbox.keep(workspaces)
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

    static let agentSessionPrefix = "agt_"
}

public enum ClaudeHooks {
    public static func snippet(command: String) -> String {
        """
        {
          "hooks": {
            "Notification": [
              {
                "hooks": [
                  {
                    "type": "command",
                    "command": "jq -r '.message // empty' | \(command) notify --quiet --kind needs-input --title Claude -"
                  }
                ]
              }
            ],
            "Stop": [
              {
                "hooks": [
                  {
                    "type": "command",
                    "command": "\(command) notify --quiet --kind done --title Claude Finished"
                  }
                ]
              }
            ]
          }
        }

        """
    }
}

extension SDUINode {
    func showsSurface(_ session: String) -> Bool {
        if type == "surface", string("sessionId") == session { return true }
        return children.contains { $0.showsSurface(session) }
    }

    func contains(idPrefix: String) -> Bool {
        if id?.hasPrefix(idPrefix) == true { return true }
        return children.contains { $0.contains(idPrefix: idPrefix) }
    }

    func selects(_ value: String) -> Bool {
        if type == "list" || type == "tree", string("selected") == value { return true }
        return children.contains { $0.selects(value) }
    }
}
