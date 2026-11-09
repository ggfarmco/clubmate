import Foundation

import ClubmateProtocol

public enum SessionGlyph: Sendable, Equatable {
    case connect
    case permission
    case question
    case step
    case cap
    case childWaiting
    case asksParent
    case running
    case stopped
    case open
    case review
    case done
    case closed

    public var needsYou: Bool {
        self == .connect || self == .permission || self == .question || self == .step || self == .cap
    }
}

public struct SessionStateLine: Sendable, Equatable {
    public enum Tone: Sendable, Equatable {
        case primary
        case secondary
        case faint
        case needsYou
        case danger
    }

    public var lead: String?
    public var text: String
    public var mono = false
    public var tone: Tone = .secondary
    public var age: String?

    public init(lead: String? = nil, text: String, mono: Bool = false, tone: Tone = .secondary, age: String? = nil) {
        self.lead = lead
        self.text = text
        self.mono = mono
        self.tone = tone
        self.age = age
    }
}

extension AgentSession {
    public var glyph: SessionGlyph {
        if isClosed { return .closed }
        if isRun { return pending.contains { $0.hasPrefix(GraphRun.stepRequest) } ? .step : .cap }
        if needsYou {
            if pendingSummary?.kind == .connect { return .connect }
            if pendingSummary?.kind == .question, pending.contains(where: AgentSession.isQuestion) { return .question }
            return status == .waiting || !pending.isEmpty || answersOnMac ? .permission : .question
        }
        if asksParent { return .asksParent }
        if childStatus == .waiting { return .childWaiting }
        if displayStatus == .running { return .running }
        if status == .error { return .stopped }
        if openLabel != nil { return .open }
        if review { return .review }
        return .done
    }

    public var subagentLabel: String {
        afk ? "Subagent · AFK" : "Subagent"
    }

    public var askedQuestion: String? {
        if let summary = pendingSummary, summary.kind == .question, !summary.text.isEmpty { return summary.text }
        return question.flatMap { $0.isEmpty ? nil : $0 }
    }

    public func glyph(waitingChildren: Int) -> SessionGlyph {
        waitingChildren > 0 && !glyph.needsYou ? .childWaiting : glyph
    }

    public func stateLine(now: Date, waitingChildren: Int = 0) -> SessionStateLine {
        let age = { (date: Date) in ShortAge.since(date, now: now) }
        switch glyph(waitingChildren: waitingChildren) {
        case .connect:
            guard let summary = pendingSummary, let subject = summary.subject else {
                return SessionStateLine(text: pendingSummary?.text ?? SessionState.waitingForYou.label, tone: .needsYou, age: pendingSummary.map { age($0.at) })
            }
            return SessionStateLine(lead: summary.lead, text: subject, tone: .primary, age: age(summary.at))
        case .step:
            if let role = pendingSummary?.role {
                return SessionStateLine(lead: "\(role) wants to run", text: pendingSummary?.text ?? "", mono: true, tone: .primary)
            }
            return SessionStateLine(lead: "Wants to run", text: pendingSummary?.text ?? "", mono: true, tone: .primary, age: pendingSummary.map { age($0.at) })
        case .cap:
            return SessionStateLine(lead: (pendingSummary?.text ?? SessionState.waitingForYou.label) + " ·", text: "continue or stop")
        case .permission:
            if let summary = pendingSummary, answersOnMac {
                return SessionStateLine(text: summary.text, tone: .needsYou, age: age(summary.at))
            }
            guard let summary = pendingSummary, summary.kind == .permission, !summary.text.isEmpty else {
                return SessionStateLine(text: SessionState.waitingForYou.label, tone: .needsYou, age: age(updatedAt))
            }
            if summary.isCommand {
                return SessionStateLine(lead: "Wants to run", text: summary.text, mono: true, tone: .primary, age: age(summary.at))
            }
            return SessionStateLine(lead: "Wants:", text: summary.text, tone: .primary, age: age(summary.at))
        case .question:
            guard let question = askedQuestion else { return SessionStateLine(text: SessionState.askingYou.label, tone: .needsYou) }
            return SessionStateLine(lead: "Asks:", text: question, tone: .primary)
        case .asksParent:
            guard let question = askedQuestion else { return SessionStateLine(text: SessionState.askingParent.label) }
            return SessionStateLine(lead: "Asks parent:", text: question, age: pendingSummary.map { age($0.at) })
        case .childWaiting:
            let count = max(waitingChildren, 1)
            return SessionStateLine(text: count == 1 ? "1 subagent waiting" : "\(count) subagents waiting")
        case .running:
            if status != .running { return SessionStateLine(text: "Waiting for subagents") }
            if let activity, !activity.title.isEmpty { return SessionStateLine(text: activity.title, age: age(activity.at)) }
            return SessionStateLine(text: SessionState.running.label, age: age(turnStartedAt ?? updatedAt))
        case .stopped:
            return SessionStateLine(text: stopped.word, tone: .danger)
        case .open:
            return SessionStateLine(text: [SessionState.done.label, openKinds?.text ?? openLabel ?? ""].joined(separator: " · "))
        case .review:
            return SessionStateLine(text: reviewProgress?.text ?? SessionState.review.label, age: age(updatedAt))
        case .done:
            if reviewProgress == nil, mayAsk { return SessionStateLine(text: SessionState.done.label + " · may have left a question", tone: .faint) }
            return SessionStateLine(text: reviewProgress?.text ?? SessionState.done.label, age: age(updatedAt))
        case .closed:
            return SessionStateLine(text: autoClosed ? "Auto-closed" : SessionState.closed.label, age: age(updatedAt))
        }
    }
}

