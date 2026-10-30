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

    func testRejectsUnknownVersionAndOtherLinks() {
        XCTAssertThrowsError(try PairingLink.parse("clubmate://pair?v=2&c=A&h=127.0.0.1&id=c&e=1800000300", now: now)) {
            XCTAssertEqual($0 as? PairingLink.Problem, .unsupportedVersion("2"))
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
}
