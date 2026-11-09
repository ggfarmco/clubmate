import XCTest

final class OpenItemsTests: E2ETestCase {
    override func setUp() async throws {
        try await super.setUp()
        for entry in try mac.run("agents.open-queue") as? [[String: Any]] ?? [] {
            if let session = entry["session"] as? String {
                _ = try mac.run("agents.open-no", ["session": session])
            }
        }
    }

    private func decision(_ header: String, _ question: String, _ options: [String]) -> [String: Any] {
        ["kind": "decision", "header": header, "question": question, "options": options, "descriptions": options.map { "Pick \($0)" }, "quote": question]
    }

    private func offer(_ label: String) -> [String: Any] {
        ["kind": "offer", "label": label, "description": "One line about it", "quote": label]
    }

    private func sessionWithItems(_ prompt: String, _ items: [[String: Any]]) throws -> String {
        let session = try mac.newSession(prompt)
        eventually("the turn to end") { try status(session) == "done" }
        _ = try mac.run("agents.open-items", ["session": session, "items": items])
        eventually("the Mac to count the items") { try openCount(session) == items.count }
        return session
    }

    private func openCount(_ session: String) throws -> Int? {
        try mac.session(session)?["openCount"] as? Int ?? 0
    }

    private func prompted(_ text: String) throws -> Bool {
        try agentCalls("session/prompt").contains { call in
            ((call["params"] as? [String: Any])?["prompt"] as? [[String: Any]] ?? []).contains { ($0["text"] as? String ?? "").contains(text) }
        }
    }

    private func answerAll(_ app: XCUIApplication) -> XCUIElement {
        let all = app.buttons["Answer all"]
        XCTAssertTrue(all.waitForExistence(timeout: 15))
        all.tap()
        let step = app.otherElements["open-step"]
        XCTAssertTrue(step.waitForExistence(timeout: 10))
        return step
    }

    func testAnsweringOneAndLeavingAnotherForLaterDropsTheMacsCount() throws {
        let session = try sessionWithItems("E2E open later", [decision("Store", "Which store?", ["Postgres", "Redis"]), decision("Cache", "Which cache?", ["Memcached", "None"])])
        let app = try launch(open: "agents")
        let step = answerAll(app)
        XCTAssertTrue(app.staticTexts["1 of 2"].waitForExistence(timeout: 5))
        option("Postgres", in: step).tap()
        XCTAssertTrue(app.staticTexts["1 answered"].waitForExistence(timeout: 5))
        sleep(1)
        app.buttons["Next"].tap()
        XCTAssertTrue(app.staticTexts["2 of 2"].waitForExistence(timeout: 5))
        sleep(1)
        app.buttons["Later"].tap()
        eventually("the Mac's count to drop to the item left for later") { try openCount(session) == 1 }
        eventually("the answer to reach the agent") { try prompted("Postgres") }
        let queue = try launch(open: "agents")
        XCTAssertTrue(queue.buttons["Answer all"].waitForExistence(timeout: 15), "the item left for later stays in the queue")
        XCTAssertTrue(text(queue, containing: "E2E open later").exists)
    }

    func testBacklogAndNoToAllClearTheCardOnBothSides() throws {
        let session = try sessionWithItems("E2E open backlog", [decision("Docs", "Write the migration guide?", ["Now", "Later"]), decision("Tests", "Add a soak test?", ["Yes", "No"])])
        let app = try launch(open: "agents")
        _ = answerAll(app)
        XCTAssertTrue(app.staticTexts["1 of 2"].waitForExistence(timeout: 5))
        sleep(1)
        app.buttons["Backlog"].tap()
        eventually("the task to land in the Mac's backlog") {
            String(decoding: try JSONSerialization.data(withJSONObject: try mac.run("backlog.list")), as: UTF8.self).contains("Write the migration guide")
        }
        let launched = try launch(open: "agents/\(session)")
        let card = launched.otherElements["open-card"]
        XCTAssertTrue(card.waitForExistence(timeout: 15))
        let no = card.buttons["No to All"]
        XCTAssertTrue(wait(for: no, "isEnabled == true"))
        no.tap()
        let confirm = launched.sheets.buttons["No to All"].firstMatch
        XCTAssertTrue(confirm.waitForExistence(timeout: 5))
        confirm.tap()
        XCTAssertTrue(wait(for: card, "exists == false"))
        eventually("the Mac to have nothing open") { try openCount(session) == 0 }
    }

    func testTheCardsOtherFieldAndThePassMenuRespond() throws {
        let session = try sessionWithItems("E2E open other", [decision("DB", "Which database?", ["Postgres", "MySQL"]), decision("Ship", "Ship today?", ["Yes", "No"])])
        var app = try launch(open: "agents/\(session)")
        let card = app.otherElements["open-card"]
        let other = option("Other…", in: card)
        XCTAssertTrue(other.waitForExistence(timeout: 15))
        XCTAssertTrue(wait(for: other, "isHittable == true"))
        other.tap()
        let field = card.textFields["Your answer"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        XCTAssertTrue(wait(for: field, "hasKeyboardFocus == true"))
        field.typeText("Use SQLite")
        let send = card.buttons["Send answers"]
        XCTAssertTrue(wait(for: send, "isEnabled == true"))
        send.tap()
        eventually("the typed answer to reach the agent") { try prompted("Use SQLite") }
        eventually("the Mac to count one item left") { try openCount(session) == 1 }
        app = try launch(open: "agents")
        _ = answerAll(app)
        let more = app.navigationBars.buttons["More"]
        XCTAssertTrue(more.waitForExistence(timeout: 5))
        more.tap()
        XCTAssertTrue(app.buttons["Send Now"].waitForExistence(timeout: 5))
        let no = app.buttons["No to This Item"]
        XCTAssertTrue(no.exists)
        no.tap()
        eventually("the Mac to have nothing open") { try openCount(session) == 0 }
    }

    func testAnOfferOfASessionClosedMeanwhileLeavesThePassForTheBacklog() throws {
        let session = try sessionWithItems("E2E open closed", [offer("Write the release notes")])
        let app = try launch(open: "agents")
        let step = answerAll(app)
        let take = option("Do this", in: step)
        XCTAssertTrue(take.waitForExistence(timeout: 10))
        _ = try mac.run("agents.close", ["session": session])
        eventually("the offer to move to the Mac's backlog") {
            String(decoding: try JSONSerialization.data(withJSONObject: try mac.run("backlog.list")), as: UTF8.self).contains("Write the release notes")
        }
        XCTAssertTrue(wait(for: take, "exists == false"), "the filed offer stays in the phone's pass")
        XCTAssertEqual(try openCount(session), 0)
    }
}
