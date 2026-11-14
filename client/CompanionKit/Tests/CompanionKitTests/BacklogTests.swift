import Foundation
import XCTest

import CompanionKit

@MainActor
final class BacklogTests: XCTestCase {
    func testDecodesTasksFixtureIntoGroups() throws {
        let tasks = try XCTUnwrap(JSONDecoder().decode(JSONValue.self, from: fixture("backlog-tasks"))["tasks"]).decode([BacklogTask].self)
        XCTAssertEqual(BacklogTask.grouped(tasks).map(\.status), [.inProgress, .todo, .idea, .notNow, .done, .other])
        XCTAssertEqual(BacklogTask.grouped(tasks)[1].tasks.map(\.id), [9, 4])
        XCTAssertEqual(tasks[0].claimedBy, "agt_0000000000000001")
        XCTAssertEqual(tasks[0].claimedAt, Date.parseRFC3339("2026-10-31T09:12:00.123Z"))
        XCTAssertNil(tasks[1].claimedBy)
        XCTAssertTrue(tasks[0].descriptionHtml?.contains("<table>") ?? false)
        XCTAssertNil(tasks[1].descriptionHtml)
        XCTAssertEqual(tasks.last?.priority, .normal)
    }

    func testStoreLoadsEveryStatusOnChangesForItsWorkspace() async throws {
        let runner = BacklogRunner()
        let store = BacklogStore(workspace: "w", runner: runner)
        store.handle(.event(BusEvent(seq: 1, topic: "backlog.changed", payload: .object(["workspace": .string("other")]))))
        store.handle(.event(BusEvent(seq: 2, topic: "agents.changed", payload: .object(["workspace": .string("w")]))))
        store.handle(.event(BusEvent(seq: 3, topic: "backlog.changed", payload: .object(["workspace": .string("w"), "id": .number(9)]))))
        try await waitUntil { store.loaded }
        XCTAssertEqual(runner.calls.map(\.args), [
            .object(["since": .string(""), "slim": .bool(true)]),
            .object(["status": .string("not_now"), "slim": .bool(true)]),
            .object(["status": .string("done"), "limit": .number(50), "slim": .bool(true)]),
        ])
        XCTAssertEqual(store.tasks.map(\.id), [7, 9, 4, 11, 3, 2])
    }

