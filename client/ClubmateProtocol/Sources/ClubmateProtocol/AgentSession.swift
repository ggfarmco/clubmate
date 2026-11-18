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
    public let reopen: FailureAction?
    public let modeLabel: String?
    public let autoClosed: Bool
    public let review: Bool
    public let asksParent: Bool
    public let service: Bool
    public let group: SessionGroup
    public let paused: Bool
    public let pausedUntil: Date?
    public let active: Bool
    public let kind: String?
    public let run: SessionRun?
    public let reviewProgress: ReviewProgress?
    public let openCount: Int
    public let openDecisions: Int
    public let openOffers: Int
    public let openSince: Date?
    public let mayAsk: Bool
    public let repo: String?
    public let taskRows: [RepoRow]
    public let taskBadge: String?
    public let archivable: Bool
    public let taskFinished: Bool
    public let requests: [String]
    public let turns: Int
    public let rowDetail: String?
    public let limitShare: [String: Double]
    public let shareLabel: String?
    public let shareWords: [String: String]
    public let shareDetail: String?
    public let listKey: String?
    public let hidden: Bool
    public let rowUnder: String?
    public let gateRound: GateRound?
    public let landing: SessionLanding?
    public let turnHeld: Bool
    public let heldRound: HeldRound?
    public let aside: AsideInfo?
    public let canAside: Bool
    public let forkedFrom: String?
    public let composerDraft: ComposerDraft?
    public let listGroupKey: String?
    public let listGroupLabel: String?
    public let listFilters: [String]?
    public let listOpen: Int
    public let inboxKey: String?

    public init(
        id: String, workspace: String = "", worktree: String = "", agent: String = "", agentSessionId: String = "",
        parentSession: String? = nil, advisor: String? = nil, fork: String? = nil, handoffFrom: String? = nil, handoffTo: String? = nil, importedFrom: String? = nil,
        title: String = "", titleSet: Bool = false, status: AgentSessionStatus = .other, mode: String = "", finish: AgentFinish = .ask, afk: Bool = false,
        createdAt: Date = .distantPast, updatedAt: Date? = nil, asks: Bool = false,
        notify: NotificationPolicy? = nil, error: String? = nil, pending: [String] = [], notifyEffective: NotificationPolicy = .all,
        childStatus: AgentSessionStatus? = nil, pendingSummary: PendingSummary? = nil, question: String? = nil, turnStartedAt: Date? = nil, needsYou: Bool? = nil,
        needsYouSince: Date? = nil, activity: SessionActivity? = nil, failure: SessionFailure? = nil, reopen: FailureAction? = nil, modeLabel: String? = nil,
        autoClosed: Bool = false, review: Bool = false, asksParent: Bool = false, service: Bool = false, group: SessionGroup? = nil, paused: Bool = false, pausedUntil: Date? = nil, active: Bool = false, kind: String? = nil, run: SessionRun? = nil,
        reviewProgress: ReviewProgress? = nil, openCount: Int = 0, openDecisions: Int = 0, openOffers: Int = 0, openSince: Date? = nil, mayAsk: Bool = false,
        repo: String? = nil, taskRows: [RepoRow] = [], taskBadge: String? = nil, archivable: Bool = false, taskFinished: Bool = false, requests: [String] = [], turns: Int = 0, rowDetail: String? = nil,
        limitShare: [String: Double] = [:], shareLabel: String? = nil, shareWords: [String: String] = [:], shareDetail: String? = nil, listKey: String? = nil,
        hidden: Bool = false, rowUnder: String? = nil, gateRound: GateRound? = nil, landing: SessionLanding? = nil, turnHeld: Bool = false, heldRound: HeldRound? = nil,
        aside: AsideInfo? = nil, canAside: Bool = false, forkedFrom: String? = nil, composerDraft: ComposerDraft? = nil,
        listGroupKey: String? = nil, listGroupLabel: String? = nil, listFilters: [String]? = nil, listOpen: Int = 0, inboxKey: String? = nil
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
        self.reopen = reopen
        self.modeLabel = modeLabel
        self.autoClosed = autoClosed
        self.review = review
        self.asksParent = asksParent
        self.service = service
        self.group = group ?? (status == .closed ? .closed : self.needsYou ? .needsInput : service ? .service : status == .running ? .running : .idle)
        self.paused = paused
        self.pausedUntil = pausedUntil
        self.active = active
        self.kind = kind
        self.run = run
        self.reviewProgress = reviewProgress
        self.openCount = openCount
        self.openDecisions = openDecisions
        self.openOffers = openOffers
        self.openSince = openSince
        self.mayAsk = mayAsk
        self.repo = repo
        self.taskRows = taskRows
        self.taskBadge = taskBadge
        self.archivable = archivable
        self.taskFinished = taskFinished
        self.rowDetail = rowDetail
        self.requests = requests
        self.turns = turns
        self.limitShare = limitShare
        self.shareLabel = shareLabel
        self.shareWords = shareWords
        self.shareDetail = shareDetail
        self.listKey = listKey
        self.hidden = hidden
        self.rowUnder = rowUnder
        self.gateRound = gateRound
        self.landing = landing
        self.turnHeld = turnHeld
        self.heldRound = heldRound
        self.aside = aside
        self.canAside = canAside
        self.forkedFrom = forkedFrom
        self.composerDraft = composerDraft
        self.listGroupKey = listGroupKey
        self.listGroupLabel = listGroupLabel
        self.listFilters = listFilters
        self.listOpen = listOpen
        self.inboxKey = inboxKey
    }

    public var isClosed: Bool { status == .closed }

    enum CodingKeys: String, CodingKey {
        case id, workspace, worktree, agent, agentSessionId, parentSession, advisor, fork, handoffFrom, handoffTo, importedFrom
        case title, titleSet, status, mode, finish, afk, createdAt, updatedAt, asks, notify, error, pending, notifyEffective, childStatus
        case pendingSummary, question, turnStartedAt, needsYou, needsYouSince, activity, failure, reopen, modeLabel, autoClosed, review, asksParent, service, group, paused, pausedUntil, active, kind, run, reviewProgress, openCount, openDecisions, openOffers, openSince, mayAsk, repo, taskRows, taskBadge, archivable, taskFinished, requests, turns, rowDetail
        case limitShare, shareLabel, shareWords, shareDetail, listKey
        case hidden, rowUnder, gateRound, landing, turnHeld, heldRound
        case aside, canAside, forkedFrom, composerDraft
        case listGroupKey, listGroupLabel, listFilters, listOpen, inboxKey
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
            reopen: try? c.decodeIfPresent(FailureAction.self, forKey: .reopen),
            modeLabel: try optional(.modeLabel),
            autoClosed: try flag(.autoClosed),
            review: try flag(.review),
            asksParent: try flag(.asksParent),
            service: try flag(.service),
            group: try c.decodeIfPresent(SessionGroup.self, forKey: .group),
            paused: try flag(.paused),
            pausedUntil: (try? c.decodeIfPresent(Double.self, forKey: .pausedUntil)).flatMap { $0.map { Date(timeIntervalSince1970: $0) } },
            active: try flag(.active),
            kind: try optional(.kind),
            run: try? c.decodeIfPresent(SessionRun.self, forKey: .run),
            reviewProgress: try? c.decodeIfPresent(ReviewProgress.self, forKey: .reviewProgress),
            openCount: try c.decodeIfPresent(Int.self, forKey: .openCount) ?? 0,
            openDecisions: try c.decodeIfPresent(Int.self, forKey: .openDecisions) ?? 0,
            openOffers: try c.decodeIfPresent(Int.self, forKey: .openOffers) ?? 0,
            openSince: date(.openSince),
            mayAsk: try flag(.mayAsk),
            repo: try optional(.repo),
            taskRows: (try? c.decodeIfPresent([RepoRow].self, forKey: .taskRows)) ?? [],
            taskBadge: try optional(.taskBadge),
            archivable: try flag(.archivable),
            taskFinished: try flag(.taskFinished),
            requests: try c.decodeIfPresent([String].self, forKey: .requests) ?? [],
            turns: try c.decodeIfPresent(Int.self, forKey: .turns) ?? 0,
            rowDetail: try optional(.rowDetail),
            limitShare: (try? c.decodeIfPresent([String: Double].self, forKey: .limitShare)) ?? [:],
            shareLabel: try optional(.shareLabel),
            shareWords: (try? c.decodeIfPresent([String: String].self, forKey: .shareWords)) ?? [:],
            shareDetail: try optional(.shareDetail),
            listKey: try optional(.listKey),
            hidden: try flag(.hidden),
            rowUnder: try optional(.rowUnder),
            gateRound: try? c.decodeIfPresent(GateRound.self, forKey: .gateRound),
            landing: try? c.decodeIfPresent(SessionLanding.self, forKey: .landing),
            turnHeld: try flag(.turnHeld),
            heldRound: try? c.decodeIfPresent(HeldRound.self, forKey: .heldRound),
            aside: try? c.decodeIfPresent(AsideInfo.self, forKey: .aside),
            canAside: try flag(.canAside),
            forkedFrom: try optional(.forkedFrom),
            composerDraft: try? c.decodeIfPresent(ComposerDraft.self, forKey: .composerDraft),
            listGroupKey: try optional(.listGroupKey),
            listGroupLabel: try optional(.listGroupLabel),
            listFilters: try c.decodeIfPresent([String].self, forKey: .listFilters),
            listOpen: try c.decodeIfPresent(Int.self, forKey: .listOpen) ?? 0,
            inboxKey: try optional(.inboxKey)
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
        try c.encodeIfPresent(reopen, forKey: .reopen)
        try c.encodeIfPresent(modeLabel, forKey: .modeLabel)
        try c.encode(autoClosed, forKey: .autoClosed)
        try c.encode(review, forKey: .review)
        try c.encode(asksParent, forKey: .asksParent)
        try c.encode(service, forKey: .service)
        try c.encode(group, forKey: .group)
        try c.encode(paused, forKey: .paused)
        try c.encodeIfPresent(pausedUntil?.timeIntervalSince1970, forKey: .pausedUntil)
        try c.encode(active, forKey: .active)
        try c.encodeIfPresent(kind, forKey: .kind)
        try c.encodeIfPresent(run, forKey: .run)
        try c.encodeIfPresent(reviewProgress, forKey: .reviewProgress)
        try c.encode(openCount, forKey: .openCount)
        try c.encode(openDecisions, forKey: .openDecisions)
        try c.encode(openOffers, forKey: .openOffers)
        try c.encodeIfPresent(openSince.map(formatter.string(from:)), forKey: .openSince)
        try c.encode(mayAsk, forKey: .mayAsk)
        try c.encodeIfPresent(repo, forKey: .repo)
        try c.encode(taskRows, forKey: .taskRows)
        try c.encodeIfPresent(taskBadge, forKey: .taskBadge)
        try c.encode(archivable, forKey: .archivable)
        try c.encode(taskFinished, forKey: .taskFinished)
        try c.encodeIfPresent(rowDetail, forKey: .rowDetail)
        try c.encode(requests, forKey: .requests)
        try c.encode(turns, forKey: .turns)
        if !limitShare.isEmpty { try c.encode(limitShare, forKey: .limitShare) }
        try c.encodeIfPresent(shareLabel, forKey: .shareLabel)
        if !shareWords.isEmpty { try c.encode(shareWords, forKey: .shareWords) }
        try c.encodeIfPresent(shareDetail, forKey: .shareDetail)
        try c.encodeIfPresent(listKey, forKey: .listKey)
        try c.encodeIfPresent(aside, forKey: .aside)
        if canAside { try c.encode(canAside, forKey: .canAside) }
        try c.encodeIfPresent(forkedFrom, forKey: .forkedFrom)
        try c.encodeIfPresent(composerDraft, forKey: .composerDraft)
        try c.encodeIfPresent(listGroupKey, forKey: .listGroupKey)
        try c.encodeIfPresent(listGroupLabel, forKey: .listGroupLabel)
        try c.encodeIfPresent(listFilters, forKey: .listFilters)
        if listOpen > 0 { try c.encode(listOpen, forKey: .listOpen) }
        try c.encodeIfPresent(inboxKey, forKey: .inboxKey)
        if hidden { try c.encode(hidden, forKey: .hidden) }
        try c.encodeIfPresent(rowUnder, forKey: .rowUnder)
        try c.encodeIfPresent(gateRound, forKey: .gateRound)
        try c.encodeIfPresent(landing, forKey: .landing)
        if turnHeld { try c.encode(turnHeld, forKey: .turnHeld) }
        try c.encodeIfPresent(heldRound, forKey: .heldRound)
    }
}

