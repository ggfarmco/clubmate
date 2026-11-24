import UIKit
import XCTest

@MainActor
final class WorkspacePickerShotsUITests: XCTestCase {
    private static let fixtures = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "CompanionKit/Tests/CompanionKitTests/Fixtures").path()
    private static let pairLink = "clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000"

    override func setUpWithError() throws {
        continueAfterFailure = false
        try XCTSkipUnless(ProcessInfo.processInfo.environment["WSPICKER_SHOTS"] != nil, "run with TEST_RUNNER_WSPICKER_SHOTS=<dir>")
    }

    func testOneMac() {
        let app = launch(["-activity", "YES"])
        openCapsule(app)
        shot(app, "1mac")
    }

    func testThreeMacs() {
        let app = launch(["-macs", "3", "-activity", "YES"])
        openCapsule(app)
        shot(app, "3macs")
        item(app, "MacBook Pro").tap()
        shot(app, "3macs-submenu")
    }

    func testOfflineMac() {
        let app = launch(["-macs", "3", "-activity", "YES"])
        Thread.sleep(forTimeInterval: 20)
        openCapsule(app)
        shot(app, "offline")
        item(app, "MacBook Air").tap()
        shot(app, "offline-submenu")
    }

    func testLongName() {
        let app = launch(["-macs", "3", "-activity", "YES", "-longWorkspace", "YES"])
        openCapsule(app)
        shot(app, "long")
    }

    func testLongNameScrolled() {
        let app = launch(["-macs", "3", "-activity", "YES", "-longWorkspace", "YES"])
        openCapsule(app)
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'infra-terraform-s'")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        for _ in 0..<6 where !row.isHittable {
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.55))
            from.press(forDuration: 0.1, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.1)), withVelocity: .slow, thenHoldForDuration: 0.3)
        }
        XCTAssertTrue(row.isHittable, "the long-name row did not scroll into view")
        shot(app, "long-scrolled")
    }

    func testFallbackWithoutActivity() {
        let app = launch([])
        openCapsule(app)
        shot(app, "no-activity")
    }

    func testIPadMenu() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "iPad only")
        XCUIDevice.shared.orientation = .landscapeLeft
        let app = launch(["-macs", "3", "-activity", "YES", "-longWorkspace", "YES"])
        let window = app.windows.firstMatch
        let turned = NSPredicate { _, _ in window.exists && window.frame.width > window.frame.height }
        XCTAssertEqual(XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: turned, object: nil)], timeout: 10), .completed)
        openCapsule(app)
        shot(app, "ipad")
        hold(name: "ipad")
    }

    func testIPadSidebarHidden() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "iPad only")
        XCUIDevice.shared.orientation = .landscapeLeft
        let app = launch(["-macs", "3", "-activity", "YES", "-longWorkspace", "YES"])
        let hide = app.buttons["Hide Sidebar"]
        XCTAssertTrue(hide.waitForExistence(timeout: 10))
        hide.tap()
        XCTAssertTrue(app.buttons["Show Sidebar"].waitForExistence(timeout: 10))
        Thread.sleep(forTimeInterval: 1.5)
        shot(app, "ipad-hidden")
        hold(name: "ipad-hidden")
        XCTAssertTrue(app.buttons["workspace.capsule"].exists, "no capsule with the sidebar hidden")
        openCapsule(app)
        shot(app, "ipad-hidden-menu")
        hold(name: "ipad-hidden-menu")
    }

    private func hold(name: String) {
        let dir = URL(filePath: ProcessInfo.processInfo.environment["WSPICKER_SHOTS"] ?? "/dev/null")
        try? "up".write(to: dir.appending(path: name + ".ready"), atomically: true, encoding: .utf8)
        let done = dir.appending(path: name + ".done").path()
        for _ in 0..<60 where !FileManager.default.fileExists(atPath: done) {
            Thread.sleep(forTimeInterval: 0.5)
        }
    }

    private func launch(_ extra: [String]) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-fixtures", Self.fixtures, "-autoConfirmPairing", "YES", "-pair", Self.pairLink, "-open", "agents"] + extra
        app.launch()
        return app
    }

    private func openCapsule(_ app: XCUIApplication) {
        let capsule = app.buttons["workspace.capsule"]
        XCTAssertTrue(capsule.waitForExistence(timeout: 10))
        Thread.sleep(forTimeInterval: 3)
        capsule.tap()
        XCTAssertTrue(app.buttons["Pair a Mac"].waitForExistence(timeout: 10))
        Thread.sleep(forTimeInterval: 1)
    }

    private func item(_ app: XCUIApplication, _ label: String) -> XCUIElement {
        let item = app.buttons.matching(NSPredicate(format: "identifier != 'workspace.capsule' AND (label == %@ OR label BEGINSWITH %@)", label, label + ", ")).element(boundBy: 0)
        XCTAssertTrue(item.waitForExistence(timeout: 10))
        return item
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        Thread.sleep(forTimeInterval: 1)
        let screenshot = app.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        let labels = app.buttons.allElementsBoundByIndex.map(\.label).filter { !$0.isEmpty }
        try? labels.joined(separator: "\n").write(to: URL(filePath: ProcessInfo.processInfo.environment["WSPICKER_SHOTS"] ?? "/dev/null").appending(path: name + ".labels.txt"), atomically: true, encoding: .utf8)
        try? screenshot.pngRepresentation.write(to: URL(filePath: ProcessInfo.processInfo.environment["WSPICKER_SHOTS"] ?? "/dev/null").appending(path: name + ".png"))
    }
}
