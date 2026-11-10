import XCTest

import CompanionKit

private let connectNode = """
{"type": "request.connectRepo", "id": "s/connect/cr_1", "props": {"request": "cr_1", "title": "Connect 2 repos?", "connect": "Connect", "seen": "abc",
 "repos": [{"key": "/r/auth", "name": "auth", "path": "~/r/auth", "remote": "github.com/acme/auth", "state": "main · clean"},
           {"key": "/r/ads", "name": "ads", "path": "~/r/ads", "state": "3 behind origin/main", "warn": true}]}}
"""

private let landNode = """
{"type": "request.land", "id": "s/land/lr_1", "props": {"request": "lr_1", "title": "Land auth?", "name": "auth",
 "options": [{"id": "review", "label": "Review…"}, {"id": "merge", "label": "Merge"}, {"id": "leave", "label": "Leave it unlanded…"}]}}
"""

private let reposNode = """
{"type": "repos", "id": "s/repos", "props": {"title": "2 repos", "foldKey": "card/1", "action": "a", "rowAction": "b", "summary": [{"text": "1 failed", "tone": "danger"}],
 "footer": "All children done", "footerAction": "s/repos/footer",
 "rows": [{"id": "r1", "order": 1, "name": "auth", "path": "/r/auth", "session": "kid", "landsBy": "merge", "state": "failed", "tone": "danger", "opens": true, "open": true, "tail": "tests failed", "actions": "s/repos/r1/actions", "foldKey": "failed/1"},
          {"id": "r2", "order": 2, "name": "ads", "landsBy": "merge", "state": "waits for 1", "opens": true, "foldKey": "waits/0"}]},
 "children": [{"type": "stack", "id": "s/repos/r1/actions", "children": [
   {"type": "button", "id": "fix", "props": {"label": "Ask it to fix", "action": "repoFix"}},
   {"type": "button", "id": "session", "props": {"label": "Open its session", "action": "repoSession"}},
   {"type": "button", "id": "code", "props": {"label": "Open in Code", "action": "repoCode"}},
   {"type": "spacer"},
   {"type": "button", "id": "leave", "props": {"label": "Leave it unlanded…", "action": "repoLeave", "confirm": "Leave auth unlanded?"}}]},
  {"type": "stack", "id": "s/repos/footer", "children": [{"type": "button", "id": "archive", "props": {"label": "Archive task…", "action": "archive"}}]}]}
"""

private let planJSON = """
{"session": "s", "title": "Pause", "workspace": "ads", "sessions": 3, "seen": "plan1", "blockers": [], "holds": [], "repos": [
 {"name": "auth", "path": "/r/auth", "worktree": {"action": "remove"}, "branch": {"action": "delete"}, "summary": "Worktree removed · branch deleted", "seen": "tip1", "state": {"path": "/r/auth/.worktrees/x"}},
 {"name": "ads", "path": "/r/ads", "worktree": {"action": "keep"}, "branch": {"action": "keep"}, "summary": "Worktree kept · branch kept", "why": "1 file would be lost — open it on the Mac", "whyTone": "warning", "seen": "tip2", "state": {"path": "/r/ads/.worktrees/x"}}]}
"""

private func decoded<T: Decodable>(_ json: String, as type: T.Type = T.self) throws -> T {
    try JSONDecoder.clubmate.decode(type, from: Data(json.utf8))
}

private func transcript(request: String = "null", repos: String = "null", session: String = #"{"id": "s", "status": "done"}"#) throws -> AgentTranscript {
    try decoded(#"{"session": \#(session), "entries": [], "request": \#(request), "repos": \#(repos)}"#)
}

private func connectCard() throws -> ConnectCard {
    guard case .connect(let card) = try transcript(request: connectNode).request else { throw RemoteError.badResponse }
    return card
}

private func landCard() throws -> LandCard {
    guard case .land(let card) = try transcript(request: landNode).request else { throw RemoteError.badResponse }
    return card
}

private func tempFile() -> URL {
    FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "queue.json")
}

