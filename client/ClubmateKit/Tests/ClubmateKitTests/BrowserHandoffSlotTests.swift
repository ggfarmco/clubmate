import XCTest

@testable import ClubmateKit

@MainActor
final class BrowserHandoffSlotTests: XCTestCase {
    private final class Item {}

    func testNewHandoffEndsOlderAsNotDone() async {
        let slot = BrowserHandoffSlot<Item>()
        let first = Item(), second = Item()
        let older = Task.immediate { await slot.wait(for: first) }
        let newer = Task.immediate { await slot.wait(for: second) }
        let olderDone = await older.value
        XCTAssertFalse(olderDone)
        XCTAssertTrue(slot.current === second)
        XCTAssertTrue(slot.finish(true) === second)
        let newerDone = await newer.value
        XCTAssertTrue(newerDone)
    }

    func testFinishOnlyEndsWhatIsShowing() async {
        let slot = BrowserHandoffSlot<Item>()
        XCTAssertNil(slot.finish(false))
        let shown = Item()
        let waiting = Task.immediate { await slot.wait(for: shown) }
        XCTAssertFalse(slot.finish(false, ifShowing: Item()))
        XCTAssertTrue(slot.finish(false, ifShowing: shown))
        let done = await waiting.value
        XCTAssertFalse(done)
        XCTAssertNil(slot.current)
    }
}
