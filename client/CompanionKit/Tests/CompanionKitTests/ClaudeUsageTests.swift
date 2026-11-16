import Foundation
import XCTest

import CompanionKit

final class ClaudeUsageTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000 + 10 * 3_600 + 180)

    private var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        calendar.locale = Locale(identifier: "en_GB")
        return calendar
    }

    private func usage(_ account: String, email: String, fiveHour: (Int, LimitStatus) = (42, .allowed), sevenDay: (Int, LimitStatus)? = (81, .allowed), alert: LimitWindowKind? = nil, ago: TimeInterval = 60) -> ClaudeUsage {
        var windows = [LimitWindow(kind: .fiveHour, used: fiveHour.0, resetsAt: now.addingTimeInterval(3_600), status: fiveHour.1)]
        if let sevenDay { windows.append(LimitWindow(kind: .sevenDay, used: sevenDay.0, resetsAt: now.addingTimeInterval(3 * 86_400), status: sevenDay.1)) }
        return ClaudeUsage(account: account, email: email, windows: windows, alert: alert, updatedAt: now.addingTimeInterval(-ago))
    }

    func testDecodesTheUsageReportWithItsAccountAlertAndUnixTimes() throws {
        let json = #"{"account":"a1","accountEmail":"alex@acme.example","windows":{"five_hour":{"used":91,"resetsAt":1800003600,"status":"allowed_warning"},"seven_day":{"used":30,"resetsAt":1800259200,"status":"later"}},"alert":{"window":"five_hour","status":"allowed_warning","used":91,"resetsAt":1800003600},"updatedAt":1799999940,"sessions":{"s":{"five_hour":6}}}"#
        let got = try JSONDecoder().decode(ClaudeUsage.self, from: Data(json.utf8))
        XCTAssertEqual(got, ClaudeUsage(account: "a1", email: "alex@acme.example", windows: [
            LimitWindow(kind: .fiveHour, used: 91, resetsAt: Date(timeIntervalSince1970: 1_800_003_600), status: .warning),
            LimitWindow(kind: .sevenDay, used: 30, resetsAt: Date(timeIntervalSince1970: 1_800_259_200), status: .allowed),
        ], alert: .fiveHour, updatedAt: Date(timeIntervalSince1970: 1_799_999_940)))
    }

    func testSessionRowKeepsThePluginsShareWordsThroughTheCache() throws {
        let json = #"{"id":"s","limitShare":{"five_hour":6.2,"seven_day":0.3},"shareLabel":"≈6% of 5h","shareWords":{"five_hour":"≈6%","seven_day":"<1%"},"shareDetail":"≈6% of 5h · <1% of 7d"}"#
        let decoded = try JSONDecoder().decode(AgentSession.self, from: Data(json.utf8))
        let session = try JSONDecoder().decode(AgentSession.self, from: JSONEncoder().encode(decoded))
        XCTAssertEqual(session.shareLabel, "≈6% of 5h")
        XCTAssertEqual(session.shareDetail, "≈6% of 5h · <1% of 7d")
        XCTAssertEqual(session.limitShare, ["five_hour": 6.2, "seven_day": 0.3])
        XCTAssertEqual([session.shareWord(.fiveHour), session.shareWord(.sevenDay)], ["≈6%", "<1%"])
        XCTAssertEqual(AgentSession(id: "t").shareWord(.fiveHour), "—")
    }

    func testLineShowsTheWindowsStillOpenAndNothingOnceBothReset() {
        let accounts = ClaudeAccounts([UsageReport(computer: "m", mac: "Mac Studio", usage: usage("a", email: "alex@acme.example"))])
        XCTAssertEqual(accounts.line(for: "m", now: now, calendar: calendar)?.text, "5h 42% · 7d 81%")
        XCTAssertEqual(accounts.line(for: "m", now: now.addingTimeInterval(7_200), calendar: calendar)?.text, "7d 81%")
        XCTAssertNil(accounts.line(for: "m", now: now.addingTimeInterval(4 * 86_400), calendar: calendar))
    }

    func testLineNarrowsToTheAlertedWindowInItsTone() {
        let warning = ClaudeAccounts([UsageReport(computer: "m", mac: "Mac Studio", usage: usage("a", email: "a@b.c", fiveHour: (91, .warning), alert: .fiveHour))])
        let line = warning.line(for: "m", now: now, calendar: calendar)
        XCTAssertEqual(line?.tone, .warning)
        XCTAssertEqual(line?.text, "5h 91% · resets 19:03")
        let reached = ClaudeAccounts([UsageReport(computer: "m", mac: "Mac Studio", usage: usage("a", email: "a@b.c", sevenDay: (100, .rejected), alert: .sevenDay))])
        XCTAssertEqual(reached.line(for: "m", now: now, calendar: calendar)?.tone, .danger)
        XCTAssertEqual(reached.line(for: "m", now: now, calendar: calendar)?.text, "7d limit reached · resets Mon")
    }

    func testAccountsNeverMixAndTheNewestReportOfAnAccountWins() {
        let accounts = ClaudeAccounts([
            UsageReport(computer: "studio", mac: "Mac Studio", usage: usage("acme", email: "alex@acme.example", fiveHour: (40, .allowed), ago: 300)),
            UsageReport(computer: "air", mac: "MacBook Air", usage: usage("acme", email: "alex@acme.example", fiveHour: (44, .allowed), ago: 60)),
            UsageReport(computer: "pro", mac: "MacBook Pro", usage: usage("home", email: "alex@home.example", fiveHour: (12, .allowed), sevenDay: (30, .allowed))),
        ])
        let acme = accounts.account(of: "studio")
        XCTAssertEqual(acme?.macs, ["Mac Studio", "MacBook Air"])
        XCTAssertEqual(acme?.via, "MacBook Air")
        XCTAssertEqual(acme?.seen(now: now), "1 min ago")
        XCTAssertEqual(accounts.line(for: "studio", now: now, calendar: calendar)?.text, "alex@acme · 5h 44% · 7d 81%")
        XCTAssertEqual(accounts.others(than: "studio").map(\.email), ["alex@home.example"])
        XCTAssertEqual(ClaudeAccounts([UsageReport(computer: "pro", mac: "MacBook Pro", usage: usage("home", email: "alex@home.example"))]).line(for: "pro", now: now, calendar: calendar)?.label, nil)
    }

    func testAMacThatReportedNothingIsNoAccount() {
        let accounts = ClaudeAccounts([
            UsageReport(computer: "studio", mac: "Mac Studio", usage: usage("acme", email: "alex@acme.example")),
            UsageReport(computer: "pro", mac: "MacBook Pro", usage: ClaudeUsage(windows: [])),
        ])
        XCTAssertNil(accounts.line(for: "studio", now: now, calendar: calendar)?.label)
        XCTAssertEqual(accounts.others(than: "studio"), [])
    }

    func testLabelIsTheLocalPartWidenedOnlyAsFarAsAClashNeeds() {
        let labels = { (emails: [String]) in
            ClaudeAccounts(emails.enumerated().map { UsageReport(computer: "m\($0.offset)", mac: "M\($0.offset)", usage: self.usage("id\($0.offset)", email: $0.element)) }).accounts.map(\.label).sorted()
        }
        XCTAssertEqual(labels(["alex@acme.example", "sam@acme.example"]), ["alex", "sam"])
        XCTAssertEqual(labels(["alex@acme.example", "alex@home.example"]), ["alex@acme", "alex@home"])
        XCTAssertEqual(labels(["alex@acme.example", "alex@acme.io"]), ["alex@acme.example", "alex@acme.io"])
    }
}