@MainActor
private final class TaskListRunner: CommandRunner {
    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        guard id == AgentCommand.sessions else { return .array([]) }
        return try JSONDecoder().decode(JSONValue.self, from: Data("""
        [{"id": "o", "status": "done", "taskFinished": true, "taskRows": [{"name": "ok", "session": "fine", "word": "merged"}, {"name": "bad", "session": "bad", "word": "failed", "tone": "danger"}]},
         {"id": "fine", "status": "done", "parentSession": "o", "repo": "ok"}, {"id": "bad", "status": "error", "parentSession": "o", "repo": "bad"}]
        """.utf8))
    }
}

@MainActor
private final class TaskRunner: CommandRunner {
    var requests: [String]
    var failure: (any Error)?
    var reply: JSONValue = .object([:])
    var during: (() -> Void)?
    private(set) var sent: [(id: String, args: JSONValue?)] = []

    init(requests: [String] = [], failure: (any Error)? = nil) {
        self.requests = requests
        self.failure = failure
    }

    var commands: [String] { sent.map(\.id) }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        if id == AgentCommand.sessions {
            return .array([.object(["id": .string("s"), "status": .string("done"), "requests": .array(requests.map(JSONValue.string))])])
        }
        sent.append((id, args))
        during?()
        if let failure { throw failure }
        return reply
    }
}

@MainActor
final class TaskCardsTests: XCTestCase {
    func testAConnectIsAnsweredPerRepo() async throws {
        let card = try connectCard()
        let runner = TaskRunner(requests: ["cr_1"])
        let store = AgentsStore(workspace: "w", runner: runner)
        var draft = ConnectDraft()
        draft.toggle(card.repos[1])

        let delivery = try await store.answerConnect("s", card: card, answers: card.answers(draft))

        XCTAssertEqual(delivery, .sent)
        let args = try XCTUnwrap(runner.sent.last { $0.id == TaskCommand.connectAnswer }?.args)
        XCTAssertEqual(args["request"]?.stringValue, "cr_1")
        XCTAssertEqual(args["seen"]?.stringValue, "abc")
        XCTAssertEqual(args["answers"]?.arrayValue?.map { $0["path"]?.stringValue }, ["/r/auth", "/r/ads"])
        XCTAssertEqual(args["answers"]?.arrayValue?.map { $0["action"]?.stringValue }, ["connect", "skip"])
    }

    func testAnotherFolderSendsTheUserToTheMac() throws {
        XCTAssertTrue(ConnectCard.footnote.contains("another folder: on the Mac"))
        XCTAssertNil(ConnectAnswer.Action(rawValue: "other"))
    }

    func testMergeIsRefusedOfflineAndNeverQueues() async throws {
        let runner = TaskRunner(requests: ["lr_1"])
        let queue = ActionQueue(file: nil)
        let store = AgentsStore(workspace: "w", runner: runner, queue: queue, computer: "m", queues: { true })

        await XCTAssertThrowsAsync(try await store.land("s", card: try landCard(), action: LandCard.merge), OfflineRefused())

        XCTAssertTrue(queue.items.isEmpty)
        XCTAssertFalse(runner.commands.contains(TaskCommand.landAnswer))
    }

    func testConnectAndArchiveQueueSurviveARestartAndGoOutOnReconnect() async throws {
        let file = tempFile()
        let offline = TaskRunner()
        let queue = ActionQueue(file: file)
        let store = AgentsStore(workspace: "w", runner: offline, queue: queue, computer: "m", queues: { true })
        let card = try connectCard()
        let plan: ArchivePlan = try decoded(planJSON)

        let connect = try await store.answerConnect("s", card: card, answers: card.declineAll)
        let archive = try await store.archive("other", plan: plan, mode: .archive).0

        XCTAssertEqual([connect, archive], [.queued, .queued])
        XCTAssertTrue(offline.sent.isEmpty)

        let restarted = ActionQueue(file: file)
        XCTAssertEqual(restarted.items.map(\.kind), [.connect, .archive])
        let runner = TaskRunner(requests: ["cr_1"])
        _ = await restarted.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertEqual(runner.commands, [TaskCommand.connectAnswer, TaskCommand.archive])
        let sent = try XCTUnwrap(runner.sent.last?.args)
        XCTAssertEqual(sent["seen"]?.stringValue, "plan1")
        XCTAssertEqual(sent["choices"]?.arrayValue?.map { $0["path"]?.stringValue }, ["/r/auth/.worktrees/x"], "only what the plan removes")
        XCTAssertTrue(restarted.items.isEmpty)
        XCTAssertTrue(ActionQueue(file: file).items.isEmpty)
    }

