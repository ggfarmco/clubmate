import Foundation
import XCTest

import CompanionKit

@MainActor
final class AgentsTests: XCTestCase {
    func testDecodesSessionsFixtureIntoGroups() throws {
        let sessions = try JSONDecoder.clubmate.decode([AgentSession].self, from: fixture("agents-sessions"))
        XCTAssertEqual(sessions.map(\.status), [.waiting, .done, .closed])
        XCTAssertEqual(sessions[0].pending, ["permission-3"])
        XCTAssertEqual(sessions[0].createdAt, Date.parseRFC3339("2026-10-30T09:41:00Z"))
        XCTAssertEqual(AgentSession.grouped(sessions).map { $0.group }, [.needsInput, .closed])
        XCTAssertEqual(AgentSession.grouped(sessions)[0].sessions.map(\.id).sorted(), ["agt_0000000000000001", "agt_0000000000000002"])
    }

    func testGroupsChildrenUnderParentsInTheMostUrgentSection() throws {
        let json = #"""
        [
          {"id":"root","status":"done","updatedAt":"2026-10-30T09:00:00Z"},
          {"id":"kid","parentSession":"root","status":"waiting","updatedAt":"2026-10-30T08:00:00Z"},
          {"id":"grandkid","parentSession":"kid","status":"done","updatedAt":"2026-10-30T07:00:00Z"},
          {"id":"orphan","parentSession":"gone","status":"done","updatedAt":"2026-10-30T10:00:00Z"},
          {"id":"loop-a","parentSession":"loop-b","status":"closed"},
          {"id":"loop-b","parentSession":"loop-a","status":"closed"}
        ]
        """#
        let groups = AgentSession.grouped(try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(json.utf8)))
        XCTAssertEqual(groups.map(\.group), [.needsInput, .idle, .closed])
        XCTAssertEqual(groups[0].sessions.map(\.id), ["root"])
        XCTAssertEqual(groups[0].sessions[0].children.map(\.id), ["kid"])
        XCTAssertEqual(groups[0].sessions[0].children[0].children.map(\.id), ["grandkid"])
        XCTAssertEqual(groups[1].sessions.map(\.id), ["orphan"])
        XCTAssertEqual(groups[2].sessions.map(\.id), ["loop-a"])
        XCTAssertEqual(groups[2].sessions[0].children.map(\.id), ["loop-b"])
    }

    func testClosedParentStaysClosedUnlessAChildIsActive() throws {
        let json = #"""
        [
          {"id":"archived","status":"closed"},
          {"id":"leftover","parentSession":"archived","status":"done"},
          {"id":"closing","status":"closed"},
          {"id":"busy","parentSession":"closing","status":"running"}
        ]
        """#
        let groups = AgentSession.grouped(try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(json.utf8)))
        XCTAssertEqual(groups.map(\.group), [.running, .closed])
        XCTAssertEqual(groups[0].sessions.map(\.id), ["closing"])
        XCTAssertEqual(groups[1].sessions.map(\.id), ["archived"])
    }

    func testParentWithActiveChildrenShowsAsWorking() throws {
        let json = #"""
        [
          {"id":"orchestrator","status":"done","childStatus":"running"},
          {"id":"blocked","status":"idle","childStatus":"waiting"},
          {"id":"finished","status":"done"}
        ]
        """#
        let sessions = try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(json.utf8))
        XCTAssertEqual(sessions.map(\.displayStatus), [.running, .waiting, .done])
        XCTAssertEqual(sessions.map(\.statusLabel), ["Waiting for child sessions", "Child session waiting", "Done"])
        XCTAssertEqual(sessions.map(\.status), [.done, .idle, .done])
    }

    func testDecodesUserImages() throws {
        let json = #"{"kind":"user","id":"prompt-1","turn":1,"text":"see","content":[{"type":"text","text":"see"},{"type":"image","data":"AAEC","mimeType":"image/jpeg"},{"type":"image","mimeType":"image/jpeg"}]}"#
        let entry = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.utf8))
        XCTAssertEqual(entry.content, .user(text: "see", fromAgent: false, images: [Data([0, 1, 2]), Data()]))
    }

    func testPromptTextNotesVideoFrames() {
        let video = ComposerAttachment.video(frames: [Data([1]), Data([2])], times: [1.5, 4.5], duration: 6)
        XCTAssertEqual(ComposerAttachment.promptText("why", [.photo(Data([0])), video]), "why\n\n[Video, 6.0 s — 2 frames in order at 1.5 s, 4.5 s]")
    }

    func testDecodesTranscriptFixture() throws {
        let transcript = try JSONDecoder.clubmate.decode(AgentTranscript.self, from: fixture("agents-transcript"))
        XCTAssertEqual(transcript.modes.map(\.id), ["default", "plan"])
        guard transcript.entries.count == 7,
              case .user(let prompt, false, []) = transcript.entries[0].content,
              case .thought = transcript.entries[1].content,
              case .agent(let markdown) = transcript.entries[2].content,
              case .plan(let plan) = transcript.entries[3].content,
              case .tool(let read) = transcript.entries[4].content,
              case .tool(let edit) = transcript.entries[5].content,
              case .permission(let permission) = transcript.entries[6].content
        else { return XCTFail("unexpected entries \(transcript.entries.map(\.id))") }
        XCTAssertEqual(prompt, "Refactor the cart total into its own function")
        XCTAssertEqual(Markdown.blocks(markdown).first, .heading(level: 2, text: "Plan"))
        XCTAssertEqual(plan.map(\.status), [.completed, .inProgress, .pending])
        XCTAssertEqual(read.displayTitle, "Read cart.go")
        XCTAssertEqual(edit.displayTitle, "Edit cart.go")
        XCTAssertEqual(edit.status, .completed)
        XCTAssertEqual(edit.diffs.first?.lines, [DiffLine(text: "func total(items []Item) int {", kind: .removed), DiffLine(text: "func cartTotal(items []Item) int {", kind: .added)])
        XCTAssertEqual(permission.id, "permission-3")
        XCTAssertEqual(permission.toolCall.command, "go test ./...")
        XCTAssertEqual(permission.options.map(\.optionId), ["allow", "allow_always", "deny"])
        XCTAssertNil(permission.outcome)
    }

    func testConfigOptionsFlattenGroupedChoices() throws {
        let json = #"""
        {"session":{"id":"s"},"advisor":"opus","configOptions":[
          {"id":"model","name":"Model","category":"model","type":"select","currentValue":"opus","options":[
            {"group":"claude","name":"Claude","options":[{"value":"opus","name":"Opus"},{"value":"sonnet"}]},
            {"value":"local","name":"Local"}
          ]},
          {"id":"mode","category":"mode","type":"select","currentValue":"plan"}
        ]}
        """#
        let transcript = try JSONDecoder.clubmate.decode(AgentTranscript.self, from: Data(json.utf8))
        XCTAssertEqual(transcript.advisor, "opus")
        XCTAssertEqual(transcript.configOptions.filter(\.pickable).map(\.id), ["model"])
        XCTAssertEqual(transcript.configOptions[0].currentValue, "opus")
        XCTAssertEqual(transcript.configOptions[0].choices.map(\.value), ["opus", "sonnet", "local"])
        XCTAssertEqual(transcript.configOptions[0].choices.map(\.label), ["Opus", "sonnet", "Local"])
    }

    func testUnknownEntryKindBecomesOther() throws {
        let transcript = try JSONDecoder.clubmate.decode(AgentTranscript.self, from: Data(#"{"session":{"id":"s","status":"brand-new"},"entries":[{"kind":"hologram","id":"h1","turn":1}]}"#.utf8))
        XCTAssertEqual(transcript.session.status, .other)
        XCTAssertEqual(transcript.entries.map(\.content), [.other("hologram")])
    }

    func testStoreRefetchesOnChangesForItsWorkspace() async throws {
        let runner = FakeRunner()
        let store = AgentsStore(workspace: "prj_00000000000000f1", runner: runner)
        store.handle(.event(BusEvent(seq: 1, topic: "agents.changed", payload: .object(["workspace": .string("other"), "session": .string("agt_0000000000000001")]))))
        store.handle(.event(BusEvent(seq: 2, topic: "agents.changed", payload: .object(["workspace": .string("prj_00000000000000f1"), "session": .string("agt_0000000000000001")]))))
        try await waitUntil { store.sessions.count == 3 }
        XCTAssertEqual(runner.calls.values, ["agents.sessions"])
    }

    func testCreateWithAttachmentsStartsIdleThenPromptsWithImages() async throws {
        let runner = CreateRunner()
        let store = AgentsStore(workspace: "w", runner: runner)
        let id = try await store.create("look", attachments: [.photo(Data([1]))])
        XCTAssertEqual(id, "s")
        XCTAssertEqual(runner.calls.map(\.id), ["agents.new", "agents.prompt", "agents.sessions"])
        XCTAssertEqual(runner.calls[0].args, .object([:]))
        XCTAssertEqual(runner.calls[1].args, .object([
            "session": .string("s"),
            "text": .string("look"),
            "attachments": .array([.object(["kind": .string("image"), "mimeType": .string("image/jpeg"), "data": .string("AQ==")])]),
        ]))
    }

    func testSetConfigSendsTheSettingThenRefreshes() async throws {
        let runner = CreateRunner()
        let store = AgentsStore(workspace: "w", runner: runner)
        try await store.setConfig("s", id: "afk", value: "on")
        XCTAssertEqual(runner.calls.map(\.id), ["agents.config", "agents.sessions"])
        XCTAssertEqual(runner.calls[0].args, .object(["session": .string("s"), "id": .string("afk"), "value": .string("on")]))
    }
}

@MainActor
final class CreateRunner: CommandRunner {
    private(set) var calls: [(id: String, args: JSONValue?)] = []

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        calls.append((id, args))
        switch id {
        case "agents.new": return .object(["session": .string("s")])
        case "agents.sessions": return .array([])
        default: return .null
        }
    }
}

@MainActor
final class FakeRunner: CommandRunner {
    let calls = Recorder<String>()

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        calls.add(id)
        let name = id == "agents.sessions" ? "agents-sessions" : "agents-transcript"
        return try JSONDecoder().decode(JSONValue.self, from: fixture(name))
    }
}

@MainActor
final class TranscriptRefreshTests: XCTestCase {
    func testChangeFetchesFromLastLoadedTurnAndMerges() async throws {
        let first = #"{"session":{"id":"s","status":"done"},"turns":[{"turn":1},{"turn":2}],"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"},{"kind":"user","id":"prompt-2","turn":2,"text":"two"},{"kind":"agent","id":"a2","turn":2,"text":"partial"}]}"#
        let second = #"{"session":{"id":"s","status":"running"},"turns":[{"turn":1},{"turn":2},{"turn":3}],"entries":[{"kind":"user","id":"prompt-2","turn":2,"text":"two"},{"kind":"agent","id":"a2","turn":2,"text":"full answer"},{"kind":"user","id":"prompt-3","turn":3,"text":"three"}]}"#
        let runner = ScriptedRunner(replies: [first, second])
        let store = AgentsStore(workspace: "w", runner: runner)
        store.watch("s")
        try await waitUntil { store.transcripts["s"]?.entries.count == 3 }
        store.handle(.event(BusEvent(seq: 1, topic: "agents.changed", payload: .object(["workspace": .string("w"), "session": .string("s")]))))
        try await waitUntil { store.transcripts["s"]?.entries.count == 4 }
        XCTAssertEqual(runner.args.first, .object(["session": .string("s"), "compact": .bool(true)]))
        XCTAssertEqual(runner.args.last, .object(["session": .string("s"), "compact": .bool(true), "fromTurn": .number(2)]))
        let transcript = try XCTUnwrap(store.transcripts["s"])
        XCTAssertEqual(transcript.entries.map(\.id), ["user/prompt-1", "user/prompt-2", "agent/a2", "user/prompt-3"])
        XCTAssertEqual(transcript.entries[2].content, .agent("full answer"))
        XCTAssertEqual(transcript.session.status, .running)
        store.unwatch("s")
    }

    func testSentPromptShowsUntilTheTranscriptRecordsIt() async throws {
        let before = #"{"session":{"id":"s","status":"done"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"}]}"#
        let started = #"{"session":{"id":"s","status":"running"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"}]}"#
        let recorded = #"{"session":{"id":"s","status":"running"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"},{"kind":"user","id":"prompt-2","turn":2,"text":"one"}]}"#
        let runner = ScriptedRunner(replies: [before, started, recorded])
        let store = AgentsStore(workspace: "w", runner: runner)
        store.watch("s")
        try await waitUntil { store.transcripts["s"] != nil }
        try await store.send("one", to: "s")
        XCTAssertEqual(store.entries("s").map(\.isOutgoing), [false, true])
        XCTAssertEqual(store.entries("s").last?.content, .user(text: "one", fromAgent: false, images: []))
        store.handle(.event(BusEvent(seq: 1, topic: "agents.changed", payload: .object(["workspace": .string("w"), "session": .string("s")]))))
        try await waitUntil { store.transcripts["s"]?.entries.count == 2 }
        XCTAssertEqual(store.entries("s").map(\.id), ["user/prompt-1", "user/prompt-2"])
        store.unwatch("s")
    }

    func testWatchRetriesAFailedLoadOnceThenShowsTheErrorUntilALoadSucceeds() async throws {
        let runner = ScriptedRunner(replies: ["", "", "", #"{"session":{"id":"s","status":"done"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"}]}"#])
        let store = AgentsStore(workspace: "w", runner: runner)
        store.watch("s")
        try await waitUntil { store.transcriptErrors["s"] != nil }
        XCTAssertEqual(runner.args.count, 2)
        XCTAssertNil(store.transcripts["s"])
        await store.loadTranscript("s")
        XCTAssertEqual(store.transcripts["s"]?.entries.count, 1)
        XCTAssertNil(store.transcriptErrors["s"])
        store.unwatch("s")
    }

    func testReopeningASessionWhileItsLoadIsInFlightStillLoadsIt() async throws {
        let runner = SlowRunner()
        let store = AgentsStore(workspace: "w", runner: runner)
        store.watch("s")
        try await waitUntil { runner.started == 1 }
        store.unwatch("s")
        store.watch("s")
        try await waitUntil { store.transcripts["s"] != nil }
        store.unwatch("s")
    }

    func testFollowsARunningSessionUntilItsTurnEnds() async throws {
        let running = #"{"session":{"id":"s","status":"running"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"}]}"#
        let done = #"{"session":{"id":"s","status":"done"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"},{"kind":"agent","id":"a1","turn":1,"text":"answer"}]}"#
        let runner = ScriptedRunner(replies: [running, done], sessions: [#"[{"id":"s","status":"running"}]"#, #"[{"id":"s","status":"done"}]"#])
        let store = AgentsStore(workspace: "w", runner: runner, sleep: { _ in await Task.yield() })
        await store.refresh()
        store.watch("s")
        try await waitUntil { store.sessions.first?.status == .done }
        XCTAssertEqual(store.transcripts["s"]?.entries.map(\.id), ["user/prompt-1", "agent/a1"])
        XCTAssertEqual(runner.args, [.object(["session": .string("s"), "compact": .bool(true)]), .object(["session": .string("s"), "compact": .bool(true), "fromTurn": .number(1)])])
        store.unwatch("s")
    }
}

@MainActor
final class ScriptedRunner: CommandRunner {
    private var replies: [String]
    private var sessions: [String]
    private(set) var args: [JSONValue] = []

    init(replies: [String], sessions: [String] = []) {
        self.replies = replies
        self.sessions = sessions
    }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        guard id == "agents.transcript" else {
            return sessions.isEmpty ? .array([]) : try JSONDecoder().decode(JSONValue.self, from: Data(sessions.removeFirst().utf8))
        }
        self.args.append(args ?? .null)
        return try JSONDecoder().decode(JSONValue.self, from: Data(replies.removeFirst().utf8))
    }
}

@MainActor
final class SlowRunner: CommandRunner {
    private(set) var started = 0

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        guard id == "agents.transcript" else { return .array([]) }
        started += 1
        try await Task.sleep(for: .milliseconds(50))
        return try JSONDecoder().decode(JSONValue.self, from: Data(#"{"session":{"id":"s","status":"done"},"entries":[]}"#.utf8))
    }
}
