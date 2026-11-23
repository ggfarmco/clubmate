import Foundation

public struct WorkspaceSummary: Sendable, Equatable {
    public enum Status: Sendable, Equatable {
        case needsYou, failed, running, idle

        public var symbol: String {
            switch self {
            case .needsYou: "hand.raised"
            case .failed: "xmark.circle"
            case .running: "play"
            case .idle: "folder"
            }
        }
    }

    public var needsYou = 0
    public var running = 0
    public var failed = 0
    public var lastActiveAt: Date?

    public init(needsYou: Int = 0, running: Int = 0, failed: Int = 0, lastActiveAt: Date? = nil) {
        self.needsYou = needsYou
        self.running = running
        self.failed = failed
        self.lastActiveAt = lastActiveAt
    }

    public init(_ activity: Workspace.Activity?) {
        self.init(needsYou: activity?.needsYou ?? 0, running: activity?.running ?? 0, failed: activity?.failed ?? 0, lastActiveAt: activity?.lastActiveAt)
    }

    public static func + (lhs: Self, rhs: Self) -> Self {
        Self(needsYou: lhs.needsYou + rhs.needsYou, running: lhs.running + rhs.running, failed: lhs.failed + rhs.failed, lastActiveAt: [lhs.lastActiveAt, rhs.lastActiveAt].compactMap { $0 }.max())
    }

    public static func of(_ workspaces: [Workspace], waiting: (Workspace) -> Int) -> Self {
        let sends = sendsActivity(workspaces)
        return workspaces.reduce(Self()) { $0 + summary(of: $1, sendsActivity: sends, waiting: waiting) }
    }

    public static func sendsActivity(_ workspaces: [Workspace]) -> Bool {
        workspaces.contains { $0.activity != nil }
    }

    public static func summary(of workspace: Workspace, sendsActivity: Bool, waiting: (Workspace) -> Int) -> Self {
        sendsActivity ? Self(workspace.activity) : Self(needsYou: waiting(workspace))
    }

    public var status: Status {
        if needsYou > 0 { return .needsYou }
        if failed > 0 { return .failed }
        if running > 0 { return .running }
        return .idle
    }

    public func glyph(home: Bool) -> String {
        let status = status
        return home && status == .idle ? "house" : status.symbol
    }

    public var counts: [String] {
        [needsYou > 0 ? "\(needsYou) waiting" : nil, failed > 0 ? "\(failed) failed" : nil, running > 0 ? "\(running) running" : nil].compactMap { $0 }
    }

    public func text(now: Date) -> String? {
        let counts = counts
        if !counts.isEmpty { return counts.joined(separator: " · ") }
        return lastActiveAt.map { "idle · " + Self.age($0, now: now) }
    }

    public func subtitle(now: Date, asOf: Date? = nil, offline: Bool = false) -> String? {
        let text = text(now: now)
        guard offline, let asOf else { return text }
        let prefix = "as of " + ComputerConnection.clock(asOf)
        return text.map { prefix + " · " + $0 } ?? prefix
    }

    public func accessibilityLabel(name: String, current: Bool, now: Date, asOf: Date? = nil, offline: Bool = false) -> String {
        let detail = subtitle(now: now, asOf: asOf, offline: offline).map { $0.replacingOccurrences(of: " · ", with: ", ") }
        return [name, current ? "current workspace" : nil, detail].compactMap { $0 }.joined(separator: ", ")
    }

    public static func macSubtitle(_ summary: Self, offline: Bool, lastSeen: Date?, now: Date) -> String? {
        guard offline else { return summary.text(now: now) }
        return "offline" + (lastSeen.map { " · as of " + ComputerConnection.clock($0) } ?? "")
    }

    public static func age(_ date: Date, now: Date) -> String {
        let seconds = max(0, Int(now.timeIntervalSince(date)))
        switch seconds {
        case ..<60: return "now"
        case ..<3_600: return "\(seconds / 60) min"
        case ..<86_400: return "\(seconds / 3_600) h"
        case ..<604_800: return "\(seconds / 86_400) d"
        default: return "\(seconds / 604_800) w"
        }
    }
}
