import XCTest

import ClubmateKit

final class NotificationInboxTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_000_000.25)

    private func note(_ id: String, _ workspace: String = "w", kind: NotificationKind = .info, ago: TimeInterval = 0) -> HubNotification {
        HubNotification(id: id, workspace: workspace, message: id, kind: kind, date: now.addingTimeInterval(-ago))
    }

    func testOrdersNewestFirstAndTracksHandling() {
        var inbox = NotificationInbox()
        inbox.add(note("old"), at: now.addingTimeInterval(-60), handled: false, now: now)
        inbox.add(note("first"), at: now, handled: false, now: now)
        inbox.add(note("second"), at: now, handled: false, now: now)
        inbox.add(note("mid", "v"), at: now.addingTimeInterval(-30), handled: true, now: now)
        XCTAssertFalse(inbox.add(note("first"), at: now, handled: false, now: now))
        XCTAssertEqual(inbox.entries.map(\.id), ["second", "first", "mid", "old"])
        XCTAssertEqual(inbox.unhandled, ["second", "first", "old"])

        inbox.handle("old")
        XCTAssertEqual(inbox.unseen("w"), 2)
        inbox.handleAll()
        XCTAssertEqual(inbox.unhandled, [])
        XCTAssertEqual(inbox.unseenTotal, 0)
    }

    func testKeepsAtMost200AndSevenDays() {
        var inbox = NotificationInbox()
        inbox.add(note("stale"), at: now.addingTimeInterval(-NotificationInbox.maxAge), handled: false, now: now)
        for index in 0..<205 {
            inbox.add(note("n\(index)"), at: now.addingTimeInterval(Double(index - 205)), handled: false, now: now)
        }
        XCTAssertEqual(inbox.entries.count, 200)
        XCTAssertEqual(inbox.entries.first?.id, "n204")
        XCTAssertEqual(inbox.entries.last?.id, "n5")
    }

    func testPinsWaitingPermissionsCountedOnceWithTheirEntry() {
        var hub = NotificationHub()
        let state = AgentSessionState(workspace: "w", session: "agt_1", agent: "claude", title: "Fix", status: .waiting, requests: ["p1"])
        hub.waiting.update(workspace: "w", session: nil, sessions: [state])
        XCTAssertTrue(hub.receive(HubNotification(AgentNotifier.requests([state])[0]), focus: HubFocus(appActive: false, workspace: nil), now: now))
        XCTAssertTrue(hub.receive(note("ask", kind: .needsInput), focus: HubFocus(appActive: false, workspace: nil), now: now))
        XCTAssertEqual(hub.pinned.map(\.id), ["agt_1/p1", "ask"])
        XCTAssertEqual(hub.bellCount, 2)

        hub.inbox.handle("agt_1/p1")
        XCTAssertEqual(hub.bellCount, 2)
        var answered = state
        answered.requests = []
        hub.waiting.update(workspace: "w", session: "agt_1", sessions: [answered])
        XCTAssertEqual(hub.pinned.map(\.id), ["ask"])
        XCTAssertEqual(hub.bellCount, 1)
    }

    func testWaitingDropsDeletedSessionsAndClosedWorkspaces() {
        var waiting = AgentWaiting()
        let sessions = [
            AgentSessionState(workspace: "w", session: "agt_1", agent: "claude", title: "", status: .waiting, requests: ["p1"]),
            AgentSessionState(workspace: "w", session: "agt_2", agent: "claude", title: "", status: .waiting, requests: ["p2"]),
        ]
        waiting.update(workspace: "w", session: nil, sessions: sessions)
        waiting.update(workspace: "v", session: nil, sessions: [AgentSessionState(workspace: "v", session: "agt_3", agent: "", title: "", status: .waiting, requests: ["p3"])])
        XCTAssertTrue(waiting.has(workspace: "w", session: "agt_2"))
        waiting.remove(workspace: "w", session: "agt_2")
        waiting.keep(["w"])
        XCTAssertEqual(waiting.alerts.map(\.id), ["agt_1/p1"])
    }

    func testRestoresAfterRestartAndReplaysMissedQuietly() throws {
        let url = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "notifications.json")
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        var hub = NotificationHub()
        let away = HubFocus(appActive: false, workspace: nil)
        XCTAssertTrue(hub.receive(note("live"), focus: away, now: now))
        hub.inbox.handle("live")
        hub.inbox.record(seq: 7)
        try hub.inbox.save(to: url)

        var restored = NotificationHub(inbox: NotificationInbox.load(from: url))
        XCTAssertEqual(restored.inbox, hub.inbox)
        XCTAssertEqual(restored.inbox.lastSeq, 7)
        XCTAssertFalse(restored.receive(note("live"), focus: away, now: now))
        XCTAssertFalse(restored.receive(note("missed", ago: 3600), focus: away, now: now))
        XCTAssertEqual(restored.inbox.entries.map(\.id), ["live", "missed"])
        XCTAssertEqual(restored.bellCount, 1)
    }

    func testWriterKeepsTheNewestGeneration() {
        let url = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "notifications.json")
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        var final = NotificationInbox()
        final.record(seq: 2)
        var stale = NotificationInbox()
        stale.record(seq: 1)
        let writer = InboxWriter(url: url)
        writer.save(final, generation: 2)
        writer.save(stale, generation: 1)
        XCTAssertEqual(NotificationInbox.load(from: url).lastSeq, 2)
    }

    func testResumesFromLastSeqUnlessTheBusLostIt() {
        let found: JSONValue = .array([.object(["seq": .number(7)])])
        XCTAssertEqual(NotificationInbox.resumePoint(lastSeq: 7, read: .success(found)), 7)
        XCTAssertEqual(NotificationInbox.resumePoint(lastSeq: 7, read: .failure(ClubmateError.timeout)), 7)
        XCTAssertEqual(NotificationInbox.resumePoint(lastSeq: 7, read: .success(.array([]))), .max)
    }

    func testLoadSkipsBadEntries() throws {
        var inbox = NotificationInbox()
        inbox.add(note("good"), at: now, handled: false, now: now)
        var json = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(inbox)) as? [String: Any])
        var entries = try XCTUnwrap(json["entries"] as? [[String: Any]])
        var unknown = entries[0]
        var unknownNote = try XCTUnwrap(unknown["note"] as? [String: Any])
        unknownNote["kind"] = "urgent"
        unknownNote["id"] = "unknown"
        unknown["note"] = unknownNote
        entries += [unknown, ["broken": true]]
        json["entries"] = entries
        let url = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString + ".json")
        defer { try? FileManager.default.removeItem(at: url) }
        try JSONSerialization.data(withJSONObject: json).write(to: url)
        XCTAssertEqual(NotificationInbox.load(from: url).entries.map(\.id), ["good"])
    }

    func testSystemNotificationsAskOnceThenStayOff() {
        XCTAssertEqual(SystemNotifications.step(.notDetermined, asked: false), .request)
        XCTAssertEqual(SystemNotifications.step(.notDetermined, asked: true), .off)
        XCTAssertEqual(SystemNotifications.step(.denied, asked: false), .off)
        XCTAssertEqual(SystemNotifications.step(.allowed, asked: true), .deliver)
    }
}
