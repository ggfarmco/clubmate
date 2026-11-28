import UIKit
import XCTest

@MainActor
final class SlotWaitUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"
    private static let waiting = "agt_00000000000005a2"
    private static let grouped = "agt_00000000000005a4"
    private static let denied = "agt_00000000000005a3"

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    func testWaitingCardShowsTheWaitAndHeldByOpensTheHolder() {
        let app = launch(Self.waiting)
        let pad = UIDevice.current.userInterfaceIdiom == .pad
        let card = row(app, "make test")
        XCTAssertTrue(card.waitForExistence(timeout: 20))
        XCTAssertTrue(card.label.contains(pad ? "waits for test slot" : "Waits for test slot · next in line"), card.label)
        XCTAssertFalse(app.activityIndicators.firstMatch.exists)
        XCTAssertFalse(app.staticTexts["Working…"].exists)
        shot(app, "slot-wait")
        card.tap()
        let wait = app.staticTexts["tool.slotWait"]
        XCTAssertTrue(wait.waitForExistence(timeout: 5))
        XCTAssertTrue(wait.label.hasPrefix("Next in line · waited "))
        let held = app.buttons["tool.slotHeld"]
        XCTAssertTrue(held.exists)
        XCTAssertTrue(held.label.hasPrefix("Held by Companion UI tests · xcodebuild -scheme Companion test"))
        shot(app, "slot-wait-expanded")
        held.tap()
        XCTAssertTrue(row(app, "xcodebuild -scheme Companion test").waitForExistence(timeout: 10))
        XCTAssertFalse(row(app, "make test").exists)
    }

    func testGroupWithAWaitingCallShowsTheHourglass() {
        let app = launch(Self.grouped)
        let pad = UIDevice.current.userInterfaceIdiom == .pad
        let group = row(app, pad ? "go test" : "2 actions")
        XCTAssertTrue(group.waitForExistence(timeout: 20))
        if pad {
            XCTAssertTrue(group.label.contains("waits for test slot"), group.label)
        }
        XCTAssertFalse(app.activityIndicators.firstMatch.exists)
        shot(app, "slot-wait-group")
    }

    func testDeniedCallReadsNotRun() {
        let app = launch(Self.denied)
        let pad = UIDevice.current.userInterfaceIdiom == .pad
        let card = row(app, "make test")
        XCTAssertTrue(card.waitForExistence(timeout: 20))
        XCTAssertTrue(card.label.contains(pad ? "not run · no test slot in 8 min" : "Not run · no test slot in 8 min"), card.label)
        shot(app, "slot-denied")
    }

    private func launch(_ session: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-landQueueFixture", Self.fixtures + "/slots", "-open", "agents/" + session]
        app.launch()
        return app
    }

    private func row(_ app: XCUIApplication, _ text: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name + (UIDevice.current.userInterfaceIdiom == .pad ? "-ipad" : "-iphone")
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
