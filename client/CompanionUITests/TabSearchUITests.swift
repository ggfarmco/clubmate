import XCTest

@MainActor
final class TabSearchUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"

    override func setUp() {
        continueAfterFailure = false
        if UIDevice.current.userInterfaceIdiom == .pad { XCUIDevice.shared.orientation = .landscapeLeft }
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
    }

    func testNoSearchTab() {
        let app = launch(open: "tab:agents", ["-wiki", "YES"])
        XCTAssertTrue(app.buttons["Wiki"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Search"].exists)
    }

    func testAgentsSearchFiltersSessions() {
        filters(launch(open: "tab:agents"), prompt: "Search sessions", query: "cart total", kept: "Refactor the cart total", gone: "Explain the checkout flow")
    }

    func testBacklogSearchFiltersTasks() {
        filters(launch(open: "tab:backlog"), prompt: "Search, or a #number", query: "split", kept: "Split checkout.go", gone: "Retry the charge step on 502")
    }

    func testWikiSearchFiltersTree() {
        filters(launch(open: "tab:wiki", ["-wiki", "YES"]), prompt: "Search wiki", query: "retr", kept: "Retry notes", gone: "Checkout flow")
    }

    func testTabRootsFoldTitleAndSearchOnScroll() {
        if UIDevice.current.userInterfaceIdiom == .pad { return }
        let app = launch(open: "tab:agents", ["-wiki", "YES"])
        for (tab, prompt, long) in [("Agents", "Search sessions", true), ("Backlog", "Search, or a #number", false), ("Wiki", "Search wiki", true)] {
            let button = app.tabBars.firstMatch.buttons[tab]
            XCTAssertTrue(wait { button.isHittable }, tab)
            button.tap()
            let bar = app.navigationBars[tab]
            let field = app.searchFields[prompt]
            XCTAssertTrue(bar.waitForExistence(timeout: 10), tab)
            XCTAssertTrue(field.waitForExistence(timeout: 10), tab)
            XCTAssertTrue(wait { field.isHittable && field.frame.minY > bar.frame.minY + 60 }, "\(tab) field at \(field.frame.minY)")
            guard long else { continue }
            let height = bar.frame.height
            let start = app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.7))
            start.press(forDuration: 0.1, thenDragTo: start.withOffset(CGVector(dx: 0, dy: -300)), withVelocity: .slow, thenHoldForDuration: 0.3)
            XCTAssertTrue(wait { !field.isHittable }, "\(tab) field at \(field.frame.minY)")
            XCTAssertTrue(wait { bar.frame.height < height - 30 }, "\(tab) bar \(bar.frame.height) vs \(height)")
            XCTAssertTrue(wait { bar.staticTexts[tab].exists }, tab)
            start.press(forDuration: 0.1, thenDragTo: start.withOffset(CGVector(dx: 0, dy: 400)), withVelocity: .fast, thenHoldForDuration: 0.1)
            XCTAssertTrue(wait { field.isHittable }, tab)
        }
    }

    private func filters(_ app: XCUIApplication, prompt: String, query: String, kept: String, gone: String) {
        let hidden = row(gone, in: app)
        XCTAssertTrue(hidden.waitForExistence(timeout: 30), gone)
        let field = app.searchFields[prompt]
        if !field.waitForExistence(timeout: 2) { app.swipeDown() }
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText(query)
        XCTAssertTrue(row(kept, in: app).waitForExistence(timeout: 10), kept)
        XCTAssertTrue(wait { !hidden.exists }, gone)
        field.buttons["Clear text"].tap()
        XCTAssertTrue(hidden.waitForExistence(timeout: 10), gone)
    }

    private func row(_ title: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "elementType != %d AND label CONTAINS %@", XCUIElement.ElementType.searchField.rawValue, title)).firstMatch
    }

    private func wait(_ condition: @escaping () -> Bool) -> Bool {
        let met = NSPredicate { _, _ in condition() }
        return XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: met, object: nil)], timeout: 10) == .completed
    }

    private func launch(open target: String, _ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", target] + extra
        app.launch()
        return app
    }
}
