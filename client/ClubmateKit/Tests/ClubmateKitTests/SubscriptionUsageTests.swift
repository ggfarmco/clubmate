import Foundation
import XCTest

import ClubmateKit

final class SubscriptionUsageTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_793_880_000)

    private var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        calendar.locale = Locale(identifier: "en_GB")
        return calendar
    }

    func testDecodesWindowsAndDropsExpiredOnes() {
        let usage = SubscriptionUsage(.object([
            "windows": .object([
                "seven_day": .object(["used": .number(81), "resetsAt": .number(1_794_200_000), "status": .string("allowed_warning")]),
                "five_hour": .object(["used": .number(42), "resetsAt": .number(1_793_870_000)]),
                "seven_day_opus": .object(["used": .number(5), "resetsAt": .number(1_794_200_000)]),
            ]),
            "updatedAt": .number(1_793_879_000),
        ]))

        XCTAssertEqual(usage.windows.map(\.kind), [.fiveHour, .sevenDay])
        XCTAssertEqual(usage.windows.map(\.status), [.allowed, .warning])
        XCTAssertEqual(usage.current(now: now).map(\.kind), [.sevenDay])
        XCTAssertEqual(usage.updatedAt, Date(timeIntervalSince1970: 1_793_879_000))
    }

    func testLabelShowsTimeWithinADayAndWeekdayBeyond() {
        let soon = UsageWindow(kind: .fiveHour, used: 42, resetsAt: now.addingTimeInterval(3 * 3600), status: .allowed)
        let later = UsageWindow(kind: .sevenDay, used: 18, resetsAt: now.addingTimeInterval(3 * 86400), status: .allowed)

        XCTAssertEqual(soon.label, "5h 42%")
        XCTAssertEqual(soon.resetLabel(now: now, calendar: calendar), "15:00")
        XCTAssertEqual(soon.detail(now: now, calendar: calendar), "5-hour limit: 42% used, resets today at 15:00")
        XCTAssertEqual(later.resetLabel(now: now, calendar: calendar), "Sun")
        XCTAssertEqual(later.detail(now: now, calendar: calendar), "Weekly limit: 18% used, resets Sun at 12:00")
    }
}
