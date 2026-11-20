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

    func testNextWaitingPrefersThisWorkspaceThenOldest() {
        let api = Workspace(id: "w2", name: "api")
        let list = NeedsYouItem.collect([
            ("mac", nil, shop, [permission("a", "p1", ago: 30), permission("b", "p2", ago: 10)]),
            ("mac", nil, api, [permission("c", "p3", ago: 90)]),
            ("mac2", nil, api, [permission("d", "p4", ago: 200)]),
        ], offline: ["mac2"])
        XCTAssertEqual(NeedsYouItem.next(list, after: "a", computer: "mac", workspace: "w1")?.session.id, "b")
        XCTAssertEqual(NeedsYouItem.next(list, after: "c", computer: "mac", workspace: "w2")?.session.id, "a")
    }
}
