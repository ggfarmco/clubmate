import XCTest

final class RelayPathTests: E2ETestCase {
    override func setUp() async throws {
        try await super.setUp()
        try mac.setRemote(true)
        try mac.setTunnel(true)
        eventually("the Mac's stream to the relay") { try mac.tunnel()["connected"] as? Bool == true }
        try mac.revokeDevices()
        try mac.clearRelayLog()
        addTeardownBlock { [mac] in
            try? mac?.releaseRelayStreams()
            try? mac?.setTunnel(false)
            try? mac?.revokeDevices()
        }
    }

    func testAPhonePairedWithoutHostsSendsAPromptAndGetsTheReplyOverTheRelay() throws {
        let app = try launchOverRelay(open: "agents")
        XCTAssertTrue(online(app, timeout: 20))
        app.navigationBars.buttons["New Session"].tap()
        let composer = app.textViews["composer"]
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        XCTAssertTrue(wait(for: composer, "hasKeyboardFocus == true"))
        composer.typeText("E2E relay ping")
        let send = app.buttons["Send"]
        XCTAssertTrue(wait(for: send, "isEnabled == true"))
        send.tap()
        XCTAssertTrue(anywhere(app, containing: "Relay reply.").waitForExistence(timeout: 30))
        XCTAssertEqual(try mac.devices().first?["path"] as? String, "relay")
        let calls = try mac.relayLog()
        for path in ["/v1/d/open", "/v1/d/call", "/v1/d/stream"] {
            XCTAssertTrue(calls.contains { $0["path"] as? String == path && $0["status"] as? Int == 200 }, path)
        }
    }

    func testAnEventArrivesOverTheRelayStream() throws {
        let session = try mac.newSession("E2E live")
        let app = try launchOverRelay(open: "agents/\(session)")
        XCTAssertTrue(step(app, "Live one").waitForExistence(timeout: 20))
        try mac.signal("live-1")
        XCTAssertTrue(step(app, "Live two").waitForExistence(timeout: 15))
        try mac.signal("live-2")
        XCTAssertEqual(try mac.devices().first?["path"] as? String, "relay")
    }

    func testAPushIsSkippedWhileTheRelayStreamIsOpenAndSentAfterTheRelayDropsIt() throws {
        let app = try launchOverRelay(open: "agents")
        XCTAssertTrue(online(app, timeout: 20))
        allowNotifications(timeout: 10)
        try waitForPushToken()
        XCTAssertEqual(try pushOutcomes("E2E relay open"), ["skipped, stream open"])
        let before = try alerts()
        try mac.dropRelayStreams(hold: true)
        eventually("the Mac to see the stream gone") { try mac.devices().first?["connected"] as? Bool == false }
        XCTAssertEqual(try pushOutcomes("E2E relay dropped"), ["200"])
        eventually("the push at the relay") { try alerts() == before + 1 }
        try mac.releaseRelayStreams()
        eventually("the phone back on the relay stream", timeout: 45) {
            let device = try mac.devices().first
            return device?["connected"] as? Bool == true && device?["path"] as? String == "relay"
        }
    }

    func testSwitchingTheMacToTailscaleLeavesOneStream() throws {
        let app = try launch(open: "mac")
        let picker = app.buttons["mac.path"]
        XCTAssertTrue(picker.waitForExistence(timeout: 20))
        for (choice, path, streams) in [("Relay", "relay", 1), ("Tailscale", "tailnet", 0), ("Auto", "tailnet", 0)] {
            picker.tap()
            app.buttons[choice].tap()
            XCTAssertTrue(wait(for: app.staticTexts["mac.path.value"], "label == '\(choice)'"))
            eventually("one stream on \(path)", timeout: 30) {
                let devices = try mac.devices()
                let open = try mac.relayStreams()
                return devices.count == 1 && devices.first?["path"] as? String == path && open == streams
            }
        }
    }

    private static func withoutHosts(_ link: String) -> String {
        guard var parts = URLComponents(string: link) else { return link }
        parts.queryItems = parts.queryItems?.filter { $0.name != "h" }
        return parts.string ?? link
    }

    private func launchOverRelay(open target: String) throws -> XCUIApplication {
        try launch(open: target, pairLink: Self.withoutHosts)
    }

    private func anywhere(_ app: XCUIApplication, containing label: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", label)).firstMatch
    }

    private func pushOutcomes(_ message: String) throws -> [String] {
        let result = try mac.run("remote.push", ["message": message]) as? [String: Any]
        return (result?["devices"] as? [[String: Any]] ?? []).compactMap { $0["outcome"] as? String }
    }

    private func alerts() throws -> Int {
        try mac.pushes().filter { $0["type"] as? String == "alert" }.count
    }
}
