import XCTest

import ClubmateKit

final class RecentCacheTests: XCTestCase {
    func testDropsLeastRecentlyWrittenPastLimit() {
        var cache = RecentCache<String, Int>(limit: 2)
        cache["a"] = 1
        cache["b"] = 2
        cache["a"] = 3
        cache["c"] = 4
        XCTAssertEqual(cache["a"], 3)
        XCTAssertNil(cache["b"])
        XCTAssertEqual(cache["c"], 4)
    }
}
