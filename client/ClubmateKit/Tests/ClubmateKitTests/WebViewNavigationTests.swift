import XCTest

import ClubmateKit

final class WebViewNavigationTests: XCTestCase {
    private let page = URL(string: "http://127.0.0.1:5000/?tkn=a&folder=%2Fx")!
    private let other = URL(string: "http://127.0.0.1:5000/?tkn=a&folder=%2Fy")!

    func testLoadsWhenNothingIsShown() {
        XCTAssertEqual(WebViewNavigation.decide(current: nil, currentRev: 0, url: page, rev: 0), .load(page))
    }

    func testKeepsTheSamePage() {
        XCTAssertEqual(WebViewNavigation.decide(current: page, currentRev: 1, url: page, rev: 1), .none)
    }

    func testReloadsWhenRevChanges() {
        XCTAssertEqual(WebViewNavigation.decide(current: page, currentRev: 1, url: page, rev: 2), .reload)
    }

    func testLoadsANewURLEvenWhenRevChanges() {
        XCTAssertEqual(WebViewNavigation.decide(current: page, currentRev: 1, url: other, rev: 2), .load(other))
    }
}
