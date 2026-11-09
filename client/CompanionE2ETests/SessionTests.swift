import XCTest

final class SessionTests: E2ETestCase {
    func testTwoQuestionsAnsweredOnThePhoneReachTheAgent() throws {
        let session = try mac.newSession("E2E two questions")
        eventually("the session to ask") { try status(session) == "waiting" }
        let app = try launch(open: "agents/\(session)")
        let card = app.otherElements["elicitation-card"]
        XCTAssertTrue(card.staticTexts["1 of 2"].waitForExistence(timeout: 15))
        let postgres = option("Postgres", in: card)
        XCTAssertTrue(wait(for: postgres, "isEnabled == true"))
        postgres.tap()
        card.buttons["Next"].tap()
        XCTAssertTrue(card.staticTexts["2 of 2"].waitForExistence(timeout: 5))
        let memcached = option("Memcached", in: card)
        XCTAssertTrue(wait(for: memcached, "isEnabled == true"))
        memcached.tap()
        card.buttons["Send"].tap()
        eventually("the agent to get both answers") {
            let answers = try agentCalls("elicitation_response").compactMap { ($0["params"] as? [String: Any])?["content"] as? [String: Any] }
            return answers.contains { $0["question_0"] as? String == "Postgres" && $0["question_1"] as? String == "Memcached" }
        }
        eventually("the Mac's session to finish its turn") { try status(session) == "done" }
        XCTAssertTrue(app.staticTexts["Got both answers"].waitForExistence(timeout: 10))
    }

    func testANumberQuestionSendsTheNumber() throws {
        let session = try mac.newSession("E2E number")
        eventually("the session to ask") { try status(session) == "waiting" }
        let app = try launch(open: "agents/\(session)")
        let card = app.otherElements["elicitation-card"]
        let field = card.textFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 15))
        screenshot(app, "number-page")
        field.tap()
        field.typeText("4")
        card.buttons["Send"].tap()
        eventually("the agent to get the number") {
            try agentCalls("elicitation_response").contains { (($0["params"] as? [String: Any])?["content"] as? [String: Any])?["workers"] as? Int == 4 }
        }
        eventually("the Mac's session to finish its turn") { try status(session) == "done" }
    }

    func testAnOpenSessionShowsTheAgentsNextStep() throws {
        let session = try mac.newSession("E2E live")
        let app = try launch(open: "agents/\(session)")
        XCTAssertTrue(step(app, "Live one").waitForExistence(timeout: 15))
        try mac.signal("live-1")
        XCTAssertTrue(step(app, "Live two").waitForExistence(timeout: 10))
        try mac.signal("live-2")
    }

    func testAReopenedSessionKeepsUpdatingAndTakesItsAnswer() throws {
        let session = try mac.newSession("E2E stream")
        let app = try launch(open: "agents/\(session)")
        XCTAssertTrue(step(app, "Tick one").waitForExistence(timeout: 15))
        app.navigationBars.buttons.firstMatch.tap()
        let row = app.staticTexts["E2E stream"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.tap()
        XCTAssertTrue(step(app, "Tick one").waitForExistence(timeout: 5))
        try mac.signal("stream-1")
        XCTAssertTrue(step(app, "Tick two").waitForExistence(timeout: 10), "the reopened screen stopped refreshing its transcript (#282)")
        try mac.signal("stream-2")
        let card = app.otherElements["elicitation-card"]
        let redis = option("Redis", in: card)
        XCTAssertTrue(redis.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: redis, "isEnabled == true"))
        redis.tap()
        eventually("the agent to get the answer") {
            try agentCalls("elicitation_response").contains { (($0["params"] as? [String: Any])?["content"] as? [String: Any])?["question_0"] as? String == "Redis" }
        }
        XCTAssertTrue(step(app, "After the answer").waitForExistence(timeout: 10))
        XCTAssertFalse(text(app, containing: "No longer pending").exists)
        XCTAssertTrue(text(app, containing: "Redis").exists)
    }

    func testReopenTakesAClosedSessionOutOfClosedOnBothSides() throws {
        let session = try mac.newSession("E2E reopen")
        eventually("the first turn to end") { try status(session) == "done" }
        _ = try mac.run("agents.close", ["session": session])
        eventually("the session to close") { try status(session) == "closed" }
        let app = try launch(open: "agents/\(session)")
        let reopen = app.buttons["Reopen"]
        XCTAssertTrue(reopen.waitForExistence(timeout: 15))
        reopen.tap()
        XCTAssertTrue(app.textViews["composer"].waitForExistence(timeout: 10))
        XCTAssertFalse(reopen.exists)
        eventually("the Mac's session to leave Closed") { try status(session).map { $0 != "closed" } ?? false }
    }
}