public struct HeldRound: Codable, Sendable, Hashable {
    public let text: String
    public var run: String?

    public init(text: String, run: String? = nil) {
        self.text = text
        self.run = run
    }
}

public struct GateRound: Codable, Sendable, Hashable {
    public let label: String

    public init(label: String) {
        self.label = label
    }
}

public struct SessionLanding: Codable, Sendable, Hashable {
    public let verdict: String
    public let words: String
    public let tone: String?
    public var line: String?

    public init(verdict: String, words: String, tone: String? = nil, line: String? = nil) {
        self.verdict = verdict
        self.words = words
        self.tone = tone
        self.line = line
    }
}

public struct RepoRow: Codable, Sendable, Hashable, Identifiable {
    public let name: String
    public var path: String?
    public var session: String?
    public var word: String?
    public var tone: String?
    public var connect: Bool?

    public init(name: String, path: String? = nil, session: String? = nil, word: String? = nil, tone: String? = nil, connect: Bool? = nil) {
        self.name = name
        self.path = path
        self.session = session
        self.word = word
        self.tone = tone
        self.connect = connect
    }

    public var id: String { path ?? name }
}

public struct ReviewProgress: Codable, Sendable, Hashable {
    public let text: String

    public init(text: String) {
        self.text = text
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

public struct SessionRun: Codable, Sendable, Hashable {
    public let id: String
    public let title: String
    public let number: Int?

    public init(id: String, title: String = "", number: Int? = nil) {
        self.id = id
        self.title = title
        self.number = number
    }
}

public struct PendingSummary: Codable, Sendable, Hashable {
    public enum Kind: String, Codable, Sendable {
        case permission
        case question
        case connect
        case land
        case failed
    }

    public let kind: Kind
    public let text: String
    public let tool: String?
    public let role: String?
    public let onMac: Bool?
    public let lead: String?
    public let subject: String?
    public let at: Date

    public init(kind: Kind, text: String, tool: String? = nil, role: String? = nil, onMac: Bool? = nil, lead: String? = nil, subject: String? = nil, at: Date) {
        self.kind = kind
        self.text = text
        self.tool = tool
        self.role = role
        self.onMac = onMac
        self.lead = lead
        self.subject = subject
        self.at = at
    }

    public var isCommand: Bool {
        kind == .permission && tool == "execute"
    }

    enum CodingKeys: String, CodingKey {
        case kind, text, tool, role, onMac, lead, subject, at
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let at = try c.decodeIfPresent(String.self, forKey: .at).flatMap(Date.parseRFC3339)
        self.init(
            kind: try c.decode(Kind.self, forKey: .kind),
            text: try c.decodeIfPresent(String.self, forKey: .text) ?? "",
            tool: try c.decodeIfPresent(String.self, forKey: .tool).flatMap { $0.isEmpty ? nil : $0 },
            role: try c.decodeIfPresent(String.self, forKey: .role).flatMap { $0.isEmpty ? nil : $0 },
            onMac: try c.decodeIfPresent(Bool.self, forKey: .onMac),
            lead: try c.decodeIfPresent(String.self, forKey: .lead).flatMap { $0.isEmpty ? nil : $0 },
            subject: try c.decodeIfPresent(String.self, forKey: .subject).flatMap { $0.isEmpty ? nil : $0 },
            at: at ?? .distantPast
        )
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        try c.encode(kind, forKey: .kind)
        try c.encode(text, forKey: .text)
        try c.encodeIfPresent(onMac, forKey: .onMac)
        try c.encodeIfPresent(lead, forKey: .lead)
        try c.encodeIfPresent(subject, forKey: .subject)
        try c.encodeIfPresent(tool, forKey: .tool)
        try c.encodeIfPresent(role, forKey: .role)
        try c.encode(formatter.string(from: at), forKey: .at)
    }
}

extension AgentSession {
    public static func isQuestion(_ request: String) -> Bool {
        request.hasPrefix("elicitation-")
    }
}

public enum ListFilter: String, CaseIterable, Sendable, Identifiable {
    case all
    case needs
    case open
    case running

    public var id: String { rawValue }
}

extension Sequence where Element == AgentSession {
    public func listed(in filter: ListFilter) -> Int {
        reduce(0) { $0 + ($1.listFilters?.contains(filter.rawValue) == true ? 1 : 0) }
    }

    public var listedOpen: Int {
        reduce(0) { $0 + $1.listOpen }
    }
}
