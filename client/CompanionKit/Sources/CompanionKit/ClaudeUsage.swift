import Foundation

public enum LimitWindowKind: String, CaseIterable, Sendable {
    case fiveHour = "five_hour"
    case sevenDay = "seven_day"

    public var short: String {
        switch self {
        case .fiveHour: "5h"
        case .sevenDay: "7d"
        }
    }

    public var title: String {
        switch self {
        case .fiveHour: "5-hour window"
        case .sevenDay: "7-day window"
        }
    }
}

public enum LimitStatus: String, Sendable {
    case allowed
    case warning = "allowed_warning"
    case rejected
}

public enum UsageTone: Sendable, Equatable {
    case normal
    case warning
    case danger
}

public struct LimitWindow: Equatable, Sendable {
    public let kind: LimitWindowKind
    public let used: Int
    public let resetsAt: Date
    public let status: LimitStatus

    public init(kind: LimitWindowKind, used: Int, resetsAt: Date, status: LimitStatus = .allowed) {
        self.kind = kind
        self.used = used
        self.resetsAt = resetsAt
        self.status = status
    }

    public var tone: UsageTone {
        switch status {
        case .allowed: .normal
        case .warning: .warning
        case .rejected: .danger
        }
    }

    public func reset(now: Date, calendar: Calendar = .autoupdatingCurrent) -> String {
        Self.clock(resetsAt, now: now, calendar: calendar)
    }

    public static func clock(_ date: Date, now: Date, calendar: Calendar = .autoupdatingCurrent) -> String {
        let locale = calendar.locale ?? .autoupdatingCurrent
        if date.timeIntervalSince(now) < 24 * 3600 {
            return date.formatted(Date.FormatStyle(date: .omitted, time: .shortened, locale: locale, calendar: calendar, timeZone: calendar.timeZone))
        }
        return date.formatted(Date.FormatStyle(locale: locale, calendar: calendar, timeZone: calendar.timeZone).weekday(.abbreviated))
    }

    public func value(now: Date, calendar: Calendar = .autoupdatingCurrent) -> String {
        "\(used)% · resets \(reset(now: now, calendar: calendar))"
    }
}

public struct ClaudeUsage: Decodable, Equatable, Sendable {
    public let account: String
    public let email: String
    public let windows: [LimitWindow]
    public let alert: LimitWindowKind?
    public let updatedAt: Date?

    public init(account: String = "", email: String = "", windows: [LimitWindow], alert: LimitWindowKind? = nil, updatedAt: Date? = nil) {
        self.account = account
        self.email = email
        self.windows = windows
        self.alert = alert
        self.updatedAt = updatedAt
    }

    private struct Wire: Decodable {
        struct Window: Decodable {
            let used: Double?
            let resetsAt: Double?
            let status: String?
        }

        struct Alert: Decodable {
            let window: String?
        }

        let account: String?
        let accountEmail: String?
        let windows: [String: Window]?
        let alert: Alert?
        let updatedAt: Double?
    }

    public init(from decoder: any Decoder) throws {
        let wire = try Wire(from: decoder)
        let windows = LimitWindowKind.allCases.compactMap { kind -> LimitWindow? in
            guard let window = wire.windows?[kind.rawValue], let resetsAt = window.resetsAt, resetsAt > 0 else { return nil }
            return LimitWindow(kind: kind, used: Int(window.used ?? 0), resetsAt: Date(timeIntervalSince1970: resetsAt), status: window.status.flatMap(LimitStatus.init) ?? .allowed)
        }
        self.init(
            account: wire.account ?? "",
            email: wire.accountEmail ?? "",
            windows: windows,
            alert: wire.alert?.window.flatMap(LimitWindowKind.init),
            updatedAt: wire.updatedAt.flatMap { $0 > 0 ? Date(timeIntervalSince1970: $0) : nil }
        )
    }

    public func window(_ kind: LimitWindowKind, now: Date) -> LimitWindow? {
        windows.first { $0.kind == kind && $0.resetsAt > now }
    }

    public func current(now: Date) -> [LimitWindow] {
        windows.filter { $0.resetsAt > now }
    }
}

public struct UsageLine: Equatable, Sendable {
    public struct Part: Equatable, Sendable {
        public let text: String
        public let strong: Bool

        public init(_ text: String, strong: Bool = false) {
            self.text = text
            self.strong = strong
        }
    }

    public let label: String?
    public let tone: UsageTone
    public let parts: [Part]

    public var text: String {
        ([label].compactMap { $0 } + [parts.map(\.text).joined()]).joined(separator: " · ")
    }
}

