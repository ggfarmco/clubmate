import Foundation

import ClubmateProtocol

public enum SessionGlyph: Sendable, Equatable {
    case permission
    case question
    case childWaiting
    case running
    case stopped
    case done
    case closed

    public var needsYou: Bool {
        self == .permission || self == .question
    }
}

public struct SessionStateLine: Sendable, Equatable {
    public enum Tone: Sendable, Equatable {
        case primary
        case secondary
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

public enum ShortAge {
    public static func since(_ date: Date, now: Date) -> String {
        let seconds = max(0, Int(now.timeIntervalSince(date)))
        switch seconds {
        case ..<60: return "\(seconds) s"
        case ..<3_600: return "\(seconds / 60) min"
        case ..<86_400: return "\(seconds / 3_600) h"
        default: return "\(seconds / 86_400) d"
        }
    }
}

extension AgentSession {
    public var glyph: SessionGlyph {
        if isClosed { return .closed }
        if needsYou {
            if pendingSummary?.kind == .question, pending.contains(where: AgentSession.isQuestion) { return .question }
            return status == .waiting || !pending.isEmpty ? .permission : .question
        }
        if childStatus == .waiting { return .childWaiting }
        if displayStatus == .running { return .running }
        if status == .error { return .stopped }
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
        case .permission:
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
        case .childWaiting:
            let count = max(waitingChildren, 1)
            return SessionStateLine(text: count == 1 ? "1 subagent waiting" : "\(count) subagents waiting")
        case .running:
            if status != .running { return SessionStateLine(text: "Waiting for subagents") }
            if let activity, !activity.title.isEmpty { return SessionStateLine(text: activity.title, age: age(activity.at)) }
            return SessionStateLine(text: SessionState.running.label, age: age(turnStartedAt ?? updatedAt))
        case .stopped:
            return SessionStateLine(text: SessionState.stopped.label + " · error", tone: .danger)
        case .done:
            return SessionStateLine(text: SessionState.done.label, age: age(updatedAt))
        case .closed:
            return SessionStateLine(text: SessionState.closed.label, age: age(updatedAt))
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

    public func rows(expanded: Set<String>) -> [SessionRowItem] {
        rows(depth: 0, expanded: expanded)
    }

    private func rows(depth: Int, expanded: Set<String>) -> [SessionRowItem] {
        let open = expanded.contains(id)
        let shown = open ? children : children.filter(\.needsYouInTree)
        let nested = shown.flatMap { $0.rows(depth: depth + 1, expanded: expanded) }
        let hidden = open ? 0 : descendants - shown.reduce(0) { $0 + 1 + $1.descendants }
        return [SessionRowItem(node: self, depth: depth, expanded: open, hidden: hidden)] + nested
    }
}

public struct SessionRowItem: Sendable, Equatable, Identifiable {
    public let node: SessionNode
    public let depth: Int
    public let expanded: Bool
    public let hidden: Int

    public var id: String { node.id }

    public var toggleCount: Int? {
        if node.children.isEmpty { return nil }
        if expanded { return node.descendants }
        return hidden > 0 ? hidden : nil
    }
}

public struct WorkspaceActivity: Sendable, Equatable {
    public let waiting: Int
    public let running: Int
    public let closed: Int

    public init(_ sessions: [AgentSession]) {
        waiting = sessions.filter { $0.glyph.needsYou }.count
        running = sessions.filter { $0.glyph == .running && $0.status == .running }.count
        closed = sessions.filter(\.isClosed).count
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
        case .question:
            return session.askedQuestion ?? SessionState.askingYou.label
        default:
            guard let text = session.pendingSummary?.text, !text.isEmpty else { return "Waiting for your permission" }
            return text + "?"
        }
    }

    public var subtitle: String {
        [workspace.name, session.displayTitle].joined(separator: " · ")
    }

    public var wants: String {
        if let command { return "wants to run \(command)" }
        if session.glyph == .question { return session.askedQuestion.map { "asks: \($0)" } ?? "asking you" }
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
