import XCTest

final class NightTests: E2ETestCase {
    private func night(_ kind: String) throws -> [String: Any]? {
        try mac.sessions().last { $0["night"] as? String == kind }
    }

    private func nightPushes() throws -> [[String: Any]] {
        try mac.pushes().filter { push in
            let raw = try JSONSerialization.data(withJSONObject: push)
            return push["type"] as? String == "alert" && String(decoding: raw, as: UTF8.self).contains("Night review")
        }
    }

    func testANightsReportReachesTheLockedPhoneAndOpensWithItsUndo() throws {
        let app = try launch(open: "agents")
        XCTAssertTrue(text(app, containing: "online").waitForExistence(timeout: 15))
        _ = try mac.rpc("ui.event", ["workspaceId": "", "slot": "agents.settings", "nodeId": "self-enabled", "action": "self-enabled", "value": true])
        try waitForPushToken()
        let day = try mac.newSession("E2E night day")
        eventually("the day's turn to end") { try status(day) == "done" }
        app.terminate()
        eventually("the Mac to see the phone gone") {
            !(try mac.run("remote.devices") as? [[String: Any]] ?? []).contains { $0["connected"] as? Bool == true }
        }
        eventually("the night to start", timeout: 30) { (try? mac.run("agents.night-run")) != nil }
        var improve: [String: Any] = [:]
        eventually("the Night review session", timeout: 60) {
            improve = try night("improve") ?? [:]
            return improve["worktree"] as? String != nil
        }
        let session = try XCTUnwrap(improve["id"] as? String)
        let worktree = try XCTUnwrap(improve["worktree"] as? String)
        try mac.write(worktree + "/prompt.md", "Keep replies short.\n")
        _ = try mac.run("agents.self-declare", ["summary": "shorter replies", "why": "the user asked twice"], caller: session, agent: true)
        try mac.signal("improve-gate")
        eventually("the night's report to go out as a push", timeout: 60) { !(try nightPushes()).isEmpty }
        XCTAssertEqual(try nightPushes().first?["delivered"] as? String, "simctl")

        let opened = try launch(open: "agents/\(session)")
        XCTAssertTrue(text(opened, containing: "Night review").waitForExistence(timeout: 15))
        let card = opened.otherElements["open-card"]
        XCTAssertTrue(card.waitForExistence(timeout: 15))
        XCTAssertTrue(card.buttons.matching(NSPredicate(format: "label CONTAINS 'Undo: shorter replies'")).firstMatch.waitForExistence(timeout: 5) || text(opened, containing: "Undo: shorter replies").exists)
        screenshot(opened, "night-report")
    }
}
