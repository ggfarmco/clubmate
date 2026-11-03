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
        let state = AgentSession(id: "agt_1", workspace: "w", agent: "claude", title: "Fix", status: .waiting, pending: ["p1"])
        hub.waiting.update(workspace: "w", session: nil, sessions: [state])
        XCTAssertTrue(hub.receive(HubNotification(AgentNotifier.requests([state])[0]), focus: HubFocus(appActive: false, workspace: nil), now: now))
        XCTAssertTrue(hub.receive(note("ask", kind: .needsInput), focus: HubFocus(appActive: false, workspace: nil), now: now))
        XCTAssertEqual(hub.pinned.map(\.id), ["agt_1/p1", "ask"])
        XCTAssertEqual(hub.bellCount, 2)

        hub.inbox.handle("agt_1/p1")
        XCTAssertEqual(hub.bellCount, 2)
        let answered = AgentSession(id: "agt_1", workspace: "w", agent: "claude", title: "Fix", status: .waiting)
        hub.waiting.update(workspace: "w", session: "agt_1", sessions: [answered])
        XCTAssertEqual(hub.pinned.map(\.id), ["ask"])
        XCTAssertEqual(hub.bellCount, 1)
    }

    func testGraphRunNeedsYouPinsOnceAndOpensTheRun() throws {
        var hub = NotificationHub()
        let away = HubFocus(appActive: false, workspace: nil)
        let run = AgentSession(id: "gr_1", workspace: "w", title: "Review gate", status: .waiting, pending: ["cap-1"], pendingSummary: PendingSummary(kind: .question, text: "Tests → Coder ran 2 of 2 rounds", at: now), needsYou: true)
        hub.waiting.update(workspace: "w", session: nil, sessions: [run])
        let alert = try XCTUnwrap(AgentNotifier.requests([run]).first)
        XCTAssertEqual([alert.title, alert.body, HubNotification(alert).source(workspaceName: "shop")], ["Question · Tests → Coder ran 2 of 2 rounds", "Review gate", "shop · Graph run"])
        XCTAssertFalse(hub.receive(HubNotification(id: "n1", workspace: "w", session: "gr_1", message: "x", kind: .permission, date: now), focus: away, now: now))
        XCTAssertEqual(hub.pinned.map(\.id), ["gr_1/cap-1"])
        let reveal = try XCTUnwrap(NotificationHub.reveal(workspace: "w", session: "gr_1", owner: AgentNotifier.section))
        XCTAssertEqual(reveal.section, "graphs")
        XCTAssertEqual(reveal.event, GraphRun.selectEvent(workspace: "w", run: "gr_1"))
    }

    func testPinsOldestFirstByWhenItNeededYou() {
        var hub = NotificationHub()
        let away = HubFocus(appActive: false, workspace: nil)
        let state = AgentSession(id: "agt_1", workspace: "w", title: "Fix", status: .waiting, updatedAt: now, pending: ["p1"], needsYouSince: now.addingTimeInterval(-30))
        hub.waiting.update(workspace: "w", session: nil, sessions: [state])
        _ = hub.receive(note("older", kind: .needsInput, ago: 60), focus: away, now: now)
        _ = hub.receive(note("newer", kind: .needsInput, ago: 10), focus: away, now: now)
        XCTAssertEqual(hub.pinned.map(\.id), ["older", "agt_1/p1", "newer"])
    }

    func testMarksAnsweredPermissionEntriesOfItsWorkspaceRead() {
        var hub = NotificationHub()
        let away = HubFocus(appActive: false, workspace: nil)
        let asking = [
            AgentSession(id: "agt_1", workspace: "w", agent: "claude", title: "", status: .waiting, pending: ["p1"]),
            AgentSession(id: "agt_2", workspace: "w", agent: "claude", title: "", status: .waiting, pending: ["p2"]),
            AgentSession(id: "agt_3", workspace: "v", agent: "claude", title: "", status: .waiting, pending: ["p3"]),
        ]
        for alert in AgentNotifier.requests(asking) {
            XCTAssertTrue(hub.receive(HubNotification(alert), focus: away, now: now))
        }
        XCTAssertTrue(hub.receive(HubNotification(id: "bus", workspace: "w", session: "agt_1", message: "?", kind: .permission, viaAgent: true), focus: away, now: now))

        XCTAssertEqual(hub.handleResolved(workspace: "w", session: nil, sessions: [asking[1]]), ["agt_1/p1"])
        XCTAssertEqual(hub.inbox.entries.count, 4)
        XCTAssertEqual(hub.inbox.unhandled, ["agt_2/p2", "agt_3/p3", "bus"])
        XCTAssertEqual(hub.handleResolved(workspace: "w", session: nil, sessions: [asking[1]]), [])
    }

    func testPendingQuestionIsItsOwnKindAndLeavesOnceAnswered() {
        var hub = NotificationHub()
        let asking = AgentSession(id: "agt_1", workspace: "w", agent: "claude", title: "", status: .waiting, pending: ["elicitation-2"])
        let alert = AgentNotifier.requests([asking])[0]
        XCTAssertEqual(alert.title, "Question")
        XCTAssertTrue(hub.receive(HubNotification(alert), focus: HubFocus(appActive: false, workspace: nil), now: now))
        XCTAssertEqual(hub.inbox.entries.map(\.note.kind), [.question])

        XCTAssertEqual(hub.handleResolved(workspace: "w", session: "agt_1", sessions: []), ["agt_1/elicitation-2"])
        XCTAssertEqual(hub.inbox.entries.map(\.note.kind), [.question])
        XCTAssertEqual(hub.inbox.unhandled, [])
    }

    func testAlertNamesWhatTheSessionWantsAndWhenItAsked() {
        let asked = now.addingTimeInterval(-120)
        let session = AgentSession(id: "agt_1", workspace: "w", title: "Fix flaky test", status: .waiting, pending: ["p1", "p2"], pendingSummary: PendingSummary(kind: .permission, text: "go test ./...", tool: "execute", at: asked))
        let alerts = AgentNotifier.requests([session])
        XCTAssertEqual(alerts.map(\.title), ["Permission · Run go test ./...", "Permission"])
        XCTAssertEqual(alerts[0].body, "Fix flaky test")
        XCTAssertEqual(alerts[0].date, asked)
    }

    func testWaitingDropsDeletedSessionsAndClosedWorkspaces() {
        var waiting = AgentWaiting()
        let sessions = [
            AgentSession(id: "agt_1", workspace: "w", agent: "claude", title: "", status: .waiting, pending: ["p1"]),
            AgentSession(id: "agt_2", workspace: "w", agent: "claude", title: "", status: .waiting, pending: ["p2"]),
        ]
        waiting.update(workspace: "w", session: nil, sessions: sessions)
        waiting.update(workspace: "v", session: nil, sessions: [AgentSession(id: "agt_3", workspace: "v", agent: "", title: "", status: .waiting, pending: ["p3"])])
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
