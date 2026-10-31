import Foundation
import Observation

public struct UsageWindow: Sendable, Equatable {
    public enum Kind: String, Sendable, CaseIterable {
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
            case .fiveHour: "5-hour limit"
            case .sevenDay: "Weekly limit"
            }
        }
    }

    public enum Status: String, Sendable {
        case allowed
        case warning = "allowed_warning"
        case rejected
    }

    public let kind: Kind
    public let used: Int
    public let resetsAt: Date
    public let status: Status

    public init(kind: Kind, used: Int, resetsAt: Date, status: Status) {
        self.kind = kind
        self.used = used
        self.resetsAt = resetsAt
        self.status = status
    }

    public var label: String {
        "\(kind.short) \(used)%"
    }

    public func resetLabel(now: Date, calendar: Calendar = .autoupdatingCurrent) -> String {
        reset(now: now, calendar: calendar, style: .short)
    }

    public func detail(now: Date, calendar: Calendar = .autoupdatingCurrent) -> String {
        "\(kind.title): \(used)% used, resets \(reset(now: now, calendar: calendar, style: .long))"
    }

    private enum ResetStyle { case short, long }

    private func reset(now: Date, calendar: Calendar, style: ResetStyle) -> String {
        let locale = calendar.locale ?? .autoupdatingCurrent
        let time = resetsAt.formatted(Date.FormatStyle(date: .omitted, time: .shortened, locale: locale, calendar: calendar, timeZone: calendar.timeZone))
        let weekday = resetsAt.formatted(Date.FormatStyle(locale: locale, calendar: calendar, timeZone: calendar.timeZone).weekday(.abbreviated))
        switch style {
        case .short:
            return resetsAt.timeIntervalSince(now) < 24 * 3600 ? time : weekday
        case .long:
            if calendar.isDate(resetsAt, inSameDayAs: now) { return "today at \(time)" }
            if let tomorrow = calendar.date(byAdding: .day, value: 1, to: now), calendar.isDate(resetsAt, inSameDayAs: tomorrow) { return "tomorrow at \(time)" }
            return "\(weekday) at \(time)"
        }
    }
}

public struct SubscriptionUsage: Sendable, Equatable {
    public let windows: [UsageWindow]
    public let updatedAt: Date?

    public init(windows: [UsageWindow], updatedAt: Date?) {
        self.windows = windows
        self.updatedAt = updatedAt
    }

    public init(_ value: JSONValue) {
        let fields = value.objectValue ?? [:]
        let windows = fields["windows"]?.objectValue ?? [:]
        self.windows = UsageWindow.Kind.allCases.compactMap { kind in
            guard let window = windows[kind.rawValue]?.objectValue,
                  let used = window["used"]?.numberValue,
                  let resetsAt = window["resetsAt"]?.numberValue, resetsAt > 0 else { return nil }
            let status = window["status"]?.stringValue.flatMap(UsageWindow.Status.init) ?? .allowed
            return UsageWindow(kind: kind, used: Int(used), resetsAt: Date(timeIntervalSince1970: resetsAt), status: status)
        }
        updatedAt = fields["updatedAt"]?.numberValue.flatMap { $0 > 0 ? Date(timeIntervalSince1970: $0) : nil }
    }

    public func current(now: Date) -> [UsageWindow] {
        windows.filter { $0.resetsAt > now }
    }
}

@MainActor
@Observable
public final class SubscriptionUsageModel {
    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public static let topic = "agents.usage"
    public static let command = "agents.usage"

    public private(set) var usage: SubscriptionUsage?

    @ObservationIgnored private let run: Run

    public init(run: @escaping Run) {
        self.run = run
    }

    public func load() async {
        guard case .success(let value) = await run(CommandInvocation(id: Self.command, args: .object([:]))) else { return }
        receive(value)
    }

    public func follow(_ client: CoreClient) async {
        let events = try? await client.busEvents(pattern: Self.topic)
        await load()
        guard let events else { return }
        for await event in events {
            receive(event.payload)
        }
    }

    private func receive(_ value: JSONValue) {
        let usage = SubscriptionUsage(value)
        if usage != self.usage { self.usage = usage }
    }
}
