import XCTest
@testable import CompanionKit

@MainActor
final class ActionQueueTests: XCTestCase {
    func testRetryReusesRequestId() async throws {
        let queue = ActionQueue(file: nil)
        let lost = OutboxRunner(failures: [RemoteError.unreachable("gone")])
        let store = BacklogStore(workspace: "w", runner: lost, queue: queue, computer: "m")

        try await store.update(7, ["title": .string("Renamed")])

        XCTAssertEqual(queue.items.count, 1, "the outcome was lost, the write stays queued")
        _ = await queue.flush(computer: "m", runner: lost, supports: { _ in true })

        XCTAssertTrue(queue.items.isEmpty)
        let sent = lost.sent.filter { $0.id == BacklogCommand.update }
        XCTAssertEqual(sent.count, 2)
        XCTAssertNotNil(sent[0].requestId)
        XCTAssertEqual(sent[0].requestId, sent[1].requestId)
    }

    func testStuckItemDoesNotHoldOtherTargets() async throws {
        let queue = ActionQueue(file: nil)
        let first = Self.update(7, title: "A")
        let second = Self.update(7, title: "B")
        let elsewhere = Self.update(7, title: "C", workspace: "w2")
        let other = Self.update(9, title: "D")
        for item in [first, second, elsewhere, other] { queue.enqueue(item) }
        let runner = OutboxRunner(failures: [RemoteError.server(code: "failed", message: "plugin backlog is not running")])

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertEqual(runner.sent.map(\.requestId), [first.id, elsewhere.id, other.id])
        XCTAssertEqual(queue.items.map(\.id), [first.id, second.id])
    }

    func testAFailedItemDoesNotHoldItsTarget() async throws {
        let queue = ActionQueue(file: nil)
        let refused = Self.update(7, title: "A")
        let next = Self.update(7, title: "B")
        queue.enqueue(refused)
        queue.enqueue(next)
        let runner = OutboxRunner(failures: [RemoteError.server(code: ServerReason.refused, message: "no")])

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertEqual(runner.sent.map(\.requestId), [refused.id, next.id])
        XCTAssertEqual(queue.items.map(\.id), [refused.id])
    }

    func testBacklogUpdateShowsOverlayUntilLoaded() async throws {
        let queue = ActionQueue(file: nil)
        let runner = BacklogRunner()
        let store = BacklogStore(workspace: "w", runner: runner, queue: queue, computer: "m", queues: { true })
        await store.refresh()
        let task = try XCTUnwrap(store.tasks.first)

        try await store.update(task.id, ["title": .string("Renamed")])
        XCTAssertEqual(store.task(task.id)?.title, "Renamed")
        XCTAssertEqual(store.queued(task.id).count, 1)

        let flushed = await queue.flush(computer: "m", runner: runner, supports: { _ in true })
        flushed.landed.forEach(store.landed)
        XCTAssertEqual(store.task(task.id)?.title, "Renamed", "delivered, not loaded yet")

        await store.refresh()
        XCTAssertEqual(store.task(task.id)?.title, task.title, "the Mac's copy wins once loaded")
        XCTAssertTrue(store.queued(task.id).isEmpty)
    }

    func testRefusedItemDropsOverlayAndKeepsText() async throws {
        for (code, line) in [(ServerReason.refused, " didn't go through"), (ServerReason.unknown, " may have run"), ("invalid_params", " didn't go through")] {
            let queue = ActionQueue(file: nil)
            let runner = BacklogRunner()
            let store = BacklogStore(workspace: "w", runner: runner, queue: queue, computer: "m", queues: { true })
            await store.refresh()
            let task = try XCTUnwrap(store.tasks.first)
            try await store.update(task.id, ["title": .string("Renamed")])

            runner.failure = .server(code: code, message: "task was deleted")
            _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

            XCTAssertEqual(store.task(task.id)?.title, task.title, code)
            let refused = try XCTUnwrap(queue.items.first, code)
            XCTAssertEqual(refused.text, "Renamed", code)
            XCTAssertEqual(refused.line(queued: "x"), refused.title + line, code)
            XCTAssertTrue(refused.canDismiss, code)
        }
    }

    func testAQueuedPromptShowsOnceWhenTheTranscriptArrivesFirst() async throws {
        let queue = ActionQueue(file: nil)
        let runner = PromptRunner()
        let store = AgentsStore(workspace: "w", runner: runner, queue: queue, computer: "m", queues: { true })
        try await store.send("Fix it", to: "s")
        XCTAssertEqual(prompts(store), ["Fix it"])

        let flushed = await queue.flush(computer: "m", runner: runner, supports: { _ in true })
        await store.loadTranscript("s")
        flushed.landed.forEach(store.landed)

        XCTAssertEqual(prompts(store), ["Fix it"])
    }

    private func prompts(_ store: AgentsStore) -> [String] {
        store.entries("s").compactMap { entry in
            guard case .user(let text, _, _, _) = entry.content else { return nil }
            return text
        }
    }

    func testAQueueFromAnOlderAppStillLoads() throws {
        let file = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString + ".json")
        let old = #"{"items":[{"id":"q1","computer":"m","workspace":"w","session":"s","kind":"archive","title":"Archive Pause","answers":[],"choices":[],"at":"2026-11-01T10:00:00Z","attempted":false,"noted":false,"tries":0}],"notes":[]}"#
        try Data(old.utf8).write(to: file)

        let queue = ActionQueue(file: file)

        XCTAssertEqual(queue.items.map(\.id), ["q1"])
        XCTAssertEqual(queue.items.first?.target, "session/s")
    }

    private static func update(_ id: Int, title: String, workspace: String = "w") -> QueuedAction {
        QueuedAction(computer: "m", workspace: workspace, command: BacklogCommand.update, args: .object(["id": .number(Double(id)), "title": .string(title)]), target: "task/\(id)", title: "Edit task")
    }
}

final class PromptRunner: CommandRunner {
    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        switch id {
        case AgentCommand.transcript:
            try JSONDecoder().decode(JSONValue.self, from: Data(#"{"session":{"id":"s","status":"running"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"Fix it"}]}"#.utf8))
        case AgentCommand.sessions:
            .array([])
        default:
            .object([:])
        }
    }
}

final class OutboxRunner: CommandRunner {
    var failures: [any Error]
    private(set) var sent: [(id: String, args: JSONValue?, requestId: String?)] = []

    init(failures: [any Error] = []) {
        self.failures = failures
    }

    var dedupes: Bool { true }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        try await command(id, workspace: workspace, args: args, requestId: nil)
    }

    func command(_ id: String, workspace: String?, args: JSONValue?, requestId: String?) async throws -> JSONValue {
        if id == BacklogCommand.list { return .object(["tasks": .array([])]) }
        sent.append((id, args, requestId))
        if !failures.isEmpty { throw failures.removeFirst() }
        return .object([:])
    }
}
