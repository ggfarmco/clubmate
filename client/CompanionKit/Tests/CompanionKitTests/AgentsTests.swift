import Foundation
import XCTest

import CompanionKit

@MainActor
final class AgentsTests: XCTestCase {
    func testDecodesSessionsFixtureIntoGroups() throws {
        let sessions = try JSONDecoder.companion.decode([AgentSession].self, from: fixture("agents-sessions"))
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
          {"id":"loop-a","parentSession":"loop-b","status":"closed","closed":true},
          {"id":"loop-b","parentSession":"loop-a","status":"closed","closed":true}
        ]
        """#
        let groups = AgentSession.grouped(try JSONDecoder.companion.decode([AgentSession].self, from: Data(json.utf8)))
        XCTAssertEqual(groups.map(\.group), [.needsInput, .idle, .closed])
        XCTAssertEqual(groups[0].sessions.map(\.id), ["root"])
        XCTAssertEqual(groups[0].sessions[0].children.map(\.id), ["kid"])
        XCTAssertEqual(groups[0].sessions[0].children[0].children.map(\.id), ["grandkid"])
        XCTAssertEqual(groups[1].sessions.map(\.id), ["orphan"])
        XCTAssertEqual(groups[2].sessions.map(\.id), ["loop-a"])
        XCTAssertEqual(groups[2].sessions[0].children.map(\.id), ["loop-b"])
    }

    func testDecodesUserImages() throws {
        let json = #"{"kind":"user","id":"prompt-1","turn":1,"text":"see","content":[{"type":"text","text":"see"},{"type":"image","data":"AAEC","mimeType":"image/jpeg"}]}"#
        let entry = try JSONDecoder.companion.decode(TranscriptEntry.self, from: Data(json.utf8))
        XCTAssertEqual(entry.content, .user(text: "see", fromAgent: false, images: [Data([0, 1, 2])]))
    }

    func testPromptTextNotesVideoFrames() {
        let video = ComposerAttachment.video(frames: [Data([1]), Data([2])], times: [1.5, 4.5], duration: 6)
        XCTAssertEqual(ComposerAttachment.promptText("why", [.photo(Data([0])), video]), "why\n\n[Video, 6.0 s — 2 frames in order at 1.5 s, 4.5 s]")
    }

    func testDecodesTranscriptFixture() throws {
        let transcript = try JSONDecoder.companion.decode(AgentTranscript.self, from: fixture("agents-transcript"))
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
        XCTAssertEqual(permission.options.map(\.optionId), ["allow_always", "allow", "deny"])
        XCTAssertNil(permission.outcome)
    }

    func testUnknownEntryKindBecomesOther() throws {
        let transcript = try JSONDecoder.companion.decode(AgentTranscript.self, from: Data(#"{"session":{"id":"s","status":"brand-new"},"entries":[{"kind":"hologram","id":"h1","turn":1}]}"#.utf8))
        XCTAssertEqual(transcript.session.status, .other)
        XCTAssertEqual(transcript.entries.map(\.content), [.other("hologram")])
    }

    func testStoreRefetchesOnChangesForItsWorkspace() async throws {
        let runner = FakeRunner()
        let store = AgentsStore(workspace: "prj_00000000000000f1", runner: runner)
        store.watch("agt_0000000000000001")
        store.handle(.event(BusEvent(seq: 1, topic: "agents.changed", payload: .object(["workspace": .string("other"), "session": .string("agt_0000000000000001")]))))
        store.handle(.event(BusEvent(seq: 2, topic: "agents.changed", payload: .object(["workspace": .string("prj_00000000000000f1"), "session": .string("agt_0000000000000001")]))))
        try await waitUntil { store.transcripts.count == 1 && store.sessions.count == 3 }
        XCTAssertEqual(runner.calls.values.sorted(), ["agents.sessions", "agents.transcript"])
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
        await store.loadTranscript("s")
        store.handle(.event(BusEvent(seq: 1, topic: "agents.changed", payload: .object(["workspace": .string("w"), "session": .string("s")]))))
        try await waitUntil { store.transcripts["s"]?.entries.count == 4 }
        XCTAssertEqual(runner.args.first, .object(["session": .string("s")]))
        XCTAssertEqual(runner.args.last, .object(["session": .string("s"), "fromTurn": .number(2)]))
        let transcript = try XCTUnwrap(store.transcripts["s"])
        XCTAssertEqual(transcript.entries.map(\.id), ["user/prompt-1", "user/prompt-2", "agent/a2", "user/prompt-3"])
        XCTAssertEqual(transcript.entries[2].content, .agent("full answer"))
        XCTAssertEqual(transcript.session.status, .running)
    }
}

@MainActor
final class ScriptedRunner: CommandRunner {
    private var replies: [String]
    private(set) var args: [JSONValue] = []

    init(replies: [String]) {
        self.replies = replies
    }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        guard id == "agents.transcript" else { return .array([]) }
        self.args.append(args ?? .null)
        return try JSONDecoder().decode(JSONValue.self, from: Data(replies.removeFirst().utf8))
    }
}
