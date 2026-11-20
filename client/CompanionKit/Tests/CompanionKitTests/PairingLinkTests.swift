import Foundation
import XCTest

import CompanionKit

final class PairingLinkTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    func testParsesHostsInOrder() throws {
        let link = try PairingLink.parse("clubmate://pair?v=1&c=ABCDEF&h=mac.tail1.ts.net&h=100.101.1.2&h=fd7a:115c:a1e0::1&p=47622&id=cmp1&n=Vadim%27s+Mac%20Studio&e=1800000300", now: now)
        XCTAssertEqual(link, PairingLink(code: "ABCDEF", hosts: ["mac.tail1.ts.net", "100.101.1.2", "fd7a:115c:a1e0::1"], port: 47622, computerID: "cmp1", computerName: "Vadim's Mac Studio", expiresAt: Date(timeIntervalSince1970: 1_800_000_300)))
    }

    func testRejectsExpiredCode() {
        XCTAssertThrowsError(try PairingLink.parse("clubmate://pair?v=1&c=A&h=100.64.0.1&id=c&e=1799999999", now: now)) {
            XCTAssertEqual($0 as? PairingLink.Problem, .expired)
        }
    }

    func testKeepsOnlyTailnetHosts() throws {
        XCTAssertEqual(try PairingLink.parse("clubmate://pair?v=1&c=A&h=192.168.1.5&h=100.64.0.7&id=c&e=1800000300", now: now).hosts, ["100.64.0.7"])
        XCTAssertThrowsError(try PairingLink.parse("clubmate://pair?v=1&c=A&h=192.168.1.5&h=127.0.0.1&id=c&e=1800000300", now: now)) {
            XCTAssertEqual($0 as? PairingLink.Problem, .noTailnetHost)
        }
        XCTAssertEqual(try PairingLink.parse("clubmate://pair?v=1&c=A&h=127.0.0.1&id=c&e=1800000300", policy: .tailnetAndLoopback, now: now).hosts, ["127.0.0.1"])
    }

    func testVersionTwoCarriesTheRelay() throws {
        let relay = relayRoute()
        let tail = "&r=https%3A%2F%2Frelay.example&k=\(relay.key)&kid=\(relay.kid)&pt=cmk_pt&id=c&e=1800000300"
        let both = try PairingLink.parse("clubmate://pair?v=2&c=A&h=100.64.0.7" + tail, now: now)
        XCTAssertEqual(both, PairingLink(code: "A", hosts: ["100.64.0.7"], port: PairingLink.defaultPort, computerID: "c", computerName: "Mac", expiresAt: Date(timeIntervalSince1970: 1_800_000_300), relay: relay, pairingTicket: "cmk_pt"))
        let relayOnly = try PairingLink.parse("clubmate://pair?v=2&c=A&h=192.168.1.5" + tail, now: now)
        XCTAssertEqual(relayOnly.hosts, [])
        XCTAssertEqual(relayOnly.relay, relay)
    }

    func testVersionTwoRejectsBadKeyAndRelay() {
        let relay = relayRoute()
        let other = relayRoute()
        let cases: [(String, PairingLink.Problem)] = [
            ("r=https%3A%2F%2Frelay.example&k=\(relay.key)&kid=\(other.kid)", .badKey),
            ("r=https%3A%2F%2Frelay.example&k=AAAA&kid=\(relay.kid)", .badKey),
            ("r=https%3A%2F%2Frelay.example&kid=\(relay.kid)", .badKey),
            ("r=http%3A%2F%2Frelay.example&k=\(relay.key)&kid=\(relay.kid)", .badRelay),
            ("r=https%3A%2F%2F203.0.113.5&k=\(relay.key)&kid=\(relay.kid)", .badRelay),
        ]
        for (query, problem) in cases {
            XCTAssertThrowsError(try PairingLink.parse("clubmate://pair?v=2&c=A&\(query)&pt=cmk_pt&id=c&e=1800000300", now: now), query) {
                XCTAssertEqual($0 as? PairingLink.Problem, problem, query)
            }
        }
    }

    func testRejectsUnknownVersionAndOtherLinks() {
        XCTAssertThrowsError(try PairingLink.parse("clubmate://pair?v=3&c=A&h=127.0.0.1&id=c&e=1800000300", now: now)) {
            XCTAssertEqual($0 as? PairingLink.Problem, .unsupportedVersion("3"))
        }
        XCTAssertThrowsError(try PairingLink.parse("https://example.com/pair?v=1", now: now)) {
            XCTAssertEqual($0 as? PairingLink.Problem, .notAPairingLink)
        }
    }
}

final class PairingReviewTests: XCTestCase {
    private let link = PairingLink(code: "C", hosts: ["mac.tail1.ts.net", "100.64.0.9"], port: 47621, computerID: "mac1", computerName: "Mac", expiresAt: .distantFuture)

    func testNewComputer() {
        let review = PairingReview(link: link, existing: [Computer(id: "other", name: "Other", hosts: ["100.64.0.2"], port: 47621)])
        XCTAssertFalse(review.replacesExisting)
        XCTAssertFalse(review.changesAddress)
    }

    func testReplaceWithSameAddress() {
        let review = PairingReview(link: link, existing: [Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.9", "mac.tail1.ts.net"], port: 47621)])
        XCTAssertTrue(review.replacesExisting)
        XCTAssertFalse(review.changesAddress)
    }

    func testReplaceWithChangedAddress() {
        let review = PairingReview(link: link, existing: [Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.2", "mac.tail1.ts.net"], port: 47000)])
        XCTAssertTrue(review.changesAddress)
        XCTAssertEqual(review.addedHosts, ["100.64.0.9"])
        XCTAssertEqual(review.removedHosts, ["100.64.0.2"])
        XCTAssertEqual(review.previousPort, 47000)
    }

    func testScanSkipsConfirmationOnlyForNewComputer() {
        let new = PairingReview(link: link, existing: [])
        let replacing = PairingReview(link: link, existing: [Computer(id: "mac1", name: "Mac", hosts: link.hosts, port: link.port)])
        XCTAssertFalse(new.needsConfirmation(scanned: true))
        XCTAssertTrue(new.needsConfirmation(scanned: false))
        XCTAssertTrue(replacing.needsConfirmation(scanned: true))
    }

    func testRelayKeyChangeNeedsConfirmation() {
        let old = relayRoute()
        let new = relayRoute()
        let saved = Computer(id: "mac1", name: "Mac", hosts: link.hosts, port: link.port, relay: old)
        let rekeyed = PairingReview(link: PairingLink(code: "C", hosts: link.hosts, port: link.port, computerID: "mac1", computerName: "Mac", expiresAt: .distantFuture, relay: new, pairingTicket: "pt"), existing: [saved])
        XCTAssertTrue(rekeyed.changesKey)
        XCTAssertFalse(rekeyed.changesRelay)
        XCTAssertFalse(rekeyed.changesAddress)
        let moved = PairingReview(link: PairingLink(code: "C", hosts: link.hosts, port: link.port, computerID: "mac1", computerName: "Mac", expiresAt: .distantFuture, relay: relayRoute(url: "https://other.example"), pairingTicket: "pt"), existing: [saved])
        XCTAssertTrue(moved.changesRelay)
    }
}
