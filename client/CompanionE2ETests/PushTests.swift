import XCTest

final class PushTests: E2ETestCase {
    struct Notice {
        let identifier: String
        let push: Bool
        let subtitle: String
        let session: String?
        let kind: String?
    }

    func testAQuestionAnsweredOnTheMacEndsAsOneFinishedEntry() throws {
        let app = try launch(open: "agents")
        XCTAssertTrue(text(app, containing: "online").waitForExistence(timeout: 15))
        XCUIDevice.shared.press(.home)
        let session = try mac.newSession("E2E push")
        eventually("a question alert") { try delivered(session).map(\.kind) == ["question"] }
        try mac.answerOnTheMac(session, "Yes")
        eventually("the alert to become Finished", timeout: 20) { try delivered(session).map(\.kind) == ["done"] }
        XCTAssertEqual(try delivered(session).map(\.identifier), [session])
    }

    func testAChildsReportThenTheParentsQuestionGiveOneQuestionAlert() throws {
        let parent = try mac.newSession("E2E parent")
        eventually("the parent's first turn to end") { try status(parent) == "done" }
        let app = try launch(open: "agents")
        XCTAssertTrue(text(app, containing: "online").waitForExistence(timeout: 15))
        XCUIDevice.shared.press(.home)
        let child = (try mac.run("agents.new", ["prompt": "E2E kid reports"], caller: parent, agent: true) as? [String: Any])?["session"] as? String
        XCTAssertNotNil(child)
        eventually("the parent to ask", timeout: 20) { try status(parent) == "waiting" }
        eventually("one question alert for the parent", timeout: 10) { try delivered(parent).map(\.kind) == ["question"] }
        RunLoop.current.run(until: .now.addingTimeInterval(6))
        XCTAssertEqual(try delivered(parent).map(\.kind), ["question"])
        XCTAssertEqual(try delivered(parent).map(\.identifier), [parent])
    }

    func testPushesOfOneSessionShareItsCollapseIDAndTheLocalAlertReplacesThem() throws {
        var app = try launch(open: "agents")
        XCTAssertTrue(text(app, containing: "online").waitForExistence(timeout: 15))
        try waitForPushToken()
        app.terminate()
        eventually("the Mac to see the phone gone") {
            !(try mac.run("remote.devices") as? [[String: Any]] ?? []).contains { $0["connected"] as? Bool == true }
        }
        let session = try mac.newSession("E2E push")
        eventually("the question to go out through the relay", timeout: 20) { try alerts(session).count == 1 }
        XCTAssertEqual(try alerts(session).first?["collapseID"] as? String, session)
        XCTAssertEqual(try alerts(session).first?["delivered"] as? String, "simctl")
        app = try launch(open: "agents")
        XCTAssertTrue(text(app, containing: "online").waitForExistence(timeout: 15))
        eventually("the pushed question on the phone") { try delivered(session).contains { $0.push && $0.kind == "question" } }
        XCUIDevice.shared.press(.home)
        try mac.answerOnTheMac(session, "Yes")
        eventually("one Finished entry for the session", timeout: 20) { try delivered(session).map(\.kind) == ["done"] }
    }

    private func alerts(_ session: String) throws -> [[String: Any]] {
        try mac.pushes().filter { push in
            guard push["type"] as? String == "alert", let payload = push["payload"] as? [String: Any] else { return false }
            return payload["session"] as? String == session
        }
    }

    private func delivered(_ session: String) throws -> [Notice] {
        guard let url = deliveredDump, let data = try? Data(contentsOf: url),
              let dump = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let rows = dump["notifications"] as? [[String: Any]] else { return [] }
        return rows.map { row in
            let info = row["userInfo"] as? [String: String] ?? [:]
            return Notice(identifier: row["identifier"] as? String ?? "", push: row["push"] as? Bool ?? false, subtitle: row["subtitle"] as? String ?? "", session: info["session"], kind: info["kind"])
        }.filter { $0.session == session || $0.identifier == session }
    }
}
