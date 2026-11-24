import XCTest

final class ConnectionTests: E2ETestCase {
    func testRunCheckPassesOnARealCoreAndNamesARefusedPort() throws {
        let app = try launch(open: "connection")
        let run = app.buttons["connection.run"]
        XCTAssertTrue(run.waitForExistence(timeout: 20))
        run.tap()
        let events = app.descendants(matching: .any)["connection.step.events"]
        XCTAssertTrue(wait(for: events, "label CONTAINS 'said “connected”'", timeout: 20))
        let port = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'connection.step.tcp:'")).firstMatch
        XCTAssertTrue(port.label.contains("Answers on port"), port.label)

        try mac.setRemote(false)
        addTeardownBlock { [mac] in try? mac?.setRemote(true) }
        run.tap()
        XCTAssertTrue(wait(for: port, "label CONTAINS 'companion access on'", timeout: 20))
        XCTAssertTrue(wait(for: app.descendants(matching: .any)["connection.step.hello"], "label CONTAINS 'no address answered'"))
        screenshot(app, "connection-refused")
    }

    func testARevokedMacPairedAgainGetsItsTabsBack() throws {
        var app = try launch(open: "agents")
        let more = app.tabBars.buttons["More"]
        XCTAssertTrue(more.waitForExistence(timeout: 20))
        try mac.revokeDevices()
        XCTAssertTrue(wait(for: more, "exists == false", timeout: 30))
        app.terminate()
        app = try launch(open: "agents")
        XCTAssertTrue(app.tabBars.buttons["More"].waitForExistence(timeout: 20))
    }
}
