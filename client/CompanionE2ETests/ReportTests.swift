import XCTest

final class ReportTests: E2ETestCase {
    func testAReportWrittenOfflineReachesTheRelayWithMacLogs() throws {
        var app = try launch(open: "sidebar")
        XCTAssertTrue(app.tabBars.buttons["More"].waitForExistence(timeout: 20))
        app.terminate()
        try mac.setRemote(false)
        addTeardownBlock { [mac] in try? mac?.setRemote(true) }

        app = try launch(open: "report")
        let words = app.descendants(matching: .any)["report.text"]
        XCTAssertTrue(words.waitForExistence(timeout: 20))
        XCTAssertTrue(wait(for: app.staticTexts["report.status"], "label CONTAINS 'is offline'", timeout: 30))
        words.tap()
        words.typeText("E2E report while the Mac is away")
        app.buttons["report.send"].tap()
        let more = app.tabBars.buttons["More"]
        XCTAssertTrue(more.waitForExistence(timeout: 10))
        more.tap()
        XCTAssertTrue(text(app, containing: "Problem report").waitForExistence(timeout: 10))

        try mac.setRemote(true)
        eventually("the report on the relay", timeout: 120) { try !mac.reports().isEmpty }
        let files = try mac.reports().first?["files"] as? [String] ?? []
        XCTAssertTrue(files.contains("logs/plugins/remote.log"), "\(files)")
        XCTAssertTrue(files.contains("phone/events.log"), "\(files)")
        XCTAssertFalse(files.contains("sessions.json"), "\(files)")
        XCTAssertFalse(files.contains("phone/outbox.json"), "\(files)")
    }
}
