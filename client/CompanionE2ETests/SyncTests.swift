import XCTest

final class SyncTests: E2ETestCase {
    func testAnAddCutMidWriteLandsOnce() throws {
        let title = "E2E add cut mid-write"
        let app = try launch(open: "backlog/new")
        let field = app.textFields["taskTitle"]
        XCTAssertTrue(field.waitForExistence(timeout: 15))
        field.typeText(title)
        try mac.freezeCore()
        addTeardownBlock { [mac] in try? mac?.thawCore() }
        app.buttons["Add"].tap()
        XCTAssertTrue(app.staticTexts["No Task Selected"].waitForExistence(timeout: 40))
        app.navigationBars.buttons["BackButton"].tap()
        XCTAssertTrue(text(app, containing: "Add “").waitForExistence(timeout: 10))
        try mac.thawCore()
        eventually("the task on the Mac", timeout: 120) { try mac.tasks(titled: title) > 0 }
        XCTAssertTrue(wait(for: text(app, containing: "Add “"), "exists == false", timeout: 60))
        XCTAssertEqual(try mac.tasks(titled: title), 1)
    }

    func testListsSurviveARestartWhileTheMacIsAway() throws {
        let title = "E2E kept on the phone"
        _ = try mac.run("backlog.add", ["title": title])
        var app = try launch(open: "backlog")
        XCTAssertTrue(text(app, containing: title).waitForExistence(timeout: 20))
        RunLoop.current.run(until: .now.addingTimeInterval(6))
        try mac.setRemote(false)
        addTeardownBlock { [mac] in try? mac?.setRemote(true) }
        app.terminate()
        app = try launch(open: "backlog")
        XCTAssertTrue(text(app, containing: title).waitForExistence(timeout: 20))
        screenshot(app, "backlog-offline-restart")
    }
}
