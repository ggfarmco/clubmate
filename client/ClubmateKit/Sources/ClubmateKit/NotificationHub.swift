import Foundation

import ClubmateProtocol

public struct HubNotification: Sendable, Equatable, Codable {
    public enum Source: Sendable, Equatable, Codable {
        case bus
        case agentsSection
    }

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
        guard let posted = NotifyPosted(payload), let workspace = posted.workspace else { return nil }
        return HubNotification(
            id: posted.id,
            workspace: workspace,
            session: posted.session,
            owner: posted.owner,
            title: posted.title,
            message: posted.message,
            kind: posted.kind,
            viaAgent: posted.viaAgent,
            date: posted.date
        )
    }

    public var isAgentSession: Bool {
        session.map(AgentSessionID.matches) == true
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
        if AgentSessionID.matches(session) || owner == AgentNotifier.section { return "Agent" }
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
    public let panel: SDUINode?

    public init(appActive: Bool, workspace: String?, sidebar: SDUINode? = nil, main: SDUINode? = nil, panel: SDUINode? = nil) {
        self.appActive = appActive
        self.workspace = workspace
        self.sidebar = sidebar
        self.main = main
        self.panel = panel
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
            || panel?.contains(idPrefix: session + "/") == true
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
        let section = owner ?? (AgentSessionID.matches(session) ? AgentNotifier.section : nil)
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