public struct UsageReport: Equatable, Sendable {
    public let computer: String
    public let mac: String
    public let usage: ClaudeUsage

    public init(computer: String, mac: String, usage: ClaudeUsage) {
        self.computer = computer
        self.mac = mac
        self.usage = usage
    }
}

public struct UsageAccount: Equatable, Sendable, Identifiable {
    public let id: String
    public let email: String
    public let label: String
    public let computers: [String]
    public let macs: [String]
    public let via: String
    public let usage: ClaudeUsage

    public var name: String {
        email.isEmpty ? "Claude account" : email
    }

    public func seen(now: Date) -> String? {
        usage.updatedAt.map { ShortAge.since($0, now: now) + " ago" }
    }
}

public struct ClaudeAccounts: Equatable, Sendable {
    public let accounts: [UsageAccount]

    public init(_ reports: [UsageReport]) {
        let groups = Dictionary(grouping: reports.filter { !$0.usage.account.isEmpty || !$0.usage.windows.isEmpty }) { $0.usage.account.isEmpty ? "mac:" + $0.computer : $0.usage.account }
        let merged = groups.map { key, reports -> (key: String, newest: UsageReport, reports: [UsageReport]) in
            let sorted = reports.sorted { $0.mac.localizedStandardCompare($1.mac) == .orderedAscending }
            let newest = sorted.max { ($0.usage.updatedAt ?? .distantPast) < ($1.usage.updatedAt ?? .distantPast) } ?? sorted[0]
            return (key, newest, sorted)
        }
        let emails = merged.map(\.newest.usage.email)
        accounts = merged.map { group in
            let email = group.newest.usage.email
            return UsageAccount(
                id: group.key,
                email: email,
                label: email.isEmpty ? group.newest.mac : Self.label(email, among: emails),
                computers: group.reports.map(\.computer),
                macs: group.reports.map(\.mac),
                via: group.newest.mac,
                usage: group.newest.usage
            )
        }
        .sorted { $0.label.localizedStandardCompare($1.label) == .orderedAscending }
    }

    public var several: Bool {
        accounts.count > 1
    }

    public func account(of computer: String) -> UsageAccount? {
        accounts.first { $0.computers.contains(computer) }
    }

    public func others(than computer: String) -> [UsageAccount] {
        accounts.filter { !$0.computers.contains(computer) }
    }

    public func line(for computer: String, now: Date, calendar: Calendar = .autoupdatingCurrent) -> UsageLine? {
        account(of: computer).flatMap { line($0, now: now, calendar: calendar) }
    }

    public func line(_ account: UsageAccount, now: Date, calendar: Calendar = .autoupdatingCurrent) -> UsageLine? {
        let windows = account.usage.current(now: now)
        guard !windows.isEmpty else { return nil }
        let label = several ? account.label : nil
        if let kind = account.usage.alert, let window = account.usage.window(kind, now: now), window.status != .allowed {
            let reset = " · resets " + window.reset(now: now, calendar: calendar)
            if window.status == .rejected {
                return UsageLine(label: label, tone: .danger, parts: [.init(kind.short + " limit reached" + reset)])
            }
            return UsageLine(label: label, tone: .warning, parts: [.init(kind.short + " "), .init("\(window.used)%", strong: true), .init(reset)])
        }
        var parts: [UsageLine.Part] = []
        for window in windows {
            parts.append(.init((parts.isEmpty ? "" : " · ") + window.kind.short + " "))
            parts.append(.init("\(window.used)%", strong: true))
        }
        return UsageLine(label: label, tone: .normal, parts: parts)
    }

    static func label(_ email: String, among emails: [String]) -> String {
        let local = Self.local(email)
        let rivals = Set(emails.filter { $0 != email && !$0.isEmpty })
        guard rivals.contains(where: { Self.local($0) == local }) else { return local }
        let short = local + "@" + Self.domainHead(email)
        return rivals.contains { Self.local($0) + "@" + Self.domainHead($0) == short } ? email : short
    }

    private static func local(_ email: String) -> String {
        String(email.split(separator: "@", maxSplits: 1).first ?? Substring(email))
    }

    private static func domainHead(_ email: String) -> String {
        let domain = email.split(separator: "@", maxSplits: 1).dropFirst().first ?? ""
        return String(domain.split(separator: ".").first ?? domain)
    }
}

extension AgentSession {
    public func shareWord(_ kind: LimitWindowKind) -> String {
        shareWords[kind.rawValue] ?? "—"
    }
}