extension SessionNode {
    public var waitingDescendants: Int {
        children.reduce(0) { $0 + ($1.session.glyph.needsYou ? 1 : 0) + $1.waitingDescendants }
    }

    public var needsYouInTree: Bool {
        session.glyph.needsYou || waitingDescendants > 0
    }

    public func rows(expanded: Set<String>, folded: Set<String> = []) -> [SessionRowItem] {
        rows(depth: 0, expanded: expanded, folded: folded, row: nil)
    }

    var unstarted: [RepoRow] {
        guard openTask else { return [] }
        return session.taskRows.filter { row in row.session.map { id in !children.contains { $0.id == id } } ?? true }
    }

    private func pinned(_ row: RepoRow?) -> Bool {
        row?.tone == "warning" || row?.tone == "danger"
    }

    var openTask: Bool {
        !session.isClosed && !session.taskRows.isEmpty
    }

    private func rows(depth: Int, expanded: Set<String>, folded: Set<String>, row: RepoRow?) -> [SessionRowItem] {
        let open = expanded.contains(id) || openTask && !folded.contains(id)
        let repoRow = { (kid: SessionNode) in session.taskRows.first { $0.session == kid.id } }
        let shown = open ? children : children.filter { $0.needsYouInTree || pinned(repoRow($0)) }
        let nested = shown.flatMap { kid in kid.rows(depth: depth + 1, expanded: expanded, folded: folded, row: repoRow(kid)) }
        let repos = unstarted.filter { open || pinned($0) }.map { SessionRowItem(node: self, depth: depth + 1, expanded: false, hidden: 0, repo: $0) }
        let total = descendants + unstarted.count
        let hidden = open ? 0 : total - repos.count - shown.reduce(0) { $0 + 1 + $1.descendants }
        return [SessionRowItem(node: self, depth: depth, expanded: open, hidden: hidden, taskRow: row)] + nested + repos
    }
}

public struct SessionRowItem: Sendable, Equatable, Identifiable {
    public let node: SessionNode
    public let depth: Int
    public let expanded: Bool
    public let hidden: Int
    public var repo: RepoRow?
    public var taskRow: RepoRow?

    public var id: String { repo.map { node.id + "/" + $0.id } ?? node.id }

    public var toggleCount: Int? {
        let total = node.descendants + node.unstarted.count
        if repo != nil || total == 0 { return nil }
        if expanded { return total }
        return hidden > 0 ? hidden : nil
    }
}

public struct WorkspaceActivity: Sendable, Equatable {
    public let waiting: Int
    public let running: Int
    public let closed: Int

    public init(_ sessions: [AgentSession]) {
        let trees = Dictionary(AgentSession.grouped(sessions).map { ($0.group, $0.sessions.count) }, uniquingKeysWith: +)
        waiting = sessions.filter { $0.glyph.needsYou }.count
        running = trees[.running] ?? 0
        closed = trees[.closed] ?? 0
    }

    public var waitingText: String? {
        waiting > 0 ? "\(waiting) waiting" : nil
    }

    public var restText: String? {
        let parts = [running > 0 ? "\(running) running" : nil, closed > 0 ? "\(closed) closed" : nil].compactMap { $0 }
        if parts.isEmpty { return waiting > 0 ? nil : "No sessions" }
        return parts.joined(separator: " · ")
    }

    public func offlineText(asOf: Date?) -> String {
        let state = waitingText ?? "nothing waiting"
        guard let asOf else { return state }
        return "as of \(ComputerConnection.clock(asOf)) · \(state)"
    }
}

public struct NeedsYouItem: Sendable, Equatable, Identifiable {
    public let computer: String
    public let mac: MacTag?
    public let workspace: Workspace
    public let session: AgentSession
    public var offline = false

    public var id: String { Self.id(computer: computer, session: session.id) }

    public var command: String? {
        guard let summary = session.pendingSummary, summary.isCommand, !summary.text.isEmpty else { return nil }
        return summary.text
    }

    public var title: String {
        if let command { return "Run \(command)?" }
        switch session.glyph {
        case .cap:
            return session.pendingSummary?.text ?? SessionState.waitingForYou.label
        case .question:
            return session.askedQuestion ?? SessionState.askingYou.label
        default:
            guard let text = session.pendingSummary?.text, !text.isEmpty else { return "Waiting for your permission" }
            return session.answersOnMac ? text : text + "?"
        }
    }

    public var subtitle: String {
        [workspace.name, session.displayTitle].joined(separator: " · ")
    }

    public var wants: String {
        if let command { return "wants to run \(command)" }
        if session.glyph == .cap { return session.pendingSummary.map { "\($0.text) · continue or stop" } ?? "waiting for you" }
        if session.glyph == .question { return session.askedQuestion.map { "asks: \($0)" } ?? "asking you" }
        if let summary = session.pendingSummary, let lead = summary.lead, let subject = summary.subject { return lead.lowercased() + " " + subject }
        guard let text = session.pendingSummary?.text, !text.isEmpty else { return "waiting for you" }
        return "wants: \(text)"
    }

    public var bannerDetail: String {
        [workspace.name, wants].joined(separator: " · ")
    }

    public var at: Date {
        session.needsYouAt ?? session.updatedAt
    }

    public static func collect(_ sources: [(computer: String, mac: MacTag?, workspace: Workspace, sessions: [AgentSession])], offline: Set<String> = []) -> [NeedsYouItem] {
        var items: [NeedsYouItem] = []
        for source in sources {
            for session in source.sessions where session.glyph.needsYou {
                items.append(NeedsYouItem(computer: source.computer, mac: source.mac, workspace: source.workspace, session: session, offline: offline.contains(source.computer)))
            }
        }
        return items.sorted { $0.at != $1.at ? $0.at < $1.at : $0.id < $1.id }
    }
}
