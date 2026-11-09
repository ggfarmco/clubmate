import XCTest

final class AutomationTests: E2ETestCase {
    private func addJob(_ name: String, _ command: String, _ args: [String: Any]) throws -> String {
        let text = String(decoding: try JSONSerialization.data(withJSONObject: args), as: UTF8.self)
        let job = try mac.run("cron.add", ["name": name, "command": command, "args": text, "every": "24h"], agent: true) as? [String: Any]
        return try XCTUnwrap(job?["id"] as? String)
    }

    func testAFailedCronRunsShowLogOpensItsRun() throws {
        let job = try addJob("E2E cron failing", "agents.prompt", ["session": "agt_0000000000000000", "text": "nobody is there"])
        _ = try mac.run("cron.run", ["id": job])
        let app = try launch(open: "more/cron/\(job)")
        let log = app.buttons["Show log"].firstMatch
        XCTAssertTrue(log.waitForExistence(timeout: 15))
        sleep(2)
        log.tap()
        eventually("the Mac to open the failed run's JSON") {
            String(decoding: try JSONSerialization.data(withJSONObject: try mac.ui("cron.main") ?? [:]), as: UTF8.self).contains("Hide the selected run")
        }
        XCTAssertTrue(app.buttons["Hide the selected run's JSON"].waitForExistence(timeout: 10), "the phone kept the old cron.main render")
    }

    func testAFailedTriggerRunsOpenSessionOpensTheSession() throws {
        let target = try mac.newSession("E2E trigger target")
        eventually("the target's turn to end") { try status(target) == "done" }
        _ = try mac.run("agents.close", ["session": target])
        eventually("the target to close") { try status(target) == "closed" }
        let added = try mac.run("triggers.add", ["name": "E2E poke", "on": "event.e2e.poke", "command": "agents.prompt", "args": "{\"session\":\"\(target)\",\"text\":\"poke\"}"], agent: true) as? [String: Any]
        let trigger = try XCTUnwrap(added?["id"] as? String)
        _ = try mac.run("triggers.emit", ["name": "e2e.poke"], agent: true)
        let app = try launch(open: "more/triggers/\(trigger)")
        let open = app.buttons["Open session"].firstMatch
        XCTAssertTrue(open.waitForExistence(timeout: 20), "the trigger's run didn't fail with the session to open")
        sleep(2)
        open.tap()
        XCTAssertTrue(app.navigationBars["E2E trigger target"].waitForExistence(timeout: 10), "Open session didn't open the run's session")
    }

    func testAGraphRunsAgentTurnShowsItsSummaryAndOpensTheSession() throws {
        let run = try XCTUnwrap((try mac.run("graphs.run", ["graph": "e2e", "input": "summary"]) as? [String: Any])?["id"] as? String)
        eventually("the coder's turn to be summarized", timeout: 30) {
            let show = try mac.run("graphs.show", ["id": run])
            return String(decoding: try JSONSerialization.data(withJSONObject: show), as: UTF8.self).contains("ticked twice")
        }
        let app = try launch(open: "more/graphs/\(run)")
        let coder = app.otherElements["sdui.graph.nodes"].buttons.matching(NSPredicate(format: "label BEGINSWITH 'Coder'")).firstMatch
        XCTAssertTrue(coder.waitForExistence(timeout: 15))
        coder.tap()
        XCTAssertTrue(text(app, containing: "The agent ticked twice and stopped.").waitForExistence(timeout: 15))
        XCTAssertTrue(app.staticTexts["summary by Haiku"].exists)
        screenshot(app, "turn-panel")
        let open = app.buttons["Open Coder"].firstMatch
        XCTAssertTrue(open.exists)
        open.tap()
        XCTAssertTrue(text(app, containing: "E2E graph coder").waitForExistence(timeout: 10), "Open Coder didn't open the coder's session")
        _ = try mac.run("graphs.stop", ["id": run])
    }
}