    func testAWriteReplysCursorNeverBecomesSince() async throws {
        let runner = SyncRunner([
            BacklogCommand.list: [#"{"tasks":[{"id":7,"title":"Old","status":"todo"}],"removed":[],"order":[7],"cursor":"e.3","delta":false}"#, #"{"tasks":[],"removed":[],"cursor":"e.9","delta":true}"#],
            BacklogCommand.update: [#"{"id":7,"title":"Mine","status":"todo","cursor":"e.9"}"#],
        ])
        let store = BacklogStore(workspace: "w", runner: runner)
        await store.refresh()
        try await store.update(7, ["title": .string("Mine")])
        XCTAssertEqual(runner.args(BacklogCommand.update).first?["seen"], .string("e.3"))
        XCTAssertEqual(runner.args(BacklogCommand.list).map { $0["since"] }, [.string(""), .string("e.3")])
    }

    func testStaleRefusalPullsDeltaAndKeepsDraft() async throws {
        let runner = SyncRunner([
            BacklogCommand.list: [#"{"tasks":[{"id":7,"title":"Old","status":"todo"}],"removed":[],"order":[7],"cursor":"e.3","delta":false}"#, #"{"tasks":[{"id":7,"title":"Theirs","status":"todo"}],"removed":[],"cursor":"e.5","delta":true}"#],
            BacklogCommand.update: ["stale"],
        ])
        let store = BacklogStore(workspace: "w", runner: runner)
        await store.refresh()
        try await store.update(7, ["title": .string("Mine")])
        XCTAssertEqual(store.task(7)?.title, "Theirs")
        XCTAssertEqual(store.queued(7).map(\.failure), [QueuedAction.changedOnMac])
        XCTAssertEqual(store.queued(7).map(\.text), ["Mine"])
    }

    func testSendAgainWaitsForAPullAfterTheRefusalAndSendsOnce() async throws {
        let seven = #"{"tasks":[{"id":7,"title":"Theirs again","status":"todo"}],"removed":[],"cursor":"e.7","delta":true}"#
        let runner = SyncRunner([
            BacklogCommand.list: [#"{"tasks":[{"id":7,"title":"Old","status":"todo"}],"removed":[],"order":[7],"cursor":"e.3","delta":false}"#, #"{"tasks":[{"id":7,"title":"Theirs","status":"todo"}],"removed":[],"cursor":"e.5","delta":true}"#, seven, seven, seven],
            BacklogCommand.update: ["stale", #"{"id":7,"title":"Mine","status":"todo","cursor":"e.8"}"#],
        ])
        let store = BacklogStore(workspace: "w", runner: runner)
        await store.refresh()
        let result = try await store.update(7, ["title": .string("Mine")])
        let refused = try XCTUnwrap(result)
        runner.holdList = true
        let pull = Task { await store.refresh() }
        try await waitUntil { runner.holding }
        let first = Task { try await store.sendAgain(refused) }
        let second = Task { try await store.sendAgain(refused) }
        await Task.yield()
        XCTAssertEqual(runner.args(BacklogCommand.update).count, 1)
        runner.release()
        await pull.value
        try await first.value
        try await second.value
        XCTAssertEqual(runner.args(BacklogCommand.update).map { $0["seen"] }, [.string("e.3"), .string("e.7")])
        XCTAssertEqual(runner.requestIds(BacklogCommand.update).first, refused.requestId)
        XCTAssertNotEqual(runner.requestIds(BacklogCommand.update).last, refused.requestId)
        XCTAssertEqual(store.queued(7), [])
    }

    func testSendAgainPutsTheLineBackWhenItFailsAndSkipsADismissedOne() async throws {
        let runner = SyncRunner([
            BacklogCommand.list: [#"{"tasks":[{"id":7,"title":"Old","status":"todo"}],"removed":[],"order":[7],"cursor":"e.3","delta":false}"#] + Array(repeating: #"{"tasks":[],"removed":[],"cursor":"e.3","delta":true}"#, count: 3),
            BacklogCommand.update: ["stale", "failed"],
        ])
        let store = BacklogStore(workspace: "w", runner: runner)
        await store.refresh()
        let result = try await store.update(7, ["title": .string("Mine")])
        let refused = try XCTUnwrap(result)
        do {
            try await store.sendAgain(refused)
            XCTFail("expected the send to fail")
        } catch {}
        XCTAssertEqual(store.queued(7).map(\.id), [refused.id])
        store.queue.cancel(refused)
        try await store.sendAgain(refused)
        XCTAssertEqual(runner.args(BacklogCommand.update).count, 2)
    }

    func testRebaseKeepsMyEditsTakesTheMacsOthersAndNamesConflicts() {
        let base = BacklogTask(id: 7, title: "Old", description: "d", status: .todo, priority: .normal)
        let mac = BacklogTask(id: 7, title: "Theirs", description: "d2", status: .todo, priority: .normal)
        var mine = BacklogDraft(base)
        mine.title = "Mine"
        mine.priority = .high
        let rebased = mine.rebased(from: base, onto: mac)
        XCTAssertEqual(rebased.draft, BacklogDraft(title: "Mine", description: "d2", priority: .high, status: .todo))
        XCTAssertEqual(rebased.conflicts, [BacklogConflict(field: "Title", mac: "Theirs")])
    }

    func testRebaseNeverSendsAStatusOverAClaimTheEditorNoLongerShows() {
        let base = BacklogTask(id: 7, title: "Old", status: .todo)
        var mine = BacklogDraft(base)
        mine.status = .notNow
        let rebased = mine.rebased(from: base, onto: BacklogTask(id: 7, title: "Old", status: .inProgress))
        XCTAssertEqual(rebased.draft.changes(from: BacklogTask(id: 7, title: "Old", status: .inProgress)), [:])
        XCTAssertEqual(rebased.conflicts, [BacklogConflict(field: "Status", mac: "In progress")])
    }

    func testARefusedEditWithoutTextSaysWhatItChanged() {
        let item = QueuedAction(computer: "c", workspace: "w", command: BacklogCommand.update, args: .object(["id": .number(7), "priority": .string("high"), "status": .string("not_now")]), target: "task/7", title: "Edit #7")
        XCTAssertEqual(item.change, "Priority → High · Status → Not now")
    }

    func testDecodesPrimaryLinksAndAttachmentsFromGet() throws {
        let json = #"{"id": 3, "title": "t", "status": "in_progress", "links": [{"kind": "blocked_by", "task": 8, "title": "Record", "status": "done"}], "attachments": [{"kind": "file", "ref": "a.md", "path": "/r/a.md", "opens": "doc"}, {"kind": "url", "ref": "https://x.y", "opens": "teleport"}], "primary": {"action": "reveal", "label": "Open Coder", "session": "agt_1"}}"#
        let task = try JSONDecoder().decode(BacklogTask.self, from: Data(json.utf8))
        XCTAssertEqual(task.primary, BacklogPrimary(action: .reveal, label: "Open Coder", session: "agt_1"))
        XCTAssertEqual(task.links.first?.detail, "blocked by · done")
        XCTAssertEqual(task.attachments.map(\.opens), [.doc, .other])
        XCTAssertEqual(task.attachments.first?.path, "/r/a.md")
    }

    func testDecodesStartOptionsAndOwnerHistory() throws {
        let json = #"{"id": 4, "title": "t", "status": "todo", "primary": {"action": "start", "label": "Start agent", "start": {"agents": [{"value": "claude", "label": "Claude"}], "modes": [{"value": "", "label": "Last used"}], "where": [{"value": "worktree", "label": "a new worktree"}], "prompt": "Backlog task #4", "owns": "The session owns #4."}}, "ownerState": "running · turn 3", "lastOwner": {"who": "Claude", "what": "stopped · exit 1", "at": "2026-11-02T18:04:00Z", "tone": "danger", "session": "agt_1"}, "history": [{"who": "you", "what": "created"}, {"who": "Claude", "what": "Start agent", "session": ""}]}"#
        let task = try JSONDecoder().decode(BacklogTask.self, from: Data(json.utf8))
        XCTAssertEqual(task.primary?.start, BacklogStart(agents: [BacklogOption(value: "claude", label: "Claude")], modes: [BacklogOption(value: "", label: "Last used")], where: [BacklogOption(value: "worktree", label: "a new worktree")], prompt: "Backlog task #4", owns: "The session owns #4."))
        XCTAssertEqual(task.ownerState, "running · turn 3")
        XCTAssertEqual(task.lastOwner, BacklogOwnerEvent(who: "Claude", what: "stopped · exit 1", at: Date.parseRFC3339("2026-11-02T18:04:00Z"), tone: "danger", session: "agt_1"))
        XCTAssertEqual(task.history.map(\.session), [nil, nil])
    }

    func testStartSendsOnlyTheChosenOptions() async throws {
        let runner = BacklogRunner()
        let store = BacklogStore(workspace: "w", runner: runner)
        try await store.start(4, agent: "claude", mode: "", where: "worktree")
        XCTAssertEqual(runner.calls.first?.id, "backlog.start")
        XCTAssertEqual(runner.calls.first?.args, .object(["id": .number(4), "agent": .string("claude"), "where": .string("worktree")]))
    }

    func testANewTaskOfflineWaitsAsAPendingRow() async throws {
        let runner = BacklogRunner()
        let store = BacklogStore(workspace: "w", runner: runner, queue: ActionQueue(file: nil), computer: "m", queues: { true })
        store.newDraft.title = "Ship it"

        try await store.addNew()

        XCTAssertEqual(store.newDraft, BacklogDraft())
        XCTAssertEqual(store.outbox.map(\.text), ["Ship it"])
        XCTAssertFalse(runner.calls.contains { $0.id == BacklogCommand.add })
    }

    func testDraftSendsOnlyChangedFieldsAndNeverInProgress() {
        let task = BacklogTask(id: 7, title: "Retry", description: "x", status: .inProgress, priority: .high)
        var draft = BacklogDraft(task)
        XCTAssertEqual(draft.changes(from: task), [:])
        draft.title = "  Retry the charge  "
        draft.priority = .low
        XCTAssertEqual(draft.changes(from: task), ["title": .string("Retry the charge"), "priority": .string("low")])
        draft.status = .done
        XCTAssertEqual(draft.changes(from: task)["status"], .string("done"))
    }
}

@MainActor
final class BacklogRunner: CommandRunner {
    private(set) var calls: [(id: String, args: JSONValue?)] = []
    var failure: RemoteError?

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        calls.append((id, args))
        if let failure { throw failure }
        let all = try JSONDecoder().decode(JSONValue.self, from: fixture("backlog-tasks"))["tasks"]?.arrayValue ?? []
        let status = args?["status"]?.stringValue ?? "open"
        let open: Set<String> = ["idea", "todo", "in_progress"]
        let tasks = all.filter { task in
            let value = task["status"]?.stringValue ?? ""
            return status == "open" ? open.contains(value) : value == status
        }
        return .object(["tasks": .array(tasks)])
    }
}

@MainActor
final class SyncRunner: CommandRunner {
    private var replies: [String: [String]]
    private(set) var calls: [(id: String, args: JSONValue)] = []
    private var requests: [(id: String, request: String?)] = []
    var holdList = false
    private var held: CheckedContinuation<Void, Never>?

    var holding: Bool {
        held != nil
    }

    func release() {
        held?.resume()
        held = nil
    }

    init(_ replies: [String: [String]]) {
        self.replies = replies
    }

    func args(_ id: String) -> [JSONValue] {
        calls.filter { $0.id == id }.map(\.args)
    }

    func requestIds(_ id: String) -> [String?] {
        requests.filter { $0.id == id }.map(\.request)
    }

    func command(_ id: String, workspace: String?, args: JSONValue?, requestId: String?) async throws -> JSONValue {
        requests.append((id, requestId))
        return try await command(id, workspace: workspace, args: args)
    }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        calls.append((id, args ?? .null))
        if id == BacklogCommand.list, holdList {
            holdList = false
            await withCheckedContinuation { held = $0 }
        }
        guard let reply = replies[id]?.first else { return .null }
        replies[id]?.removeFirst()
        if reply == ServerReason.stale { throw RemoteError.server(code: ServerReason.stale, message: "#7 changed") }
        if reply == "failed" { throw RemoteError.server(code: "failed", message: "boom") }
        return try JSONDecoder().decode(JSONValue.self, from: Data(reply.utf8))
    }
}
