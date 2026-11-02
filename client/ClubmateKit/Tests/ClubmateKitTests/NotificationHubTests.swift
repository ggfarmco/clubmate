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
        XCTAssertFalse(hub.receive(HubNotification(id: "p", workspace: "w", session: "agt_2", message: "hi"), focus: HubFocus(appActive: true, workspace: "w", panel: SDUINode(type: "composer", id: "agt_2/composer/0"))))
        XCTAssertFalse(hub.receive(HubNotification(id: "x", workspace: "w", message: "hi"), focus: HubFocus(appActive: true, workspace: "w")))
    }

    func testHiddenSessionInFrontWorkspaceStaysUnhandledAndPinned() {
        var hub = NotificationHub()
        let front = HubFocus(appActive: true, workspace: "w", main: terminal)
        XCTAssertTrue(hub.receive(HubNotification(id: "ask", workspace: "w", session: "ses_2", message: "?", kind: .needsInput), focus: front))
        XCTAssertTrue(hub.receive(HubNotification(id: "away", workspace: "v", message: "hi"), focus: front))
        XCTAssertEqual([hub.unseen("w"), hub.unseen("v")], [1, 1])
        XCTAssertEqual(hub.pinned.map(\.id), ["ask"])
        XCTAssertEqual(hub.bellCount, 2)

        hub.focused(front)
        XCTAssertEqual([hub.unseen("w"), hub.unseen("v")], [0, 1])
        XCTAssertEqual(hub.pinned.map(\.id), ["ask"])
        XCTAssertEqual(hub.bellCount, 2)

        hub.focused(HubFocus(appActive: true, workspace: "w", main: SDUINode(type: "surface", props: ["sessionId": .string("ses_2")])))
        XCTAssertEqual(hub.pinned, [])
        XCTAssertEqual(hub.bellCount, 1)
        XCTAssertEqual(hub.unseenTotal, 1)
    }

    func testDropsAgentSessionRequestsButKeepsTurnEnds() {
        var hub = NotificationHub()
        let away = HubFocus(appActive: false, workspace: nil)
        for kind in [NotificationKind.permission, .question] {
            XCTAssertFalse(hub.receive(HubNotification(id: kind.rawValue, workspace: "w", session: "agt_1", message: "?", kind: kind), focus: away))
        }
        for kind in [NotificationKind.needsInput, .done] {
            XCTAssertTrue(hub.receive(HubNotification(id: kind.rawValue, workspace: "w", session: "agt_1", message: "m", kind: kind), focus: away))
        }
        XCTAssertEqual(hub.unseen("w"), 2)
    }

    func testSessionPolicyLimitsBannersAndKeepsSkippedAsRead() {
        let listed: JSONValue = .array([
            .object(["id": .string("agt_1"), "workspace": .string("w"), "notifyEffective": .string("needs-input")]),
            .object(["id": .string("agt_2"), "workspace": .string("w"), "notifyEffective": .string("muted")]),
            .object(["id": .string("agt_3"), "workspace": .string("w")]),
        ])
        XCTAssertEqual(AgentSession.list(listed).map(\.notifyEffective), [.needsInput, .muted, .all])

        let away = HubFocus(appActive: false, workspace: nil)
        let note = { (id: String, kind: NotificationKind) in HubNotification(id: id, workspace: "w", session: "agt_1", message: "m", kind: kind, viaAgent: true) }
        var hub = NotificationHub()
        XCTAssertTrue(hub.receive(note("all-done", .done), focus: away, policy: .all))
        XCTAssertFalse(hub.receive(note("needs-done", .done), focus: away, policy: .needsInput))
        XCTAssertTrue(hub.receive(note("needs-ask", .needsInput), focus: away, policy: .needsInput))
        XCTAssertFalse(hub.receive(note("muted-ask", .needsInput), focus: away, policy: .muted))
        XCTAssertEqual(hub.inbox.entries.count, 4)
        XCTAssertEqual(hub.inbox.unhandled, ["all-done", "needs-ask"])
    }

    func testAgentPermissionPostsOncePerRequestWhenHidden() {
        var notifier = AgentNotifier()
        let waiting = AgentSession(id: "agt_1", workspace: "w", agent: "claude", title: "Fix", status: .waiting, pending: ["permission-1"])
        let running = AgentSession(id: "agt_2", workspace: "w", agent: "claude", title: "Run", status: .running, pending: ["permission-9"])

        XCTAssertEqual(notifier.pending([running], shown: { _ in false }), [])
        XCTAssertEqual(notifier.pending([waiting], shown: { _ in true }), [])
        let alerts = notifier.pending([waiting], shown: { _ in false })
        XCTAssertEqual(alerts.map(\.request), ["permission-1"])
        var hub = NotificationHub()
        XCTAssertTrue(hub.receive(HubNotification(alerts[0]), focus: HubFocus(appActive: false, workspace: nil)))
        XCTAssertEqual(notifier.pending([waiting], shown: { _ in false }).map(\.request), ["permission-1"])
        notifier.markPosted(alerts[0])
        XCTAssertEqual(notifier.pending([waiting], shown: { _ in false }), [])

        let next = AgentSession(id: "agt_1", workspace: "w", agent: "claude", title: "Fix", status: .waiting, pending: ["permission-1", "permission-2"])
        XCTAssertEqual(notifier.pending([next], shown: { _ in false }).map(\.request), ["permission-2"])
    }

    func testClaudeHooksSkipAgentSessions() throws {
        let json = try JSONSerialization.jsonObject(with: Data(ClaudeHooks.snippet(command: "exit 3 #").utf8)) as? [String: Any]
        let hooks = try XCTUnwrap(json?["hooks"] as? [String: [[String: Any]]])
        let commands = hooks.values.flatMap { $0.flatMap { ($0["hooks"] as? [[String: String]] ?? []).compactMap { $0["command"] } } }
        XCTAssertEqual(commands.count, 2)
        for command in commands {
            XCTAssertEqual(try run(command, env: ["CLUBMATE_AGENT_SESSION": "agt_1"]), 0)
            XCTAssertEqual(try run(command, env: [:]), 3)
        }
    }

    private func run(_ command: String, env: [String: String]) throws -> Int32 {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/sh")
        process.arguments = ["-c", command]
        process.environment = env.merging(["PATH": "/usr/bin:/bin"]) { a, _ in a }
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        try process.run()
        process.waitUntilExit()
        return process.terminationStatus
    }
}