    func testAQueueGoesOutOnceTheMacIsOnline() async throws {
        let file = tempFile()
        ActionQueue(file: file).enqueue(QueuedAction(computer: "mac1", workspace: "w", session: "s", kind: .archive, title: "Archive Pause"))
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621), token: "t")
        let hello = #"{"computer":{"id":"mac1","name":"Mac"},"version":"1","commands":["\#(TaskCommand.archive)"],"topics":[]}"#
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(#"[{"id":"w","name":"ads"}]"#), 200)
            case "hello": (FakeTransport.result(hello), 200)
            default: (FakeTransport.result("{}"), 200)
            }
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), queueFile: file, sleep: { _ in })

        model.start()

        try await waitUntil { model.queue.items.isEmpty }
        XCTAssertTrue(transport.requests.contains { FakeTransport.rpc($0) == TaskCommand.archive })
        model.connection("mac1")?.stop()
    }

    func testAStaleOneIsDroppedWithANote() async throws {
        let queue = ActionQueue(file: nil)
        queue.enqueue(QueuedAction(computer: "m", workspace: "w", session: "s", kind: .archive, title: "Archive Pause", seen: "old"))
        let runner = TaskRunner(failure: RemoteError.server(code: ServerReason.stale, message: "this task changed since you saw the plan"))

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertTrue(queue.items.isEmpty)
        XCTAssertEqual(queue.notes(session: "s").map(\.text), ["Archive Pause wasn't sent: this task changed since you saw the plan."])
    }

    func testAMacThatStaysOfflineKeepsTheQueue() async throws {
        let queue = ActionQueue(file: nil)
        queue.enqueue(QueuedAction(computer: "m", workspace: "w", session: "s", kind: .archive, title: "Archive Pause"))
        let runner = TaskRunner(failure: RemoteError.unreachable("gone"))

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertEqual(queue.items.count, 1)
        XCTAssertTrue(queue.notes.isEmpty)
    }

    func testAnAnswerGivenElsewhereIsNotSentAgain() async throws {
        let queue = ActionQueue(file: nil)
        queue.enqueue(QueuedAction(computer: "m", workspace: "w", session: "s", kind: .connect, title: "Connect auth", request: "cr_1"))
        let runner = TaskRunner(requests: [])

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })
        let store = AgentsStore(workspace: "w", runner: runner)
        await XCTAssertThrowsAsync(try await store.answerConnect("s", card: try connectCard(), answers: []).void, AnswerError.alreadyAnswered)

        XCTAssertFalse(runner.commands.contains(TaskCommand.connectAnswer))
        XCTAssertEqual(queue.notes(session: "s").map(\.text), ["Connect auth: already answered, so yours wasn't sent."])
    }

    func testARealQuestionBeatsTheFailedRepoRow() throws {
        let question: AgentSession = try decoded(#"{"id": "s", "status": "done", "needsYou": true, "asks": true, "pendingSummary": {"kind": "question", "text": "Which scope?"}}"#)
        let failed: AgentSession = try decoded(#"{"id": "s", "status": "done", "needsYou": true, "pendingSummary": {"kind": "failed", "text": "auth failed", "onMac": true}}"#)
        let repos = try transcript(repos: reposNode).repos
        let asked: TranscriptEntry = try decoded(#"{"kind": "agent", "id": "a", "turn": 1, "text": "Which scope?"}"#)

        XCTAssertEqual(SessionScreen(entries: [asked], session: question, repos: repos).dock, .question("Which scope?"))
        XCTAssertNil(SessionScreen(entries: [], session: failed, repos: repos).dock)
    }

    func testAFailedRowOpensByItselfAsOnTheMac() throws {
        let repos = try XCTUnwrap(try transcript(repos: reposNode).repos)
        let failed = repos.rows[0]
        var fold = ReposFold()

        XCTAssertTrue(fold.open(failed))
        XCTAssertFalse(fold.open(repos.rows[1]))
        XCTAssertEqual(failed.buttons.map(\.kind), [.fix, .session, .leave], "Open in Code stays on the Mac")
        XCTAssertEqual(failed.buttons.last?.confirm, "Leave auth unlanded?")
        XCTAssertEqual(repos.archive, "Archive task…")

        fold.toggle(failed)
        XCTAssertFalse(fold.open(failed))
        var same = failed
        same.tail = "the same failure, more output"
        XCTAssertFalse(fold.open(same), "the plugin's key decides, as on the Mac")
        var again = failed
        again.foldKey = "failed/2"
        XCTAssertTrue(fold.open(again), "a new failure opens again")
    }

    func testAConnectCardBeatsTheMacRowAndAnAnsweredOneIsGone() async throws {
        let session: AgentSession = try decoded(#"{"id": "s", "status": "done", "needsYou": true, "pending": ["cr_1"], "requests": ["cr_1"], "pendingSummary": {"kind": "connect", "text": "Connect 2 repos?", "onMac": true}}"#)
        let card = try transcript(request: connectNode, session: #"{"id": "s", "status": "done", "requests": ["cr_1"]}"#).request

        XCTAssertEqual(SessionScreen(entries: [], session: session, request: card).dock, .connect(try connectCard()))
    }

    func testRepoRowsShowInLandingOrderWithTheOneNotConnectedLast() throws {
        let sessions: [AgentSession] = try decoded("""
        [{"id": "o", "status": "done", "needsYou": true, "pendingSummary": {"kind": "connect", "text": "Connect auth?", "lead": "Wants to connect", "subject": "auth", "onMac": true, "at": "2026-11-06T10:00:00Z"},
          "taskRows": [{"name": "ads", "session": "b"}, {"name": "core", "session": "a"}, {"name": "auth", "path": "/r/auth", "word": "not connected yet", "tone": "warning", "connect": true}]},
         {"id": "a", "status": "running", "parentSession": "o", "repo": "core", "createdAt": "2026-11-06T10:01:00Z"},
         {"id": "b", "status": "running", "parentSession": "o", "repo": "ads", "createdAt": "2026-11-06T10:00:00Z"}]
        """)
        let node = try XCTUnwrap(AgentSession.grouped(sessions).first?.sessions.first)

        let rows = node.rows(expanded: [])

        XCTAssertEqual(rows.map { $0.repo?.name ?? $0.node.id }, ["o", "b", "a", "auth"])
        XCTAssertEqual(node.session.glyph, .connect)
        let line = node.session.stateLine(now: try XCTUnwrap(node.session.pendingSummary?.at))
        XCTAssertEqual([line.lead, line.text], ["Wants to connect", "auth"])
        let folded = node.rows(expanded: [], folded: ["o"])
        XCTAssertEqual(folded.map { $0.repo?.name ?? $0.node.id }, ["o", "auth"], "a row that needs you stays pinned")
        XCTAssertEqual(folded[0].toggleCount, 2)
    }

    func testAFinishedTaskFoldsOnceWhenOpened() async throws {
        let runner = TaskListRunner()
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.refresh()
        let tree = { try XCTUnwrap(AgentSession.grouped(store.sessions).first { $0.sessions.contains { $0.id == "o" } }?.sessions.first { $0.id == "o" }) }
        XCTAssertEqual(try tree().rows(expanded: store.expanded, folded: store.foldedTasks).count, 3)

        store.opened("o")
        let rows = try tree().rows(expanded: store.expanded, folded: store.foldedTasks)
        XCTAssertEqual(rows.map(\.id), ["o", "bad"], "a failed child stays pinned")

        store.toggleTree(rows[0])
        store.opened("o")
        XCTAssertEqual(try tree().rows(expanded: store.expanded, folded: store.foldedTasks).count, 3, "once only")
    }

    func testANoTaskSessionWithAConnectRequestFolds() throws {
        let sessions: [AgentSession] = try decoded(#"[{"id": "n", "status": "done", "needsYou": true, "taskRows": [{"name": "auth", "path": "/r/auth", "word": "not connected yet", "connect": true}]}]"#)
        let node = try XCTUnwrap(AgentSession.grouped(sessions).first?.sessions.first)

        XCTAssertEqual(node.rows(expanded: []).first?.toggleCount, 1)
        XCTAssertEqual(node.rows(expanded: [], folded: ["n"]).count, 1)
    }

    func testUndoDuringAFlushStopsTheItem() async throws {
        let queue = ActionQueue(file: nil)
        let first = QueuedAction(computer: "m", workspace: "w", session: "a", kind: .archive, title: "Archive A")
        let second = QueuedAction(computer: "m", workspace: "w", session: "b", kind: .archive, title: "Archive B")
        queue.enqueue(first)
        queue.enqueue(second)
        let runner = TaskRunner()
        runner.during = { queue.cancel(second) }

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertEqual(runner.sent.map { $0.args?["session"]?.stringValue }, ["a"])
        XCTAssertTrue(queue.items.isEmpty)
    }

    func testAResendCarriesItsKeyAndReportsWhatStayed() async throws {
        let queue = ActionQueue(file: nil)
        let plan: ArchivePlan = try decoded(planJSON)
        let lost = TaskRunner(failure: RemoteError.unreachable("gone"))
        var retried = false
        let store = AgentsStore(workspace: "w", runner: lost, queue: queue, computer: "m", retry: { retried = true }, sleep: { _ in })

        let delivery = try await store.archive("s", plan: plan, mode: .archive).0

        XCTAssertEqual(delivery, .queued, "the answer was lost, so the Mac may have run it")
        XCTAssertEqual(queue.items.first?.attempted, true)
        try await waitUntil { retried }

        let runner = TaskRunner()
        runner.reply = .object(["kept": .array([.string("ads stays on disk")])])
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertEqual(runner.sent.first?.args?["key"], lost.sent.first?.args?["key"])
        XCTAssertEqual(queue.notes(session: "s").map(\.text), ["Archive Pause ran; some of it stayed:\nads stays on disk"])
    }

    func testAnActionThatNeverLeftThePhoneStaysUndoable() async throws {
        let queue = ActionQueue(file: nil)
        let plan: ArchivePlan = try decoded(planJSON)
        let offline = TaskRunner(failure: RemoteError.notSent("Could not connect to the server."))
        let store = AgentsStore(workspace: "w", runner: offline, queue: queue, computer: "m", sleep: { _ in })

        let delivery = try await store.archive("s", plan: plan, mode: .archive).0
        XCTAssertEqual(delivery, .queued)
        XCTAssertEqual(queue.items.first?.canUndo, true)

        _ = await queue.flush(computer: "m", runner: offline, supports: { _ in true })
        let item = try XCTUnwrap(queue.items.first)
        XCTAssertTrue(item.canUndo)
        XCTAssertEqual(item.line(queued: "Queued"), "Queued")
    }

    func testAnUnknownOutcomeReadsAsUnknown() async throws {
        let queue = ActionQueue(file: nil)
        queue.enqueue(QueuedAction(computer: "m", workspace: "w", session: "s", kind: .archive, title: "Archive Pause"))
        let runner = TaskRunner(failure: RemoteError.server(code: ServerReason.unknown, message: "the first try was cut off, so its outcome is unknown"))

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertEqual(queue.notes(session: "s").map(\.text), ["Archive Pause: the Mac never confirmed it, so it may have run. Check it on the Mac."])
    }

    func testATransientErrorKeepsTheItem() async throws {
        for message in ["plugin agents is not running", "command is not available remotely: agents.archive", "dial tcp: connection refused", "this says refused and already answered"] {
            let queue = ActionQueue(file: nil)
            queue.enqueue(QueuedAction(computer: "m", workspace: "w", session: "s", kind: .archive, title: "Archive Pause"))
            let runner = TaskRunner(failure: RemoteError.server(code: "failed", message: message))

            _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

            XCTAssertEqual(queue.items.count, 1, message)
            XCTAssertTrue(queue.notes.isEmpty, message)
        }
    }

    func testALastingErrorBacksOffAndEndsVisibleAndDismissable() async throws {
        var clock = Date(timeIntervalSince1970: 1_000)
        let queue = ActionQueue(file: nil, now: { clock })
        let item = QueuedAction(computer: "m", workspace: "w", session: "s", kind: .archive, title: "Archive Pause")
        queue.enqueue(item)
        let runner = TaskRunner(failure: RemoteError.server(code: "failed", message: "disk full"))

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })
        XCTAssertEqual(runner.sent.count, 1, "the second flush is inside the backoff")
        XCTAssertEqual(queue.nextWait(computer: "m"), ActionQueue.firstWait)
        XCTAssertFalse(queue.isDue(computer: "m"))

        for _ in 1..<ActionQueue.maxTries {
            clock = clock.addingTimeInterval(ActionQueue.longestWait)
            _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })
        }
        XCTAssertEqual(runner.sent.count, ActionQueue.maxTries)
        clock = clock.addingTimeInterval(ActionQueue.longestWait)
        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })
        XCTAssertEqual(runner.sent.count, ActionQueue.maxTries, "it has stopped")

        let stopped = try XCTUnwrap(queue.items.first)
        XCTAssertEqual(stopped.detail, "disk full")
        XCTAssertEqual(stopped.line(queued: "x"), "Archive Pause didn't go through")
        XCTAssertTrue(stopped.canDismiss)
        queue.cancel(stopped)
        XCTAssertTrue(queue.items.isEmpty)
    }

    func testAMissingWorkspaceIsStaleAtOnce() async throws {
        let queue = ActionQueue(file: nil)
        queue.enqueue(QueuedAction(computer: "m", workspace: "gone", session: "s", kind: .archive, title: "Archive Pause"))
        let runner = TaskRunner(failure: RemoteError.server(code: ServerReason.stale, message: "workspace not found"))

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in true })

        XCTAssertTrue(queue.items.isEmpty)
        XCTAssertEqual(queue.notes.map(\.text), ["Archive Pause wasn't sent: workspace not found."])
    }

    func testAnAttemptedItemOffersNoUndoAndAQueuedOneCantReplaceIt() async throws {
        let queue = ActionQueue(file: nil)
        let plan: ArchivePlan = try decoded(planJSON)
        let lost = TaskRunner(failure: RemoteError.unreachable("gone"))
        let store = AgentsStore(workspace: "w", runner: lost, queue: queue, computer: "m", sleep: { _ in })
        _ = try await store.archive("s", plan: plan, mode: .archive)
        let attempted = try XCTUnwrap(queue.items.first)

        XCTAssertFalse(attempted.canUndo)
        XCTAssertFalse(attempted.canDismiss)
        XCTAssertEqual(attempted.line(queued: "x"), "Archive Pause may have run")
        XCTAssertEqual(attempted.detail, "Check it on the Mac")
        queue.cancel(attempted)
        XCTAssertEqual(queue.items.count, 1, "cancelling can't take back what may have run")

        queue.enqueue(QueuedAction(computer: "m", workspace: "w", session: "s", kind: .archive, title: "Archive Pause"))
        XCTAssertEqual(queue.items.map(\.id), [attempted.id], "the key stays")

        let online = TaskRunner()
        let again = AgentsStore(workspace: "w", runner: online, queue: queue, computer: "m")
        let delivery = try await again.archive("s", plan: plan, mode: .archive).0
        XCTAssertEqual(delivery, .queued)
        XCTAssertTrue(online.sent.isEmpty, "a second key isn't sent while the first is unaccounted for")
    }

    func testAnOlderMacGetsANoteAndARestartingPluginDoesNot() async throws {
        let queue = ActionQueue(file: nil)
        queue.enqueue(QueuedAction(computer: "m", workspace: "w", session: "s", kind: .archive, title: "Archive Pause"))
        let runner = TaskRunner()

        _ = await queue.flush(computer: "m", runner: runner, supports: { _ in false })
        XCTAssertTrue(queue.notes.isEmpty, "agents isn't listed at all: a restart")

        for _ in 0..<2 {
            _ = await queue.flush(computer: "m", runner: runner, supports: { $0 == AgentCommand.sessions })
        }

        XCTAssertEqual(queue.items.count, 1, "it can still go out once the Mac is updated")
        XCTAssertEqual(queue.notes.map(\.text), ["Archive Pause waits: this Mac's Clubmate is too old for it. Update Clubmate, or Undo."])
    }

    func testPluginChangedFlushesAQueueThatWaitedForACommand() async throws {
        let file = tempFile()
        ActionQueue(file: file).enqueue(QueuedAction(computer: "mac1", workspace: "w", session: "s", kind: .archive, title: "Archive Pause"))
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621), token: "t")
        let hellos = Recorder<Int>()
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": return (FakeTransport.result(#"[{"id":"w","name":"ads"}]"#), 200)
            case "hello":
                hellos.add(1)
                let commands = hellos.values.count > 1 ? #"["\#(TaskCommand.archive)"]"# : "[]"
                return (FakeTransport.result(#"{"computer":{"id":"mac1","name":"Mac"},"version":"1","commands":\#(commands),"topics":[]}"#), 200)
            default: return (FakeTransport.result("{}"), 200)
            }
        }, stream: { _ in
            let wire = ": connected\n\nid: 1\nevent: bus\ndata: {\"seq\":1,\"topic\":\"\(Topic.pluginChanged)\",\"payload\":{}}\n\n"
            return (200, AsyncThrowingStream { $0.yield(Data(wire.utf8)) })
        })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), queueFile: file, sleep: { _ in })

        model.start()

        try await waitUntil { model.queue.items.isEmpty }
        model.connection("mac1")?.stop()
    }

    func testMergeWaitsForTheCommitsAndCarriesTheTipItShowed() async throws {
        let card = try landCard()
        let commits: LandCommits = try decoded(#"{"commits": [{"sha": "a1", "title": "x"}], "tip": "abc", "detail": "Merge · 1 commit"}"#)
        XCTAssertFalse(card.canMerge(commits: nil, blocked: nil))
        XCTAssertFalse(card.canMerge(commits: commits, blocked: "offline"))
        XCTAssertTrue(card.canMerge(commits: commits, blocked: nil))

        let runner = TaskRunner(requests: ["lr_1"])
        let store = AgentsStore(workspace: "w", runner: runner)
        try await store.land("s", card: card, action: LandCard.merge, tip: commits.tip)

        XCTAssertEqual(runner.sent.last { $0.id == TaskCommand.landAnswer }?.args?["tip"]?.stringValue, "abc")
    }

    func testScreensWithNoTaskAreUnchanged() throws {
        let sessions: [AgentSession] = try decoded(#"[{"id": "p", "status": "running", "title": "Plain"}, {"id": "k", "status": "running", "parentSession": "p"}]"#)
        let node = try XCTUnwrap(AgentSession.grouped(sessions).first?.sessions.first)
        let plain = try transcript()

        XCTAssertEqual(node.rows(expanded: []).map(\.id), ["p"])
        XCTAssertEqual(node.rows(expanded: []).first?.toggleCount, 1)
        XCTAssertEqual(node.session.headerDetail, "")
        XCTAssertEqual(sessions[1].headerDetail, "subagent")
        XCTAssertNil(plain.request)
        XCTAssertNil(plain.repos)
        XCTAssertNil(SessionScreen(entries: [], session: sessions[0], request: plain.request, repos: plain.repos).dock)
        XCTAssertNil(Workspace(id: "w", name: "ads", folders: [.init(path: "/a")]).foldersLine)
        XCTAssertEqual(Workspace(id: "w", name: "ads", folders: [.init(path: "/a", seen: true), .init(path: "/b", seen: false)]).foldersLine, "2 folders")
    }

    func testMarkdownFromARepoNeverBecomesALink() {
        let text = PlainMarkdown.attributed("1 graph **[x](https://evil.example)**")

        XCTAssertEqual(String(text.characters), "1 graph x")
        XCTAssertFalse(text.runs.contains { $0.link != nil })
    }
}

private extension Delivery {
    var void: Void { () }
}
