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
    public let closed: Bool
    public let asks: Bool
    public let notify: NotificationPolicy?
    public let error: String?
    public let pending: [String]
    public let notifyEffective: NotificationPolicy

    public init(
        id: String, workspace: String = "", worktree: String = "", agent: String = "", agentSessionId: String = "",
        parentSession: String? = nil, advisor: String? = nil, fork: String? = nil, handoffFrom: String? = nil, handoffTo: String? = nil, importedFrom: String? = nil,
        title: String = "", titleSet: Bool = false, status: AgentSessionStatus = .other, mode: String = "", finish: AgentFinish = .ask, afk: Bool = false,
        createdAt: Date = .distantPast, updatedAt: Date? = nil, closed: Bool = false, asks: Bool = false,
        notify: NotificationPolicy? = nil, error: String? = nil, pending: [String] = [], notifyEffective: NotificationPolicy = .all
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
        self.closed = closed
        self.asks = asks
        self.notify = notify
        self.error = error
        self.pending = pending
        self.notifyEffective = notifyEffective
    }

    enum CodingKeys: String, CodingKey {
        case id, workspace, worktree, agent, agentSessionId, parentSession, advisor, fork, handoffFrom, handoffTo, importedFrom
        case title, titleSet, status, mode, finish, afk, createdAt, updatedAt, closed, asks, notify, error, pending, notifyEffective
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
            closed: try flag(.closed),
            asks: try flag(.asks),
            notify: try optional(.notify).map(NotificationPolicy.init),
            error: try optional(.error),
            pending: try c.decodeIfPresent([String].self, forKey: .pending) ?? [],
            notifyEffective: try c.decodeIfPresent(NotificationPolicy.self, forKey: .notifyEffective) ?? .all
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
        try c.encode(closed, forKey: .closed)
        try c.encode(asks, forKey: .asks)
        try c.encodeIfPresent(notify, forKey: .notify)
        try c.encodeIfPresent(error, forKey: .error)
        try c.encode(pending, forKey: .pending)
        try c.encode(notifyEffective, forKey: .notifyEffective)
    }
}
