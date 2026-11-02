import Foundation

public struct AgentSession: Codable, Sendable, Hashable, Identifiable {
    public let id: String
    public let workspace: String
    public let worktree: String
    public let agent: String
    public let agentSessionId: String
    public let parentSession: String?
    public let advisor: String?
    public let fork: String?
    public let handoffFrom: String?
    public let handoffTo: String?
    public let importedFrom: String?
    public let title: String
    public let titleSet: Bool
    public let status: AgentSessionStatus
    public let mode: String
    public let finish: AgentFinish
    public let afk: Bool
    public let createdAt: Date
    public let updatedAt: Date
    public let asks: Bool
    public let notify: NotificationPolicy?
    public let error: String?
    public let pending: [String]
    public let pendingSummary: PendingSummary?
    public let question: String?
    public let turnStartedAt: Date?
    public let notifyEffective: NotificationPolicy
    public let childStatus: AgentSessionStatus?
    public let needsYou: Bool
    public let needsYouSince: Date?
    public let activity: SessionActivity?
    public let failure: SessionFailure?
    public let modeLabel: String?
    public let autoClosed: Bool
    public let review: Bool
    public let asksParent: Bool

    public init(
        id: String, workspace: String = "", worktree: String = "", agent: String = "", agentSessionId: String = "",
        parentSession: String? = nil, advisor: String? = nil, fork: String? = nil, handoffFrom: String? = nil, handoffTo: String? = nil, importedFrom: String? = nil,
        title: String = "", titleSet: Bool = false, status: AgentSessionStatus = .other, mode: String = "", finish: AgentFinish = .ask, afk: Bool = false,
        createdAt: Date = .distantPast, updatedAt: Date? = nil, asks: Bool = false,
        notify: NotificationPolicy? = nil, error: String? = nil, pending: [String] = [], notifyEffective: NotificationPolicy = .all,
        childStatus: AgentSessionStatus? = nil, pendingSummary: PendingSummary? = nil, question: String? = nil, turnStartedAt: Date? = nil, needsYou: Bool? = nil,
        needsYouSince: Date? = nil, activity: SessionActivity? = nil, failure: SessionFailure? = nil, modeLabel: String? = nil,
        autoClosed: Bool = false, review: Bool = false, asksParent: Bool = false
    ) {
        self.id = id
        self.workspace = workspace
        self.worktree = worktree
        self.agent = agent
        self.agentSessionId = agentSessionId
        self.parentSession = parentSession
        self.advisor = advisor
        self.fork = fork
        self.handoffFrom = handoffFrom
        self.handoffTo = handoffTo
        self.importedFrom = importedFrom
        self.title = title
        self.titleSet = titleSet
        self.status = status
        self.mode = mode
        self.finish = finish
        self.afk = afk
        self.createdAt = createdAt
        self.updatedAt = updatedAt ?? createdAt
        self.asks = asks
        self.notify = notify
        self.error = error
        self.pending = pending
        self.pendingSummary = pendingSummary
        self.question = question
        self.turnStartedAt = turnStartedAt
        self.notifyEffective = notifyEffective
        self.childStatus = childStatus
        self.needsYou = needsYou ?? (status != .closed && (status == .waiting || !pending.isEmpty || status == .done && asks && parentSession == nil))
        self.needsYouSince = needsYouSince
        self.activity = activity
        self.failure = failure
        self.modeLabel = modeLabel
        self.autoClosed = autoClosed
        self.review = review
        self.asksParent = asksParent
    }

    public var isClosed: Bool { status == .closed }

