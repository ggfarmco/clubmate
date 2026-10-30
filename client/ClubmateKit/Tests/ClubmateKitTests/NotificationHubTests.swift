import XCTest

import ClubmateKit

final class NotificationHubTests: XCTestCase {
    private let terminal = SDUINode(type: "surface", props: ["sessionId": .string("ses_1")])
    private let agents = SDUINode(type: "tree", id: "sessions", props: ["selected": .string("agt_1")])

    func testPostsUnlessTargetSessionIsVisibleInFront() {
        let note = HubNotification(id: "n", workspace: "w", session: "ses_1", message: "hi")
        let cases: [(HubFocus, Bool)] = [
            (HubFocus(appActive: true, workspace: "w", main: terminal), false),
            (HubFocus(appActive: false, workspace: "w", main: terminal), true),
            (HubFocus(appActive: true, workspace: "other", main: terminal), true),
            (HubFocus(appActive: true, workspace: "w", main: SDUINode(type: "surface", props: ["sessionId": .string("ses_2")])), true),
            (HubFocus(appActive: true, workspace: "w", main: SDUINode(type: "split", children: [SDUINode(type: "surface", props: ["sessionId": .string("ses_2")]), terminal])), false),
        ]
        for (index, (focus, posts)) in cases.enumerated() {
            var hub = NotificationHub()
            XCTAssertEqual(hub.receive(note, focus: focus), posts, "case \(index)")
        }

        var hub = NotificationHub()
        let agent = HubNotification(id: "a", workspace: "w", session: "agt_1", message: "hi", kind: .permission)
        XCTAssertFalse(hub.receive(agent, focus: HubFocus(appActive: true, workspace: "w", sidebar: agents)))
        XCTAssertFalse(hub.receive(HubNotification(id: "x", workspace: "w", message: "hi"), focus: HubFocus(appActive: true, workspace: "w")))
    }

    func testCountsUnreadUntilWorkspaceIsSeen() {
        var hub = NotificationHub()
        let away = HubFocus(appActive: false, workspace: "w")
        XCTAssertTrue(hub.receive(HubNotification(id: "1", workspace: "w", message: "a"), focus: away))
        XCTAssertTrue(hub.receive(HubNotification(id: "2", workspace: "w", message: "b"), focus: away))
        XCTAssertTrue(hub.receive(HubNotification(id: "3", workspace: "v", message: "c"), focus: away))
        XCTAssertTrue(hub.receive(HubNotification(id: "4", workspace: "v", session: "ses_9", message: "d"), focus: HubFocus(appActive: true, workspace: "v")))
        XCTAssertEqual([hub.unread("w"), hub.unread("v")], [2, 1])
        XCTAssertEqual(hub.total, 3)

        hub.seen("w")
        XCTAssertEqual([hub.unread("w"), hub.unread("v")], [0, 1])
        XCTAssertEqual(hub.total, 1)
    }

    func testDropsAgentSessionNeedsInputButKeepsDone() {
        var hub = NotificationHub()
        let away = HubFocus(appActive: false, workspace: nil)
        for kind in [NotificationKind.needsInput, .permission] {
            XCTAssertFalse(hub.receive(HubNotification(id: kind.rawValue, workspace: "w", session: "agt_1", message: "?", kind: kind), focus: away))
        }
        XCTAssertTrue(hub.receive(HubNotification(id: "d", workspace: "w", session: "agt_1", message: "Finished", kind: .done), focus: away))
        XCTAssertTrue(hub.receive(HubNotification(id: "q", workspace: "w", session: "agt_1", message: "Which?", kind: .needsInput, viaAgent: true), focus: away))
        XCTAssertEqual(hub.unread("w"), 2)
    }

    func testAgentPermissionPostsOncePerRequestWhenHidden() {
        var notifier = AgentNotifier()
        let waiting = AgentSessionState(workspace: "w", session: "agt_1", agent: "claude", title: "Fix", status: "waiting", requests: ["permission-1"])
        let running = AgentSessionState(workspace: "w", session: "agt_2", agent: "claude", title: "Run", status: "running", requests: ["permission-9"])

        XCTAssertEqual(notifier.pending([running], shown: { _ in false }), [])
        XCTAssertEqual(notifier.pending([waiting], shown: { _ in true }), [])
        let alerts = notifier.pending([waiting], shown: { _ in false })
        XCTAssertEqual(alerts.map(\.request), ["permission-1"])
        var hub = NotificationHub()
        XCTAssertTrue(hub.receive(HubNotification(alerts[0]), focus: HubFocus(appActive: false, workspace: nil)))
        XCTAssertEqual(notifier.pending([waiting], shown: { _ in false }).map(\.request), ["permission-1"])
        notifier.markPosted(alerts[0])
        XCTAssertEqual(notifier.pending([waiting], shown: { _ in false }), [])

        var next = waiting
        next.requests = ["permission-1", "permission-2"]
        XCTAssertEqual(notifier.pending([next], shown: { _ in false }).map(\.request), ["permission-2"])
    }
}
