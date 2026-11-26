import XCTest

@MainActor
final class GuideUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"

    override func setUp() {
        continueAfterFailure = false
    }

    func testStepLabelOpensTheTourAndAConceptShowsOnlyItsOffers() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom == .pad, "the iPad shows the strip or the pane")
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-latency", "20", "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", "agents/agt_0000000000000004", "-guideFixture", "aside"]
        app.launch()
        XCTAssertTrue(app.buttons["guide.step.continue"].waitForExistence(timeout: 30))
        XCTAssertTrue(app.buttons["guide.step.go_deeper"].isSelected)
        shot(app, "steps")
        app.buttons["guide.header"].tap()
        let refill = app.buttons["guide.concept.c4"]
        XCTAssertTrue(refill.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["guide.offer.ask_again"].exists)
        refill.tap()
        XCTAssertTrue(app.buttons["guide.offer.ask_again"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["guide.offer.go_back"].exists)
        shot(app, "tour")
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
        if let dir = ProcessInfo.processInfo.environment["GUIDE_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(filePath: dir).appending(path: "iphone-" + name + ".png"))
        }
    }
}
