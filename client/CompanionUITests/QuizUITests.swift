import XCTest

@MainActor
final class QuizUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"

    override func setUp() {
        continueAfterFailure = false
    }

    func testTapPicksALineAndUndoTakesTheRatingBack() {
        let app = launch("asking")
        shot(app, "peek")
        line(31, in: app).tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Line 31 ")).firstMatch.waitForExistence(timeout: 10))
        app.buttons["How sure? sure"].tap()
        let undo = app.buttons["Undo"]
        XCTAssertTrue(undo.waitForExistence(timeout: 5))
        shot(app, "sending")
        undo.tap()
        XCTAssertTrue(wait(for: undo, "exists == false"))
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Line 31 ")).firstMatch.exists)
    }

    func testFoldsToABarAndTheHoldEndsInTheRecord() {
        let app = launch("asking")
        app.buttons["Fold"].tap()
        let bar = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Claude is asking'")).firstMatch
        XCTAssertTrue(bar.waitForExistence(timeout: 5))
        shot(app, "bar")
        bar.swipeUp()
        XCTAssertTrue(app.buttons["How sure? think so"].waitForExistence(timeout: 5))
        let card = app.otherElements["quiz-card"]
        card.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.02)).press(forDuration: 0.1, thenDragTo: card.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: -0.4)))
        XCTAssertTrue(line(29, in: app).waitForExistence(timeout: 5))
        shot(app, "full")
        app.buttons["How sure? think so"].tap()
        XCTAssertTrue(app.otherElements["quiz-record"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.otherElements["quiz-card"].exists)
        app.swipeUp()
        shot(app, "record")
    }

    func testTypedAnswerShowsTheRatingsAboveSend() {
        let app = launch("text")
        let composer = app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText("It keeps a burst from growing without end")
        XCTAssertTrue(app.segmentedControls["quiz-rating"].waitForExistence(timeout: 5))
        shot(app, "typed")
    }

    func testPadDigitRatesAndSends() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "iPad keys")
        connectKeyboard()
        let app = launch("asking")
        XCTAssertTrue(app.descendants(matching: .any)["quiz.focus"].waitForExistence(timeout: 10))
        app.typeKey(.upArrow, modifierFlags: [])
        app.typeKey("2", modifierFlags: [])
        XCTAssertTrue(app.buttons["Undo"].waitForExistence(timeout: 5))
        shot(app, "keys-sending")
        XCTAssertTrue(app.otherElements["quiz-record"].waitForExistence(timeout: 10))
    }

    private func connectKeyboard() {
        let app = launch("answered", card: "quiz-record")
        app.typeKey(XCUIKeyboardKey.shift, modifierFlags: [])
        app.terminate()
    }

    private func launch(_ quiz: String, card: String = "quiz-card") -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-latency", "20", "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", "agents/agt_0000000000000004", "-quizFixture", quiz]
        app.launch()
        XCTAssertTrue(app.otherElements[card].waitForExistence(timeout: 30))
        return app
    }

    private func line(_ n: Int, in app: XCUIApplication) -> XCUIElement {
        let element = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Line \(n):")).firstMatch
        XCTAssertTrue(element.waitForExistence(timeout: 10))
        return element
    }

    private func wait(for element: XCUIElement, _ predicate: String) -> Bool {
        XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: predicate), object: element)], timeout: 5) == .completed
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
        if let dir = ProcessInfo.processInfo.environment["QUIZ_SHOTS"] {
            let device = UIDevice.current.userInterfaceIdiom == .pad ? "ipad-" : "iphone-"
            try? app.screenshot().pngRepresentation.write(to: URL(filePath: dir).appending(path: device + name + ".png"))
        }
    }
}
