import Foundation
import XCTest

import CompanionKit

final class RequestBannerTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private let shop = Workspace(id: "w1", name: "shop")

    private func items(_ sessions: [AgentSession]) -> [NeedsYouItem] {
        NeedsYouItem.collect([("mac", nil, shop, sessions)])
    }

    private func permission(_ id: String, _ request: String, ago: Double) -> AgentSession {
        AgentSession(id: id, status: .waiting, pending: [request], pendingSummary: PendingSummary(kind: .permission, text: "go test ./...", tool: "execute", at: now.addingTimeInterval(-ago)))
    }

    func testShowsTheNewestRequestOfAnotherSessionAndCountsTheRest() {
        let list = items([
            permission("a", "p1", ago: 90),
            AgentSession(id: "b", status: .done, asks: true, pendingSummary: PendingSummary(kind: .question, text: "Add retries?", at: now.addingTimeInterval(-30))),
            permission("c", "p2", ago: 10),
            AgentSession(id: "d", status: .running),
        ])
        let banner = RequestBanner(list, current: NeedsYouItem.id(computer: "mac", session: "c"))
        XCTAssertEqual(banner?.item.session.id, "b")
        XCTAssertEqual(banner?.moreText, "1 more")
        XCTAssertNil(RequestBanner(items([permission("c", "p2", ago: 10)]), current: "mac|c"))
        XCTAssertNil(RequestBanner(NeedsYouItem.collect([("mac", nil, shop, [permission("a", "p1", ago: 5)])], offline: ["mac"]), current: "mac|x"))
    }

    func testDismissedRequestsStayHiddenUntilANewOneArrives() {
        let first = items([permission("a", "p1", ago: 60)])
        let dismissed = RequestBanner(first, current: "mac|x")?.keys ?? []
        XCTAssertNil(RequestBanner(first, current: "mac|x", dismissed: dismissed))
        let next = RequestBanner(items([permission("a", "p2", ago: 5)]), current: "mac|x", dismissed: dismissed)
        XCTAssertEqual(next?.item.session.pending, ["p2"])
        XCTAssertNil(next?.moreText)
    }
}
