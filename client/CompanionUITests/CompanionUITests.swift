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
        app.buttons["Attach Photos or Videos"].tap()
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

    func testBackToMacsNeverShowsChooseAMac() {
        let app = launch(open: "sidebar")
        let macs = app.navigationBars["Macs"]
        XCTAssertTrue(macs.waitForExistence(timeout: 10))
        for (row, title, swipe) in [("shop", "shop", false), ("shop", "shop", true), ("Inbox", "Inbox", false)] {
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
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS '· AFK'")).firstMatch.waitForExistence(timeout: 5))
    }

    private func launch(open target: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", target]
        app.launch()
        return app
    }

    private func wait(for element: XCUIElement, _ predicate: String) -> Bool {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: predicate), object: element)
        return XCTWaiter().wait(for: [expectation], timeout: 5) == .completed
    }
}
