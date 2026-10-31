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
            .object(["status": .string("open")]),
            .object(["status": .string("not_now")]),
            .object(["status": .string("done"), "limit": .number(50)]),
        ])
        XCTAssertEqual(store.tasks.map(\.id), [7, 9, 4, 11, 3, 2])
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

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        calls.append((id, args))
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
