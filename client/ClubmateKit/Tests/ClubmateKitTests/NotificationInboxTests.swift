import XCTest

import ClubmateKit

final class NotificationInboxTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_000_000)

    private func note(_ id: String, _ workspace: String = "w", kind: NotificationKind = .info, ago: TimeInterval = 0) -> HubNotification {
        HubNotification(id: id, workspace: workspace, message: id, kind: kind, date: now.addingTimeInterval(-ago))
    }

    func testOrdersNewestFirstAndKeepsReadState() {
        var inbox = NotificationInbox()
        inbox.add(note("old"), at: now.addingTimeInterval(-60), read: false, now: now)
        inbox.add(note("new"), at: now, read: false, now: now)
        inbox.add(note("mid", "v"), at: now.addingTimeInterval(-30), read: true, now: now)
        XCTAssertFalse(inbox.add(note("new"), at: now, read: false, now: now))
        XCTAssertEqual(inbox.entries.map(\.id), ["new", "mid", "old"])
        XCTAssertEqual(inbox.unreadTotal, 2)

        inbox.markRead("old")
        XCTAssertEqual([inbox.unread("w"), inbox.unread("v")], [1, 0])
        inbox.markAllRead()
        XCTAssertEqual(inbox.unreadTotal, 0)
    }

    func testKeepsAtMost200AndSevenDays() {
        var inbox = NotificationInbox()
        inbox.add(note("stale"), at: now.addingTimeInterval(-NotificationInbox.maxAge), read: false, now: now)
        for index in 0..<205 {
            inbox.add(note("n\(index)"), at: now.addingTimeInterval(Double(index - 205)), read: false, now: now)
        }
        XCTAssertEqual(inbox.entries.count, 200)
        XCTAssertEqual(inbox.entries.first?.id, "n204")
        XCTAssertEqual(inbox.entries.last?.id, "n5")
    }

    func testPinsWaitingPermissionsAndUnreadInput() {
        var inbox = NotificationInbox()
        inbox.add(note("ask", kind: .needsInput), at: now, read: false, now: now)
        inbox.add(note("answered", kind: .needsInput), at: now, read: true, now: now)
        inbox.add(note("done", kind: .done), at: now, read: false, now: now)
        let waiting = AgentNotifier.requests([AgentSessionState(workspace: "w", session: "agt_1", agent: "claude", title: "Fix", status: "waiting", requests: ["p1"])])
        XCTAssertEqual(inbox.pinned(waiting: waiting).map(\.id), ["agt_1/p1", "ask"])
        inbox.markRead(workspace: "w")
        XCTAssertEqual(inbox.pinned(waiting: []).map(\.id), [])
    }

    func testRestoresAfterRestartAndReplaysMissedQuietly() throws {
        let url = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "notifications.json")
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        var hub = NotificationHub()
        let away = HubFocus(appActive: false, workspace: nil)
        XCTAssertTrue(hub.receive(note("live"), focus: away, now: now))
        hub.inbox.markRead("live")
        hub.inbox.advance(to: 7)
        try hub.inbox.save(to: url)

        var restored = NotificationHub(inbox: NotificationInbox.load(from: url))
        XCTAssertEqual(restored.inbox, hub.inbox)
        XCTAssertEqual(restored.inbox.lastSeq, 7)
        XCTAssertFalse(restored.receive(note("live"), focus: away, now: now))
        XCTAssertFalse(restored.receive(note("missed", ago: 3600), focus: away, now: now))
        XCTAssertEqual(restored.inbox.entries.map(\.id), ["live", "missed"])
        XCTAssertEqual(restored.total, 1)
    }

    func testSystemNotificationsAskOnceThenStayOff() {
        XCTAssertEqual(SystemNotifications.step(.notDetermined, asked: false), .request)
        XCTAssertEqual(SystemNotifications.step(.notDetermined, asked: true), .off)
        XCTAssertEqual(SystemNotifications.step(.denied, asked: false), .off)
        XCTAssertEqual(SystemNotifications.step(.allowed, asked: true), .deliver)
    }
}
