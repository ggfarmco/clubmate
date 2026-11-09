import XCTest

final class HardwareKeyboardTests: E2ETestCase {
    override func setUp() async throws {
        try await super.setUp()
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "a hardware keyboard check: run on the cm-e2e-ipad simulator")
    }

    func testOptionKeysAnswerASessionsPermissionRequests() throws {
        let session = try mac.newSession("E2E permission")
        eventually("the first permission request") { try status(session) == "waiting" }
        let app = try launch(open: "agents/\(session)")
        XCTAssertTrue(text(app, containing: "Run make first").waitForExistence(timeout: 15))
        let reject = app.buttons["Reject"]
        XCTAssertTrue(reject.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: reject, "isEnabled == true"))
        sleep(1)
        app.typeKey("1", modifierFlags: .option)
        eventually("the agent to be allowed once") { try optionsAnswered() == ["allow"] }
        XCTAssertTrue(text(app, containing: "Run make second").waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: app.buttons["Reject"], "isEnabled == true"))
        sleep(1)
        app.typeKey("3", modifierFlags: .option)
        eventually("the agent to be rejected") { try optionsAnswered() == ["allow", "reject"] }
        eventually("the turn to end") { try status(session) == "done" }
    }

    func testOptionKeysAnswerARealRunsStep() throws {
        for (key, outcome) in [("1", "done"), ("3", "error")] {
            let run = try XCTUnwrap((try mac.run("graphs.run", ["graph": "e2e", "input": "key \(key)"]) as? [String: Any])?["id"] as? String)
            eventually("the step to wait for approval", timeout: 30) { try stepStatus(run) == "waiting" }
            let app = try launch(open: "agents/\(run)")
            let reject = app.buttons["Reject"]
            XCTAssertTrue(reject.waitForExistence(timeout: 15))
            XCTAssertTrue(wait(for: reject, "isEnabled == true"))
            sleep(1)
            app.typeKey(key, modifierFlags: .option)
            eventually("⌥\(key) to answer the step", timeout: 20) { try stepStatus(run)?.hasPrefix(outcome) == true }
        }
    }

    private func optionsAnswered() throws -> [String] {
        try agentCalls("permission_response").compactMap { ((($0["params"] as? [String: Any])?["outcome"] as? [String: Any])?["optionId"] as? String) }
    }

    private func stepStatus(_ run: String) throws -> String? {
        let nodes = (try mac.run("graphs.show", ["id": run]) as? [String: Any])?["nodes"] as? [[String: Any]] ?? []
        return nodes.first { $0["id"] as? String == "tick" }?["status"] as? String
    }
}