    enum CodingKeys: String, CodingKey {
        case id, workspace, worktree, agent, agentSessionId, parentSession, advisor, fork, handoffFrom, handoffTo, importedFrom
        case title, titleSet, status, mode, finish, afk, createdAt, updatedAt, asks, notify, error, pending, notifyEffective, childStatus
        case pendingSummary, question, turnStartedAt, needsYou, needsYouSince, activity, failure, modeLabel, autoClosed, review, asksParent
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let text = { (key: CodingKeys) in try c.decodeIfPresent(String.self, forKey: key) ?? "" }
        let optional = { (key: CodingKeys) in try c.decodeIfPresent(String.self, forKey: key).flatMap { $0.isEmpty ? nil : $0 } }
        let flag = { (key: CodingKeys) in try c.decodeIfPresent(Bool.self, forKey: key) ?? false }
        let date = { (key: CodingKeys) in (try? c.decodeIfPresent(String.self, forKey: key)).flatMap { $0.flatMap(Date.parseRFC3339) } }
        let created = date(.createdAt) ?? .distantPast
        self.init(
            id: try c.decode(String.self, forKey: .id),
            workspace: try text(.workspace),
            worktree: try text(.worktree),
            agent: try text(.agent),
            agentSessionId: try text(.agentSessionId),
            parentSession: try optional(.parentSession),
            advisor: try optional(.advisor),
            fork: try optional(.fork),
            handoffFrom: try optional(.handoffFrom),
            handoffTo: try optional(.handoffTo),
            importedFrom: try optional(.importedFrom),
            title: try text(.title),
            titleSet: try flag(.titleSet),
            status: try c.decodeIfPresent(AgentSessionStatus.self, forKey: .status) ?? .other,
            mode: try text(.mode),
            finish: try c.decodeIfPresent(AgentFinish.self, forKey: .finish) ?? .ask,
            afk: try flag(.afk),
            createdAt: created,
            updatedAt: date(.updatedAt) ?? created,
            asks: try flag(.asks),
            notify: try optional(.notify).map(NotificationPolicy.init),
            error: try optional(.error),
            pending: try c.decodeIfPresent([String].self, forKey: .pending) ?? [],
            notifyEffective: try c.decodeIfPresent(NotificationPolicy.self, forKey: .notifyEffective) ?? .all,
            childStatus: try c.decodeIfPresent(AgentSessionStatus.self, forKey: .childStatus),
            pendingSummary: try? c.decodeIfPresent(PendingSummary.self, forKey: .pendingSummary),
            question: try optional(.question),
            turnStartedAt: date(.turnStartedAt),
            needsYou: try c.decodeIfPresent(Bool.self, forKey: .needsYou),
            needsYouSince: date(.needsYouSince),
            activity: try? c.decodeIfPresent(SessionActivity.self, forKey: .activity),
            failure: try? c.decodeIfPresent(SessionFailure.self, forKey: .failure),
            modeLabel: try optional(.modeLabel),
            autoClosed: try flag(.autoClosed),
            review: try flag(.review),
            asksParent: try flag(.asksParent)
        )
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        try c.encode(id, forKey: .id)
        try c.encode(workspace, forKey: .workspace)
        try c.encode(worktree, forKey: .worktree)
        try c.encode(agent, forKey: .agent)
        try c.encode(agentSessionId, forKey: .agentSessionId)
        try c.encodeIfPresent(parentSession, forKey: .parentSession)
        try c.encodeIfPresent(advisor, forKey: .advisor)
        try c.encodeIfPresent(fork, forKey: .fork)
        try c.encodeIfPresent(handoffFrom, forKey: .handoffFrom)
        try c.encodeIfPresent(handoffTo, forKey: .handoffTo)
        try c.encodeIfPresent(importedFrom, forKey: .importedFrom)
        try c.encode(title, forKey: .title)
        try c.encode(titleSet, forKey: .titleSet)
        try c.encode(status, forKey: .status)
        try c.encode(mode, forKey: .mode)
        try c.encode(finish, forKey: .finish)
        try c.encode(afk, forKey: .afk)
        try c.encode(formatter.string(from: createdAt), forKey: .createdAt)
        try c.encode(formatter.string(from: updatedAt), forKey: .updatedAt)
        try c.encode(asks, forKey: .asks)
        try c.encodeIfPresent(notify, forKey: .notify)
        try c.encodeIfPresent(error, forKey: .error)
        try c.encode(pending, forKey: .pending)
        try c.encode(notifyEffective, forKey: .notifyEffective)
        try c.encodeIfPresent(childStatus, forKey: .childStatus)
        try c.encodeIfPresent(pendingSummary, forKey: .pendingSummary)
        try c.encodeIfPresent(question, forKey: .question)
        try c.encodeIfPresent(turnStartedAt.map(formatter.string(from:)), forKey: .turnStartedAt)
        try c.encode(needsYou, forKey: .needsYou)
        try c.encodeIfPresent(needsYouSince.map(formatter.string(from:)), forKey: .needsYouSince)
        try c.encodeIfPresent(activity, forKey: .activity)
        try c.encodeIfPresent(failure, forKey: .failure)
        try c.encodeIfPresent(modeLabel, forKey: .modeLabel)
        try c.encode(autoClosed, forKey: .autoClosed)
        try c.encode(review, forKey: .review)
        try c.encode(asksParent, forKey: .asksParent)
    }
}

