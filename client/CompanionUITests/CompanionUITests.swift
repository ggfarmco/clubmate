import XCTest

@MainActor
final class CompanionUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"

    override func setUp() {
        continueAfterFailure = false
    }

    func testNewSessionSendsPhoto() {
        let app = launch(open: "agents")
        app.navigationBars.buttons["New Session"].tap()
        let composer = app.textFields["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: composer, "hasKeyboardFocus == true"))
        app.buttons["Attach"].tap()
        app.buttons["Photos or Videos"].tap()
        let photo = app.images.matching(NSPredicate(format: "label BEGINSWITH 'Photo'")).firstMatch
        XCTAssertTrue(photo.waitForExistence(timeout: 10))
        photo.tap()
        app.buttons["Add"].tap()
        XCTAssertTrue(app.buttons["Remove"].waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText("Fix the layout")
        let send = app.buttons["Send"]
        XCTAssertTrue(wait(for: send, "isEnabled == true"))
        send.tap()
        XCTAssertTrue(app.staticTexts["1 image"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.navigationBars["Fix the layout"].waitForExistence(timeout: 5))
    }

    func testNewSessionSendsFile() {
        let app = launch(open: "agents")
        app.navigationBars.buttons["New Session"].tap()
        let composer = app.textFields["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 5))
        app.buttons["Attach"].tap()
        add(screenshot(app, "attach-menu"))
        app.buttons["Files"].tap()
        let browse = app.buttons["Browse"]
        XCTAssertTrue(browse.waitForExistence(timeout: 10))
        browse.tap()
        let file = app.cells.matching(NSPredicate(format: "identifier BEGINSWITH 'crash-logs.zip'")).firstMatch
        XCTAssertTrue(file.waitForExistence(timeout: 10))
        file.tap()
        if app.buttons["Open"].waitForExistence(timeout: 2) { app.buttons["Open"].tap() }
        XCTAssertTrue(app.staticTexts["crash-logs.zip"].waitForExistence(timeout: 10))
        add(screenshot(app, "file-chip"))
        composer.tap()
        composer.typeText("Why does it crash")
        app.buttons["Send"].tap()
        XCTAssertTrue(app.navigationBars["Why does it crash"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["crash-logs.zip"].waitForExistence(timeout: 10))
        add(screenshot(app, "sent-file"))
    }

    func testASubagentThatNeedsYouIsNeverFolded() {
        let app = launch(open: "agents")
        XCTAssertTrue(app.staticTexts["Explain the checkout flow"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Show 1 subagents"].exists)
    }

    func testSubagentStopIsInTheCardMenuAndAsksFirst() {
        let app = launch(open: "agents/agt_0000000000000001")
        let card = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Explore: Check the cart tests")).firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Stop subagent"].exists)
        card.press(forDuration: 1)
        app.buttons["Stop Subagent…"].tap()
        let confirm = app.buttons["Stop Subagent"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 5))
        confirm.tap()
        let running = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Running ·")).firstMatch
        wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: running)], timeout: 5)
    }

    func testBacklogOpensFromTheWorkspaceNotTheMac() {
        let app = launch(open: "sidebar")
        XCTAssertTrue(app.navigationBars["Macs"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Backlog"].exists)
        app.staticTexts["shop"].tap()
        let backlog = app.buttons["Backlog"]
        XCTAssertTrue(backlog.waitForExistence(timeout: 5))
        backlog.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Retry the charge step on 502")).firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.navigationBars["shop"].exists)
    }

    func testBackToMacsNeverShowsChooseAMac() {
        let app = launch(open: "sidebar")
        let macs = app.navigationBars["Macs"]
        XCTAssertTrue(macs.waitForExistence(timeout: 10))
        for (row, title, swipe) in [("shop", "shop", false), ("shop", "shop", true), ("Inbox", "Inbox", false)] {
            reveal(app.staticTexts[row], in: app)
            app.staticTexts[row].tap()
            XCTAssertTrue(app.navigationBars[title].waitForExistence(timeout: 5))
            if swipe {
                let edge = app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5))
                edge.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)))
            } else {
                app.navigationBars[title].buttons.firstMatch.tap()
            }
            XCTAssertTrue(macs.waitForExistence(timeout: 5))
            XCTAssertFalse(app.staticTexts["Choose a Mac"].waitForExistence(timeout: 2))
            XCTAssertTrue(macs.exists)
        }
    }

    func testSendQueuesAndStopAsksOnceFromTheMenu() {
        let app = launch(open: "agents/agt_0000000000000002")
        let composer = app.textFields["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText("Add retries")
        app.buttons["Send"].tap()
        XCTAssertTrue(app.staticTexts["Add retries"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Queue"].waitForExistence(timeout: 5))
        app.navigationBars.buttons["More"].tap()
        app.buttons["Stop…"].tap()
        let stop = app.buttons["Stop"]
        XCTAssertTrue(stop.waitForExistence(timeout: 5))
        stop.tap()
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.staticTexts["Working…"].exists)
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Got it'")).firstMatch.waitForExistence(timeout: 5))
    }

    func testAlwaysAllowConfirmsInASheetThenCollapsesToARow() {
        let app = launch(open: "agents/agt_0000000000000001")
        let always = app.buttons["Always allow…"]
        XCTAssertTrue(always.waitForExistence(timeout: 10))
        always.tap()
        XCTAssertTrue(app.staticTexts["Always allow this command?"].waitForExistence(timeout: 5))
        app.buttons["Always allow"].tap()
        XCTAssertTrue(app.staticTexts["Always allowed · go test ./..."].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Allow once"].exists)
    }

    func testTurnOnAFKFromSessionSettings() {
        let app = launch(open: "agents/agt_0000000000000001")
        let more = app.navigationBars.buttons["More"]
        XCTAssertTrue(more.waitForExistence(timeout: 10))
        more.tap()
        app.buttons["Session Settings"].tap()
        let afk = app.switches["settings.afk"]
        XCTAssertTrue(afk.waitForExistence(timeout: 5))
        afk.switches.firstMatch.tap()
        XCTAssertTrue(wait(for: afk, "value == '1'"))
        app.buttons["Done"].tap()
        more.tap()
        app.buttons["Session Settings"].tap()
        XCTAssertTrue(app.switches["settings.afk"].waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: app.switches["settings.afk"], "value == '1'"))
    }

    func testOptionKeysAnswerThePermissionInButtonOrder() {
        let app = launch(open: "agents/agt_0000000000000001")
        XCTAssertTrue(app.buttons["Reject"].waitForExistence(timeout: 10))
        app.typeKey("2", modifierFlags: .option)
        XCTAssertTrue(app.buttons["Reject"].exists)
        app.typeKey("1", modifierFlags: .option)
        XCTAssertTrue(wait(for: app.buttons["Reject"], "exists == false"))
    }

    func testRotatingKeepsTheOpenSession() {
        let app = launch(open: "agents/agt_0000000000000002")
        let title = app.navigationBars["Explain the checkout flow"]
        XCTAssertTrue(title.waitForExistence(timeout: 10))
        for orientation in [UIDeviceOrientation.landscapeLeft, .portrait] {
            XCUIDevice.shared.orientation = orientation
            XCTAssertTrue(title.waitForExistence(timeout: 5))
            XCTAssertTrue(app.textFields["composer"].exists)
        }
    }

    func testDocLinkPushesUpdatesLiveAndBackReturns() {
        let app = launch(open: "agents", ["-doc", "/Users/me/src/shop/docs/checkout-retries.md"])
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.navigationBars.buttons["Back"].exists)
        add(screenshot(app, "doc-top"))
        let link = app.webViews.links["payments overview"]
        XCTAssertTrue(link.waitForExistence(timeout: 5))
        link.tap()
        XCTAssertTrue(app.navigationBars["Payments overview"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.webViews.staticTexts["Updated while you were reading."].waitForExistence(timeout: 10))
        add(screenshot(app, "doc-linked-updated"))
        app.navigationBars.buttons["Back"].tap()
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.navigationBars.buttons["Back"].exists)
        for index in 1...3 {
            app.webViews.firstMatch.swipeUp()
            add(screenshot(app, "doc-scroll-\(index)"))
        }
        app.buttons["Done"].tap()
        XCTAssertTrue(wait(for: app.navigationBars["Checkout retries"], "exists == false"))
    }

    func testDocOpensFromTheBannerAndAToolRow() {
        let app = launch(open: "agents/agt_0000000000000001", ["-docOpened", "YES"])
        let banner = app.buttons["Opened checkout-retries.md"]
        XCTAssertTrue(banner.waitForExistence(timeout: 10))
        add(screenshot(app, "doc-banner"))
        app.buttons["Open"].tap()
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 10))
        app.buttons["Done"].tap()
        XCTAssertTrue(wait(for: banner, "exists == false"))
        let group = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '3 actions'")).firstMatch
        reveal(group, in: app)
        group.tap()
        let open = app.buttons["Open checkout-retries.md"]
        reveal(open, in: app)
        add(screenshot(app, "doc-tool-row"))
        open.tap()
        XCTAssertTrue(app.navigationBars["Checkout retries"].waitForExistence(timeout: 10))
    }

    private func launch(open target: String, _ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", target] + extra
        app.launch()
        return app
    }

    private func screenshot(_ app: XCUIApplication, _ name: String) -> XCTAttachment {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        return shot
    }

    private func reveal(_ element: XCUIElement, in app: XCUIApplication) {
        for _ in 0..<5 where !(element.exists && element.isHittable) {
            app.swipeUp()
        }
    }

    private func wait(for element: XCUIElement, _ predicate: String) -> Bool {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: predicate), object: element)
        return XCTWaiter().wait(for: [expectation], timeout: 5) == .completed
    }
}
