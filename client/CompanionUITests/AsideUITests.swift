import XCTest

@MainActor
final class AsideUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"
    private static let parent = "agents/agt_0000000000000004"

    override func setUp() {
        continueAfterFailure = false
    }

    func testLongPressOptionOpensAsideSheet() {
        let app = launch(["-asideFixture", "empty"])
        longPress(option("10 minutes", in: app))
        app.buttons["Ask aside"].tap()
        XCTAssertTrue(app.staticTexts["Aside · 10 minutes"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.otherElements["aside.forked"].exists || app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Forked from the question'")).firstMatch.exists)
        shot(app, "option-sheet")
    }

    func testLongPressOnTapAnswerCardDoesNotAnswer() {
        let app = launch(["-asideFixture", "empty", "-questionSingle", "YES"])
        longPress(option("24 hours", in: app))
        XCTAssertTrue(app.buttons["Ask aside"].waitForExistence(timeout: 5))
        app.buttons["Ask aside"].tap()
        XCTAssertTrue(app.staticTexts["Aside · 24 hours"].waitForExistence(timeout: 10))
        app.swipeDown(velocity: .fast)
        XCTAssertTrue(app.otherElements["elicitation-card"].waitForExistence(timeout: 5))
        XCTAssertFalse(option("24 hours", in: app).isSelected)
    }

    func testLongPressMessageAsksAside() {
        let app = launch(["-asideFixture", "empty"])
        let message = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Before I add the expiry job'")).firstMatch
        XCTAssertTrue(message.waitForExistence(timeout: 10))
        message.press(forDuration: 1.2)
        XCTAssertTrue(app.buttons["Copy"].waitForExistence(timeout: 5))
        shot(app, "message-menu")
        app.buttons["Ask aside"].tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Forked from a message'")).firstMatch.waitForExistence(timeout: 10))
    }

    func testExpandAndBackThreeTimes() {
        let app = launch(["-asideFixture", "YES"])
        openAside(app)
        for round in 0..<3 {
            app.buttons["aside.expand"].tap()
            XCTAssertTrue(app.buttons["aside.strip.30 minutes"].waitForExistence(timeout: 10), "round \(round)")
            if round == 0 { shot(app, "full") }
            if round == 2 {
                app.buttons["aside.collapse"].tap()
            } else {
                app.navigationBars.buttons.element(boundBy: 0).tap()
            }
            XCTAssertTrue(app.buttons["aside.expand"].waitForExistence(timeout: 10), "round \(round)")
        }
    }

    func testStripPickPrefillsParentCard() {
        let app = launch(["-asideFixture", "YES"])
        openAside(app)
        app.buttons["aside.expand"].tap()
        let chip = app.buttons["aside.strip.10 minutes"]
        XCTAssertTrue(chip.waitForExistence(timeout: 10))
        chip.tap()
        XCTAssertTrue(chip.wait(for: \.isSelected, toEqual: true, timeout: 10))
        shot(app, "strip-picked")
        app.buttons["aside.collapse"].tap()
        XCTAssertTrue(app.buttons["aside.expand"].waitForExistence(timeout: 10))
        app.swipeDown(velocity: .fast)
        XCTAssertTrue(option("10 minutes", in: app).wait(for: \.isSelected, toEqual: true, timeout: 10))
    }

    func testSendBackAnswerPrefillsCard() {
        let app = launch(["-asideFixture", "YES"])
        openAside(app)
        app.buttons["aside.sendBack"].tap()
        XCTAssertTrue(app.buttons["Answer question 1"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.textViews.matching(NSPredicate(format: "value CONTAINS 'stock waits for the shopper'")).firstMatch.waitForExistence(timeout: 10))
        let send = app.buttons["sendBack.send"]
        XCTAssertTrue(send.isEnabled)
        shot(app, "send-back")
        send.tap()
        XCTAssertTrue(option("30 minutes", in: app).wait(for: \.isSelected, toEqual: true, timeout: 10))
        XCTAssertTrue(app.textViews.matching(NSPredicate(format: "value CONTAINS 'stock waits for the shopper'")).firstMatch.waitForExistence(timeout: 5)
            || app.textFields.matching(NSPredicate(format: "value CONTAINS 'stock waits for the shopper'")).firstMatch.exists)
    }

    func testSendBackDraftFillsComposer() {
        let app = launch(["-asideFixture", "many"])
        let pill = app.buttons["asides.pill"]
        XCTAssertTrue(pill.waitForExistence(timeout: 10))
        pill.tap()
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Aside · “'")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        shot(app, "pill-list")
        row.tap()
        XCTAssertTrue(app.buttons["aside.sendBack"].waitForExistence(timeout: 10))
        app.buttons["aside.sendBack"].tap()
        XCTAssertFalse(app.buttons["Answer question 1"].waitForExistence(timeout: 2))
        let send = app.buttons["sendBack.send"]
        XCTAssertTrue(send.wait(for: \.isEnabled, toEqual: true, timeout: 10))
        send.tap()
        XCTAssertTrue(app.textViews.matching(NSPredicate(format: "value BEGINSWITH '> `reserve.go`'")).firstMatch.waitForExistence(timeout: 10))
        shot(app, "draft")
    }

    func testAsideOpenedAsTheDetailHasItsChrome() {
        let app = launch(["-asideFixture", "YES"], open: "agents/agt_00000000000000b1")
        XCTAssertTrue(app.buttons["aside.sendBack"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["aside.strip.30 minutes"].exists)
        app.buttons["aside.collapse"].tap()
        XCTAssertTrue(app.buttons["aside.expand"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.otherElements["elicitation-card"].exists)
    }

    func testTypeAndSendInTheSheet() {
        let app = launch(["-asideFixture", "YES"])
        openAside(app)
        let field = app.textViews.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText("And on a sale day?")
        shot(app, "sheet-typing")
        app.buttons["Send"].tap()
        XCTAssertTrue(app.staticTexts["And on a sale day?"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["aside.expand"].exists)
    }

    func testKeepOpensSession() {
        let app = launch(["-asideFixture", "YES"])
        openAside(app)
        app.buttons["aside.keep"].tap()
        XCTAssertTrue(app.staticTexts["30 minutes"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["aside.expand"].exists)
    }

    func testCloseAsksWhenUnsent() {
        let app = launch(["-asideFixture", "YES"])
        openAside(app)
        app.buttons["aside.close"].tap()
        XCTAssertTrue(app.staticTexts["Close without sending?"].waitForExistence(timeout: 10))
        app.buttons["Close"].tap()
        XCTAssertTrue(app.otherElements["elicitation-card"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["aside.expand"].exists)
    }

    private func launch(_ extra: [String], open target: String = parent) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-latency", "20", "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", target] + extra
        app.launch()
        return app
    }

    private func option(_ label: String, in app: XCUIApplication) -> XCUIElement {
        let element = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", label)).firstMatch
        XCTAssertTrue(element.waitForExistence(timeout: 10))
        return element
    }

    private func longPress(_ element: XCUIElement) {
        element.press(forDuration: 1.2)
    }

    private func openAside(_ app: XCUIApplication) {
        longPress(option("30 minutes", in: app))
        app.buttons["Open aside"].tap()
        XCTAssertTrue(app.buttons["aside.expand"].waitForExistence(timeout: 10))
        shot(app, "sheet")
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
        if let dir = ProcessInfo.processInfo.environment["ASIDE_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(filePath: dir).appending(path: name + ".png"))
        }
    }
}