public struct SessionFailure: Codable, Sendable, Hashable {
    public let word: String
    public let badge: String
    public let line: String
    public let actions: [FailureAction]

    public init(word: String, badge: String, line: String, actions: [FailureAction] = []) {
        self.word = word
        self.badge = badge
        self.line = line
        self.actions = actions
    }

    enum CodingKeys: String, CodingKey {
        case word, badge, line, actions
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        word = try c.decode(String.self, forKey: .word)
        badge = try c.decode(String.self, forKey: .badge)
        line = try c.decode(String.self, forKey: .line)
        actions = (try? c.decodeIfPresent([FailureAction].self, forKey: .actions)) ?? []
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(word, forKey: .word)
        try c.encode(badge, forKey: .badge)
        try c.encode(line, forKey: .line)
        if !actions.isEmpty { try c.encode(actions, forKey: .actions) }
    }
}

public struct FailureAction: Codable, Sendable, Hashable, Identifiable {
    public let id: String
    public let label: String
    public let icon: String
    public let command: String?
    public let args: [String: String]?

    public init(id: String, label: String, icon: String, command: String? = nil, args: [String: String]? = nil) {
        self.id = id
        self.label = label
        self.icon = icon
        self.command = command
        self.args = args
    }
}

public struct SessionActivity: Codable, Sendable, Hashable {
    public let title: String
    public let at: Date

    public init(title: String, at: Date) {
        self.title = title
        self.at = at
    }

    enum CodingKeys: String, CodingKey {
        case title, at
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            title: try c.decodeIfPresent(String.self, forKey: .title) ?? "",
            at: try c.decodeIfPresent(String.self, forKey: .at).flatMap(Date.parseRFC3339) ?? .distantPast
        )
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        try c.encode(title, forKey: .title)
        try c.encode(formatter.string(from: at), forKey: .at)
    }
}

public struct PendingSummary: Codable, Sendable, Hashable {
    public enum Kind: String, Codable, Sendable {
        case permission
        case question
    }

    public let kind: Kind
    public let text: String
    public let tool: String?
    public let at: Date

    public init(kind: Kind, text: String, tool: String? = nil, at: Date) {
        self.kind = kind
        self.text = text
        self.tool = tool
        self.at = at
    }

    public var isCommand: Bool {
        kind == .permission && tool == "execute"
    }

    enum CodingKeys: String, CodingKey {
        case kind, text, tool, at
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let at = try c.decodeIfPresent(String.self, forKey: .at).flatMap(Date.parseRFC3339)
        self.init(
            kind: try c.decode(Kind.self, forKey: .kind),
            text: try c.decodeIfPresent(String.self, forKey: .text) ?? "",
            tool: try c.decodeIfPresent(String.self, forKey: .tool).flatMap { $0.isEmpty ? nil : $0 },
            at: at ?? .distantPast
        )
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        try c.encode(kind, forKey: .kind)
        try c.encode(text, forKey: .text)
        try c.encodeIfPresent(tool, forKey: .tool)
        try c.encode(formatter.string(from: at), forKey: .at)
    }
}

extension AgentSession {
    public static func isQuestion(_ request: String) -> Bool {
        request.hasPrefix("elicitation-")
    }
}
