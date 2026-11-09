import XCTest

final class TaskTests: E2ETestCase {
    private func owner(_ title: String) throws -> String {
        let session = try mac.newSession(title)
        eventually("the owner's turn to end") { try status(session) == "done" }
        return session
    }

    private func askToConnect(_ owner: String, _ path: String) throws {
        _ = try mac.run("agents.connect-repo", ["repos": [["path": path, "reason": "the shared code lives there", "landsBy": "merge", "source": "its CLAUDE.md"]]], caller: owner, agent: true)
        eventually("the connect request on the Mac") { !(try requests(owner)).isEmpty }
    }

    private func requests(_ session: String) throws -> [String] {
        try mac.session(session)?["requests"] as? [String] ?? []
    }

    private func connectButton(_ app: XCUIApplication) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Connect'")).firstMatch
    }

    func testConnectingFromThePhoneAddsTheRepoOnTheMac() throws {
        let owner = try owner("E2E task connect")
        let lib = try mac.repo("connect-lib")
        try askToConnect(owner, lib)
        let app = try launch(open: "agents/\(owner)")
        let connect = connectButton(app)
        XCTAssertTrue(connect.waitForExistence(timeout: 15))
        XCTAssertTrue(wait(for: connect, "isEnabled == true"))
        screenshot(app, "connect-card")
        connect.tap()
        eventually("the repo to become a folder of the workspace") { try mac.folders().contains(lib) }
        eventually("the request to be answered") { try requests(owner).isEmpty }
        XCTAssertTrue(wait(for: connect, "exists == false"))
    }

    func testMergingALandRequestFromThePhonePromptsTheRepoChild() throws {
        let owner = try owner("E2E task land")
        let lib = try mac.repo("land-lib")
        try mac.addFolder(lib)
        _ = try mac.run("agents.task", ["slug": "e2e-land", "repos": [["path": lib, "order": 1, "landsBy": "merge", "source": "its CLAUDE.md"]]], caller: owner, agent: true)
        let kid = try XCTUnwrap(try mac.run("agents.new", ["repo": lib, "slug": "e2e-land", "prompt": "E2E kid work"], caller: owner, agent: true) as? [String: Any])
        let session = try XCTUnwrap(kid["session"] as? String)
        let worktree = try XCTUnwrap(kid["worktree"] as? String)
        eventually("the repo child's turn to end", timeout: 30) { try status(session) == "done" }
        try mac.commit(in: worktree, "land.txt")
        _ = try mac.run("agents.task-report", ["state": "ready"], caller: session, agent: true)
        eventually("the Land request on the Mac") { !(try requests(owner)).isEmpty }
        let prompts = try agentCalls("session/prompt").count
        let app = try launch(open: "agents/\(owner)")
        let review = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Review and merge'")).firstMatch
        XCTAssertTrue(review.waitForExistence(timeout: 15))
        let repos = (try mac.session(owner)?["task"] as? [String: Any])?["repos"] as? [[String: Any]] ?? []
        XCTAssertEqual(repos.first { $0["path"] as? String == lib }?["state"] as? String, "ready")
        XCTAssertTrue(text(app, containing: "ready to land").exists, "the Repos card disagrees with the Mac's ready")
        screenshot(app, "land-card")
        review.tap()
        let merge = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Merge'")).firstMatch
        XCTAssertTrue(merge.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: merge, "isEnabled == true"))
        screenshot(app, "land-review")
        merge.tap()
        eventually("the Land request to be answered") { try requests(owner).isEmpty }
        eventually("the repo child to be prompted to land") { try agentCalls("session/prompt").count > prompts }
    }

    func testAConnectQueuedWhileTheMacIsAwayGoesThroughOnce() throws {
        let owner = try owner("E2E task offline")
        let lib = try mac.repo("offline-lib")
        try askToConnect(owner, lib)
        let app = try launch(open: "agents/\(owner)")
        let connect = connectButton(app)
        XCTAssertTrue(connect.waitForExistence(timeout: 15))
        XCTAssertTrue(wait(for: connect, "isEnabled == true"))
        try mac.setRemote(false)
        defer { try? mac.setRemote(true) }
        XCTAssertTrue(text(app, containing: "offline").waitForExistence(timeout: 30) || text(app, containing: "Offline").exists)
        XCTAssertTrue(wait(for: connect, "isEnabled == true"))
        connect.tap()
        let queued = text(app, containing: "queued")
        XCTAssertTrue(queued.waitForExistence(timeout: 10) || text(app, containing: "Queued").exists)
        screenshot(app, "queued-connect")
        XCTAssertFalse(try mac.folders().contains(lib))
        try mac.setRemote(true)
        eventually("the queued connect to go through", timeout: 60) { try mac.folders().contains(lib) }
        eventually("the request to be answered") { try requests(owner).isEmpty }
        XCTAssertTrue(wait(for: queued, "exists == false", timeout: 20))
        XCTAssertEqual(try mac.folders().filter { $0 == lib }.count, 1)
    }
}
