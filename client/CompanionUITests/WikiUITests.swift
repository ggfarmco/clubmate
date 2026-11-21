import XCTest

@MainActor
final class WikiUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"

    override func setUp() {
        continueAfterFailure = false
    }

    func testWikiTabRootIsTree() {
        let app = launch(["-wiki", "YES"])
        let tab = app.tabBars.firstMatch.buttons["Wiki"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10))
        tab.tap()
        let row = app.buttons["Checkout flow"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        for _ in 0..<3 {
            row.tap()
            XCTAssertTrue(app.navigationBars["Checkout flow"].waitForExistence(timeout: 10))
            XCTAssertTrue(app.webViews.staticTexts["Checkout flow part 1 explains one step of the shop in detail."].waitForExistence(timeout: 10))
            app.navigationBars["Checkout flow"].buttons.firstMatch.tap()
            XCTAssertTrue(row.waitForExistence(timeout: 5))
        }
    }

    func testAPageDeletedOnTheMacSaysSo() {
        let app = launch(["-wiki", "YES", "-wikiGone", "shop/notes/retries.md"], open: "tab:wiki")
        let row = app.buttons["Retry notes"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.tap()
        XCTAssertTrue(app.staticTexts["Page Not Found"].waitForExistence(timeout: 10))
    }

    func testWikiRowHeightMatchesBacklog() {
        let app = launch(["-wiki", "YES"], open: "tab:backlog")
        let task = app.cells.containing(NSPredicate(format: "label CONTAINS %@", "Retry the charge step on 502")).firstMatch
        XCTAssertTrue(task.waitForExistence(timeout: 10))
        let height = task.frame.height
        app.tabBars.firstMatch.buttons["Wiki"].tap()
        let page = app.cells.containing(NSPredicate(format: "label CONTAINS %@", "agent · 08:41")).firstMatch
        XCTAssertTrue(page.waitForExistence(timeout: 10))
        XCTAssertEqual(page.frame.height, height, accuracy: 1)
    }

    func testATapBelowTheTreeDropsTheSearchKeyboard() {
        let app = launch(["-wiki", "YES"])
        let tab = app.tabBars.firstMatch.buttons["Wiki"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10))
        tab.tap()
        let field = app.searchFields["Search wiki"]
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        let keyboard = app.keyboards.firstMatch
        field.tap()
        XCTAssertTrue(keyboard.waitForExistence(timeout: 5))
        field.tap()
        XCTAssertFalse(wait { !keyboard.exists })
        for _ in 0..<3 {
            field.tap()
            field.typeText("ch")
            XCTAssertTrue(keyboard.waitForExistence(timeout: 5))
            app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: keyboard.frame.midX, dy: keyboard.frame.minY - 80)).tap()
            XCTAssertTrue(wait { !keyboard.exists })
        }
    }

    func testNoWikiTabWithoutWikiPlugin() {
        let app = launch([])
        let tabs = app.tabBars.firstMatch
        XCTAssertTrue(tabs.buttons["More"].waitForExistence(timeout: 10))
        tabs.buttons["More"].tap()
        XCTAssertTrue(app.staticTexts["Cron"].waitForExistence(timeout: 10))
        XCTAssertFalse(tabs.buttons["Wiki"].exists)
    }

    func testWikiLinkPushesWikiPageAndBackKeepsScroll() {
        let app = launch(["-wiki", "YES"])
        let tab = app.tabBars.firstMatch.buttons["Wiki"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10))
        tab.tap()
        let row = app.buttons["Shop wiki"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.tap()
        let link = app.webViews.links["read the checkout flow next"]
        XCTAssertTrue(link.waitForExistence(timeout: 10))
        for _ in 0..<8 where !link.isHittable {
            app.webViews.firstMatch.swipeUp()
        }
        XCTAssertTrue(link.isHittable)
        var before: CGRect?
        for _ in 0..<4 {
            link.tap()
            let page = app.navigationBars["Checkout flow"]
            XCTAssertTrue(page.waitForExistence(timeout: 10))
            XCTAssertFalse(app.buttons["back.chip"].exists)
            page.buttons.firstMatch.tap()
            XCTAssertTrue(app.navigationBars["Shop wiki"].waitForExistence(timeout: 5))
            if let before {
                XCTAssertTrue(wait { link.isHittable && abs(link.frame.minY - before.minY) < 2 }, "\(link.frame) vs \(before)")
            } else {
                var last = CGRect.null
                XCTAssertTrue(wait {
                    let frame = link.frame
                    defer { last = frame }
                    return link.isHittable && frame == last
                })
                before = last
            }
        }
    }

    func testWikiLinkOutsideWikiPushesDocPage() {
        let app = launch(["-wiki", "YES"])
        let tab = app.tabBars.firstMatch.buttons["Wiki"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10))
        tab.tap()
        let row = app.buttons["Shop wiki"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.tap()
        let link = app.webViews.links["payments overview"]
        XCTAssertTrue(link.waitForExistence(timeout: 10))
        link.tap()
        let text = app.webViews.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "calls the provider once per order")).firstMatch
        XCTAssertTrue(text.waitForExistence(timeout: 10))
        XCTAssertFalse(app.navigationBars["Shop wiki"].exists)
    }

    func testWikiPageFromSessionHasChipAndShowInWiki() {
        let app = launch(["-wiki", "YES"], open: "agents/agt_0000000000000002")
        let link = app.links["the retries plan"]
        XCTAssertTrue(link.waitForExistence(timeout: 10))
        link.tap()
        let page = app.navigationBars["Checkout retries"]
        XCTAssertTrue(page.waitForExistence(timeout: 10))
        XCTAssertTrue(app.webViews.staticTexts["Retries keep the idempotency key."].waitForExistence(timeout: 10))
        let chip = app.buttons["back.chip"]
        XCTAssertTrue(chip.waitForExistence(timeout: 5))
        XCTAssertEqual(chip.label, "Explain the…")
        app.buttons["More Actions"].firstMatch.tap()
        app.buttons["Show in Wiki"].tap()
        XCTAssertTrue(app.tabBars.firstMatch.buttons["Wiki"].isSelected)
        XCTAssertTrue(page.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["back.chip"].exists)
    }

    func testWikiHeaderFoldsIntoBar() {
        let app = openCheckoutFlow()
        let bar = app.navigationBars["Checkout flow"]
        XCTAssertTrue(bar.staticTexts["Updated 08:41 by agent"].exists)
        let rest = bar.frame.height
        drag(app, by: -110)
        XCTAssertTrue(wait { bar.frame.height < rest - 20 }, "\(bar.frame.height) vs \(rest)")
        XCTAssertTrue(bar.isHittable)
    }

    func testReadingModeHidesBarsAndComesBackOnScrollUp() {
        let app = openCheckoutFlow()
        let bar = app.navigationBars["Checkout flow"]
        let tabs = app.tabBars.firstMatch
        XCTAssertTrue(tabs.isHittable)
        app.webViews.firstMatch.swipeUp()
        app.webViews.firstMatch.swipeUp()
        XCTAssertTrue(wait { !(bar.exists && bar.isHittable) && !(tabs.exists && tabs.isHittable) })
        drag(app, by: 60)
        XCTAssertTrue(wait { bar.exists && bar.isHittable && tabs.exists && tabs.isHittable })
    }

    func testReadingModeShowsNoTextUnderTheStatusBar() {
        let app = openCheckoutFlow()
        let bar = app.navigationBars["Checkout flow"]
        app.webViews.firstMatch.swipeUp()
        app.webViews.firstMatch.swipeUp()
        XCTAssertTrue(wait { !(bar.exists && bar.isHittable) })
        let width = app.windows.firstMatch.frame.width
        for _ in 0..<3 {
            drag(app, by: -140)
            let shot = app.screenshot()
            let page = color(at: CGPoint(x: 4, y: 400), in: shot)
            for y in stride(from: 52, through: 56, by: 2) {
                for x in stride(from: 16, to: Int(width) - 16, by: 3) {
                    let pixel = color(at: CGPoint(x: x, y: y), in: shot)
                    XCTAssertTrue(zip(pixel, page).allSatisfy { abs(Int($0) - Int($1)) <= 24 }, "\(pixel) vs \(page) at \(x),\(y)")
                }
            }
        }
    }

    func testPageActionsInBarAndMenu() {
        let app = openCheckoutFlow()
        let bar = app.navigationBars["Checkout flow"]
        XCTAssertTrue(bar.buttons["Edit"].exists)
        XCTAssertFalse(bar.buttons["History"].exists)
        app.buttons["More Actions"].firstMatch.tap()
        XCTAssertTrue(app.buttons["History"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Find in Page"].exists)
        XCTAssertTrue(app.buttons["Copy path"].exists)
    }

    func testPageBarIsEditVoiceMore() {
        let app = openCheckoutFlow()
        let bar = app.navigationBars["Checkout flow"]
        let order = [bar.buttons["Edit"], bar.buttons["Talk to Clubmate"], bar.buttons["More Actions"]].map(\.frame.minX)
        XCTAssertEqual(order, order.sorted(), "\(order)")
    }

    func testChipPageKeepsActionsInItsOwnMenu() {
        let app = launch(["-wiki", "YES"], open: "tab:wiki")
        let tray = app.buttons["inbox.tray"]
        XCTAssertTrue(tray.waitForExistence(timeout: 10))
        tray.tap()
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "Should I add retries")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.tap()
        let link = app.links["the retries plan"]
        XCTAssertTrue(link.waitForExistence(timeout: 10))
        link.tap()
        let bar = app.navigationBars["Checkout retries"]
        XCTAssertTrue(bar.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["back.chip"].exists)
        XCTAssertFalse(bar.buttons["OverflowBarButtonItem"].exists)
        XCTAssertFalse(bar.buttons["Edit"].exists)
        bar.buttons["More Actions"].tap()
        XCTAssertTrue(app.buttons["Edit"].waitForExistence(timeout: 5))
    }

    func testLeavingReadingModeKeepsTextInPlace() {
        let app = openCheckoutFlow()
        let bar = app.navigationBars["Checkout flow"]
        app.webViews.firstMatch.swipeUp()
        app.webViews.firstMatch.swipeUp()
        XCTAssertTrue(wait { !(bar.exists && bar.isHittable) })
        let text = app.webViews.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Checkout flow part")).allElementsBoundByIndex.first { $0.frame.minY > 200 && $0.frame.minY < 600 }
        let before = text?.frame.minY ?? .nan
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.96, dy: 0.5)).tap()
        XCTAssertTrue(wait { bar.exists && bar.isHittable })
        XCTAssertTrue(wait { abs((text?.frame.minY ?? .nan) - before) < 2 }, "\(text?.frame.minY ?? .nan) vs \(before)")
    }

    func testPageHeaderSharesThePageBackground() {
        let app = openCheckoutFlow()
        let width = app.windows.firstMatch.frame.width
        let shot = app.screenshot()
        XCTAssertEqual(color(at: CGPoint(x: width - 12, y: 150), in: shot), color(at: CGPoint(x: width - 12, y: 600), in: shot))
    }

    func testTextStartsFourteenPointsBelowTheSubtitle() {
        let app = openCheckoutFlow()
        let subtitle = app.navigationBars["Checkout flow"].staticTexts["Updated 08:41 by agent"]
        let text = app.webViews.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "On this page")).firstMatch
        XCTAssertTrue(text.waitForExistence(timeout: 10))
        XCTAssertTrue(wait { abs(text.frame.minY - subtitle.frame.maxY - 14) <= 3 }, "text at \(text.frame.minY), subtitle ends at \(subtitle.frame.maxY)")
        XCTAssertLessThanOrEqual(text.frame.minY / app.windows.firstMatch.frame.height, 0.225)
    }

    func testTitleDrawnOnce() {
        let app = openCheckoutFlow()
        XCTAssertEqual(app.staticTexts.matching(NSPredicate(format: "label == %@", "Checkout flow")).count, 1)
    }

    private func openCheckoutFlow() -> XCUIApplication {
        let app = launch(["-wiki", "YES"])
        let tab = app.tabBars.firstMatch.buttons["Wiki"]
        XCTAssertTrue(tab.waitForExistence(timeout: 10))
        tab.tap()
        let row = app.buttons["Checkout flow"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.tap()
        XCTAssertTrue(app.navigationBars["Checkout flow"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.webViews.staticTexts["Checkout flow part 1 explains one step of the shop in detail."].waitForExistence(timeout: 10))
        return app
    }

    private func drag(_ app: XCUIApplication, by points: CGFloat) {
        let start = app.webViews.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.6))
        start.press(forDuration: 0.1, thenDragTo: start.withOffset(CGVector(dx: 0, dy: points)), withVelocity: .slow, thenHoldForDuration: 0.3)
    }

    private func color(at point: CGPoint, in shot: XCUIScreenshot) -> [UInt8] {
        guard let image = shot.image.cgImage else { return [] }
        let scale = CGFloat(image.width) / shot.image.size.width
        var pixel = [UInt8](repeating: 0, count: 4)
        let context = CGContext(data: &pixel, width: 1, height: 1, bitsPerComponent: 8, bytesPerRow: 4, space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        context?.draw(image, in: CGRect(x: -point.x * scale, y: point.y * scale - CGFloat(image.height) + 1, width: CGFloat(image.width), height: CGFloat(image.height)))
        return Array(pixel.prefix(3))
    }

    private func wait(_ condition: @escaping () -> Bool) -> Bool {
        let met = NSPredicate { _, _ in condition() }
        return XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: met, object: nil)], timeout: 5) == .completed
    }

    private func launch(_ extra: [String]) -> XCUIApplication {
        launch(extra, open: "tab:agents")
    }

    private func launch(_ extra: [String], open target: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", target] + extra
        app.launch()
        return app
    }
}
