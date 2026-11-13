import Foundation
import XCTest

import CompanionKit

@MainActor
final class AgentsTests: XCTestCase {
    func testDecodesSessionsFixtureIntoGroups() throws {
        let sessions = try JSONDecoder.clubmate.decode([AgentSession].self, from: fixture("agents-sessions"))
        XCTAssertEqual(sessions.map(\.status), [.waiting, .done, .closed])
        XCTAssertEqual(sessions[0].pending, ["permission-3"])
        XCTAssertEqual(sessions[0].pendingSummary, PendingSummary(kind: .permission, text: "go test ./...", tool: "execute", at: Date.parseRFC3339("2026-10-30T09:41:00Z")!))
        XCTAssertEqual(sessions[1].pendingSummary?.kind, .question)
        XCTAssertEqual(sessions[1].question, "Should I add retries to the charge step?")
        XCTAssertEqual(sessions[0].modeLabel, "Ask before edits")
        XCTAssertEqual(sessions.map(\.reopen?.command), [nil, nil, "agents.handoff"])
        XCTAssertEqual(sessions[0].createdAt, Date.parseRFC3339("2026-10-30T09:41:00Z"))
        XCTAssertEqual(AgentSession.grouped(sessions).map { $0.group }, [.needsInput, .closed])
        XCTAssertEqual(AgentSession.grouped(sessions)[0].sessions.map(\.id).sorted(), ["agt_0000000000000001", "agt_0000000000000002"])
    }

    func testDecodesTheReviewProgressText() throws {
        let json = #"[{"id":"a","status":"done","reviewProgress":{"id":"rv_1","reviewed":5,"total":15,"open":1,"sent":0,"text":"review 5/15 · 1"}}]"#
        let sessions = try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(json.utf8))
        XCTAssertEqual(sessions[0].reviewProgress, ReviewProgress(text: "review 5/15 · 1"))
    }

    func testOpenSectionSitsAboveRunningOldestFirstAndCountsItsTrees() throws {
        let json = #"""
        [
          {"id":"run","status":"running","group":"running"},
          {"id":"new","status":"done","group":"open","openCount":2,"openSince":"2026-11-04T10:00:00Z"},
          {"id":"old","status":"done","group":"open","openCount":3,"openSince":"2026-11-03T10:00:00Z","mayAsk":true},
          {"id":"ask","status":"done","needsYou":true,"group":"needsYou"}
        ]
        """#
        let groups = AgentSession.grouped(try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(json.utf8)))
        XCTAssertEqual(groups.map(\.group), [.needsInput, .open, .running])
        XCTAssertEqual(groups[1].sessions.map(\.id), ["old", "new"])
        XCTAssertEqual(groups[1].sessions.map(\.openInTree), [3, 2])
    }

    func testGroupsTreesByThePluginsGroupOfTheirRoot() throws {
        let json = #"""
        [
          {"id":"root","status":"done","group":"needsYou","updatedAt":"2026-10-30T09:00:00Z"},
          {"id":"kid","parentSession":"root","status":"waiting","group":"needsYou","updatedAt":"2026-10-30T08:00:00Z"},
          {"id":"grandkid","parentSession":"kid","status":"done","group":"done","updatedAt":"2026-10-30T07:00:00Z"},
          {"id":"orphan","parentSession":"gone","status":"done","group":"done","updatedAt":"2026-10-30T10:00:00Z"},
          {"id":"voice","status":"running","service":true,"group":"service"},
          {"id":"loop-a","parentSession":"loop-b","status":"closed","group":"closed"},
          {"id":"loop-b","parentSession":"loop-a","status":"closed","group":"closed"}
        ]
        """#
        let groups = AgentSession.grouped(try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(json.utf8)))
        XCTAssertEqual(groups.map(\.group), [.needsInput, .idle, .closed, .service])
        XCTAssertEqual(groups[0].sessions.map(\.id), ["root"])
        XCTAssertEqual(groups[0].sessions[0].children.map(\.id), ["kid"])
        XCTAssertEqual(groups[0].sessions[0].children[0].children.map(\.id), ["grandkid"])
        XCTAssertEqual(groups[1].sessions.map(\.id), ["orphan"])
        XCTAssertEqual(groups[2].sessions.map(\.id), ["loop-a"])
        XCTAssertEqual(groups[2].sessions[0].children.map(\.id), ["loop-b"])
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

    func testDecodesUserImagesAndFiles() throws {
        let json = #"{"kind":"user","id":"prompt-1","turn":1,"text":"see","content":[{"type":"text","text":"see"},{"type":"image","data":"AAEC","mimeType":"image/jpeg"},{"type":"image","mimeType":"image/jpeg"},{"type":"resource_link","uri":"file:///u/logs.zip","name":"logs.zip"},{"type":"resource","resource":{"uri":"file:///r/a.go","text":"x"}},{"type":"resource","resource":{"uri":"file:///r/b.go#L1-2","text":"y"}}]}"#
        let entry = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.utf8))
        XCTAssertEqual(entry.content, .user(text: "see", fromAgent: false, images: [Data([0, 1, 2]), Data()], files: ["logs.zip", "a.go"]))
    }

    func testDecodesVoicePromptAsHeardWithPrettyJSON() throws {
        let json = #"{"kind":"user","id":"prompt-2","turn":2,"from":"voice","text":"{\"heard\":\"удали ворктри\",\"context\":{\"path\":\"a/b\"}}","voice":{"heard":"удали ворктри","utterance":"vu_1"}}"#
        let entry = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.utf8))
        guard case .voice(let prompt) = entry.content else { return XCTFail("unexpected \(entry.content)") }
        XCTAssertEqual(prompt.heard, "удали ворктри")
        XCTAssertEqual(prompt.utterance, "vu_1")
        XCTAssertEqual(prompt.json, "{\n  \"context\" : {\n    \"path\" : \"a/b\"\n  },\n  \"heard\" : \"удали ворктри\"\n}")
    }

    func testPromptTextNotesVideoFrames() {
        let video = ComposerAttachment.video(frames: [Data([1]), Data([2])], times: [1.5, 4.5], duration: 6)
        XCTAssertEqual(ComposerAttachment.promptText("why", [.photo(Data([0])), video]), "why\n\n[Video, 6.0 s — 2 frames in order at 1.5 s, 4.5 s]")
    }

    func testDecodesTranscriptFixture() throws {
        let transcript = try JSONDecoder.clubmate.decode(AgentTranscript.self, from: fixture("agents-transcript"))
        XCTAssertEqual(transcript.modes.map(\.id), ["default", "plan"])
        guard transcript.entries.count == 10,
              case .user(let prompt, false, [], []) = transcript.entries[0].content,
              case .thought = transcript.entries[1].content,
              case .agent(let markdown) = transcript.entries[2].content,
              case .plan(let plan) = transcript.entries[3].content,
              case .tool(let read) = transcript.entries[4].content,
              case .tool(let edit) = transcript.entries[5].content,
              case .tool(let write) = transcript.entries[6].content,
              case .compaction(let compaction) = transcript.entries[8].content,
              case .permission(let permission) = transcript.entries[9].content
        else { return XCTFail("unexpected entries \(transcript.entries.map(\.id))") }
        XCTAssertEqual(compaction.title, "Context compacted")
        XCTAssertFalse(compaction.summary.isEmpty)
        XCTAssertEqual(transcript.usage?.contextLabel, "42% context")
        XCTAssertEqual(transcript.usage?.tokensLabel, "84k of 200k tokens")
        XCTAssertEqual(transcript.usage?.costLabel, "$0.84")
        XCTAssertEqual(transcript.configOptions.filter(\.isToggle).map(\.currentValue), ["true"])
        XCTAssertEqual(prompt, "Refactor the cart total into its own function")
        XCTAssertEqual(Markdown.blocks(markdown).first, .heading(level: 2, text: "Plan"))
        XCTAssertEqual(plan.map(\.status), [.completed, .inProgress, .pending])
        XCTAssertEqual(read.displayTitle, "Read cart.go")
        XCTAssertEqual(edit.displayTitle, "Edit cart.go")
        XCTAssertEqual(edit.status, .completed)
        XCTAssertNil(edit.doc)
        XCTAssertEqual(write.doc, "/Users/me/src/shop/docs/checkout-retries.md")
        XCTAssertEqual(edit.diffs.first?.lines, [DiffLine(text: "func total(items []Item) int {", kind: .removed), DiffLine(text: "func cartTotal(items []Item) int {", kind: .added)])
        XCTAssertEqual(permission.id, "permission-3")
        XCTAssertEqual(permission.toolCall.command, "go test ./...")
        XCTAssertEqual(permission.options.map(\.optionId), ["allow", "allow_always", "deny", "deny_always"])
        XCTAssertNil(permission.outcome)
        XCTAssertEqual(permission.card?.title, "Run a command?")
        XCTAssertEqual(permission.card?.caption, "Claude says: Run the shop tests")
        XCTAssertEqual(permission.card?.buttons.map(\.label), ["Allow once", "Always allow…", "Reject"])
        XCTAssertEqual(permission.card?.menu.map(\.id), ["deny_always"])
        XCTAssertEqual([permission.alwaysConfirm("allow_always")?.rule, permission.alwaysConfirm("allow_always")?.workspace], ["Bash(go test:*)", "demo"])
        XCTAssertEqual((1...4).map { permission.keyed($0)?.id }, ["allow", "allow_always", "deny", nil])
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

    func testElicitationEntrySummarisesItsAnswer() throws {
        let json = #"{"kind":"elicitation","id":"elicitation-3","turn":1,"elicitation":{"mode":"form","message":"Which store?","fields":[{"key":"question_0","type":"choice","options":[{"value":"pg","label":"Postgres"},{"value":"redis","label":"Redis"}]},{"key":"question_0_custom","type":"text","title":"Other"}]},"response":{"action":"accept","content":{"question_0":"pg","question_0_custom":"HA"}}}"#
        let entry = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.utf8))
        guard case .elicitation(let question) = entry.content else { return XCTFail("\(entry.content)") }
        XCTAssertEqual(question.singleChoice?.key, "question_0")
        XCTAssertEqual(question.answer, "Postgres · HA")
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

    func testRefreshAfterTheFirstLoadMergesOnlyWhatChanged() async throws {
        let runner = ScriptedRunner(replies: [], sessions: [
            #"{"sessions":[{"id":"a","status":"done"},{"id":"b","status":"done"},{"id":"c","status":"done"}],"removed":[],"cursor":"e.3"}"#,
            #"{"sessions":[{"id":"b","status":"running"},{"id":"d","status":"idle"}],"removed":["c"],"cursor":"e.5","delta":true}"#,
        ])
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.refresh()
        await store.refresh()
        XCTAssertEqual(runner.sessionArgs, [.object(["since": .string("")]), .object(["since": .string("e.3")])])
        XCTAssertEqual(store.sessions.map(\.id), ["a", "b", "d"])
        XCTAssertEqual(store.sessions[1].status, .running)
    }

    func testACancelledCallerDoesNotLoseTheSessions() async throws {
        let runner = SlowSessionsRunner()
        let store = AgentsStore(workspace: "prj_00000000000000f1", runner: runner)
        let caller = Task { await store.refresh() }
        try await Task.sleep(for: .milliseconds(10))
        caller.cancel()
        try await waitUntil { store.loaded }
        XCTAssertEqual(store.sessions.count, 3)
        XCTAssertNil(store.error)
    }

    func testCreateWithAttachmentsStartsIdleThenPromptsWithImagesAndFiles() async throws {
        let runner = CreateRunner()
        let store = AgentsStore(workspace: "w", runner: runner)
        let id = try await store.create("look", attachments: [.photo(Data([1])), .file(name: "logs.zip", data: Data([2]))])
        XCTAssertEqual(id, "s")
        XCTAssertEqual(runner.calls.map(\.id), ["agents.new", "agents.prompt", "agents.sessions"])
        XCTAssertEqual(runner.calls[0].args, .object([:]))
        XCTAssertEqual(runner.calls[1].args, .object([
            "session": .string("s"),
            "text": .string("look"),
            "attachments": .array([
                .object(["kind": .string("image"), "mimeType": .string("image/jpeg"), "data": .string("AQ==")]),
                .object(["kind": .string("file"), "name": .string("logs.zip"), "data": .string("Ag==")]),
            ]),
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
final class SlowSessionsRunner: CommandRunner {
    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        try await Task.sleep(for: .milliseconds(100))
        return try JSONDecoder().decode(JSONValue.self, from: fixture("agents-sessions"))
    }
}

@MainActor
final class ImageRunner: CommandRunner {
    var calls = 0
    var data: String?

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        if id == AgentCommand.sessions { return .array([]) }
        if id == AgentCommand.transcript {
            return try JSONDecoder().decode(JSONValue.self, from: Data(#"{"session":{"id":"s","status":"done"},"entries":[]}"#.utf8))
        }
        calls += 1
        guard let data else { throw RemoteError.server(code: "error", message: "no such image") }
        return .object(["mimeType": .string("image/png"), "data": .string(data)])
    }
}

@MainActor
final class TranscriptImageTests: XCTestCase {
    func testImageLoadsOncePerMessageAndRetriesAfterAMiss() async {
        let runner = ImageRunner()
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.loadImage("/w/a.png", entry: "e", in: "s")
        XCTAssertEqual(store.image("/w/a.png", entry: "e", in: "s"), .missing)
        runner.data = Data("png".utf8).base64EncodedString()
        await store.loadImage("/w/a.png", entry: "e", in: "s")
        await store.loadImage("/w/a.png", entry: "e", in: "s")
        XCTAssertEqual(store.image("/w/a.png", entry: "e", in: "s"), .loaded(Data("png".utf8)))
        XCTAssertEqual(runner.calls, 2)
        await store.loadImage("/w/a.png", entry: "later", in: "s")
        XCTAssertEqual(runner.calls, 3)
    }

    func testClosingASessionDropsItsImagesAndTranscript() async throws {
        let runner = ImageRunner()
        runner.data = Data("png".utf8).base64EncodedString()
        let store = AgentsStore(workspace: "w", runner: runner)
        store.watch("s")
        try await waitUntil { store.transcripts["s"] != nil }
        await store.loadImage("/w/a.png", entry: "e", in: "s")
        await store.loadImage("/w/a.png", entry: "e", in: "t")

        store.unwatch("s")

        XCTAssertNil(store.transcripts["s"])
        XCTAssertNil(store.image("/w/a.png", entry: "e", in: "s"))
        XCTAssertEqual(store.image("/w/a.png", entry: "e", in: "t"), .loaded(Data("png".utf8)))
    }

    func testOnlyTheNewestImagesStayLoaded() async {
        let runner = ImageRunner()
        runner.data = Data("png".utf8).base64EncodedString()
        let store = AgentsStore(workspace: "w", runner: runner)
        for index in 0...AgentsStore.imageLimit {
            await store.loadImage("/w/\(index).png", entry: "e", in: "s")
        }
        XCTAssertNil(store.image("/w/0.png", entry: "e", in: "s"))
        XCTAssertEqual(store.image("/w/1.png", entry: "e", in: "s"), .loaded(Data("png".utf8)))
        XCTAssertEqual(store.image("/w/\(AgentsStore.imageLimit).png", entry: "e", in: "s"), .loaded(Data("png".utf8)))
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

    func testOpenedBannerOnlyForShownSession() {
        let store = AgentsStore(workspace: "w", runner: SlowRunner())
        store.watch("shown")
        let opened = { (session: String) in
            BusEvent(seq: 1, topic: "docs.opened", payload: .object(["workspace": .string("w"), "session": .string(session), "path": .string("/r/\(session).md")]))
        }
        store.handle(.event(opened("other")))
        store.handle(.event(opened("shown")))
        XCTAssertEqual(store.openedDocs, ["shown": "/r/shown.md"])
        store.dismissOpenedDoc("shown")
        XCTAssertEqual(store.openedDocs, [:])
        store.unwatch("shown")
    }

    func testShownDetailIsTheOnlyWatchedSession() {
        let store = AgentsStore(workspace: "w", runner: SlowRunner())
        let opened = { (session: String) in
            BusEvent(seq: 1, topic: "docs.opened", payload: .object(["workspace": .string("w"), "session": .string(session), "path": .string("/r/\(session).md")]))
        }
        store.show("a")
        store.show("a")
        store.show("b")
        store.handle(.event(opened("a")))
        store.handle(.event(opened("b")))
        XCTAssertEqual(store.openedDocs, ["b": "/r/b.md"])
        store.dismissOpenedDoc("b")
        store.show(AgentsPluginInfo.openQueue)
        store.handle(.event(opened("b")))
        XCTAssertEqual(store.openedDocs, [:])
    }

    func testChangeSendsTheCursorAndMergesTheDeltaByEntry() async throws {
        let first = #"{"session":{"id":"s","status":"running"},"cursor":"e.2","entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"},{"kind":"agent","id":"a1","turn":1,"text":"part"}]}"#
        let delta = #"{"session":{"id":"s","status":"running"},"cursor":"e.4","delta":true,"entries":[{"kind":"agent","id":"a1","turn":1,"text":"partial answer"},{"kind":"tool","id":"t1","turn":1}]}"#
        let runner = ScriptedRunner(replies: [first, delta])
        let store = AgentsStore(workspace: "w", runner: runner)
        store.watch("s")
        try await waitUntil { store.transcripts["s"] != nil }
        store.handle(.event(BusEvent(seq: 1, topic: "agents.changed", payload: .object(["workspace": .string("w"), "session": .string("s")]))))
        try await waitUntil { store.transcripts["s"]?.cursor == "e.4" }
        XCTAssertEqual(runner.args.last, .object(["session": .string("s"), "compact": .bool(true), "fromTurn": .number(1), "since": .string("e.2")]))
        let transcript = try XCTUnwrap(store.transcripts["s"])
        XCTAssertEqual(transcript.entries.map(\.id), ["user/prompt-1", "agent/a1", "tool/t1"])
        XCTAssertEqual(transcript.entries[1].content, .agent("partial answer"))
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
final class PermissionAnswerTests: XCTestCase {
    func testAnswerToARequestNoLongerPendingIsNotSent() async throws {
        let runner = AnswerRunner(pending: [], failure: nil)
        let store = AgentsStore(workspace: "w", runner: runner)
        await XCTAssertThrowsAsync(try await store.answer("s", request: "permission-1", option: "a"), AnswerError.alreadyAnswered)
        XCTAssertFalse(runner.commands.contains("agents.permission"))
    }

    func testAnswerChecksOnlyThatSessionAndKeepsTheRestOfTheList() async throws {
        let runner = AnswerRunner(pending: ["permission-1"], failure: nil)
        let store = AgentsStore(workspace: "w", runner: runner)
        runner.store = store
        await store.refresh()
        try await store.answer("s", request: "permission-1", option: "a")
        XCTAssertEqual(runner.sessionArgs[1], .object(["session": .string("s")]))
        XCTAssertEqual(runner.listedAtPermission, ["s", "other"])
    }

    func testRequestTheMacNoLongerKnowsReadsAsAlreadyAnswered() async throws {
        let runner = AnswerRunner(pending: ["permission-1"], failure: RemoteError.server(code: "internal", message: "agents.permission: unknown permission request"))
        let store = AgentsStore(workspace: "w", runner: runner)
        await XCTAssertThrowsAsync(try await store.answer("s", request: "permission-1", option: "a"), AnswerError.alreadyAnswered)
    }
}

@MainActor
final class ElicitationDraftTests: XCTestCase {
    func testDraftOutlivesTheCardUntilTheAnswerIsSent() async throws {
        let store = AgentsStore(workspace: "w", runner: ScriptedRunner(replies: []))
        var draft = ElicitationDraft()
        draft.picks["stores"] = ["pg", "redis"]
        draft.texts["note"] = "HA"
        store.setElicitationDraft(draft, session: "s", request: "elicitation-1")
        XCTAssertEqual(store.elicitationDraft("s", request: "elicitation-1"), draft)
        try await store.respond("s", request: "elicitation-1", action: "accept", answers: draft.answers([]))
        XCTAssertEqual(store.elicitationDraft("s", request: "elicitation-1"), ElicitationDraft())
    }

    func testSendWaitsForAnAnswerOnlyWhenThePluginSaysSo() throws {
        let json = #"{"kind":"elicitation","id":"elicitation-1","turn":1,"elicitation":{"mode":"form","needsAnswer":true,"fields":[{"key":"question_0","type":"multi","options":[{"value":"a","label":"A"}]},{"key":"question_0_custom","type":"text","title":"Other"}]}}"#
        let entry = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.utf8))
        guard case .elicitation(let question) = entry.content else { return XCTFail("\(entry.content)") }
        let fields = question.request.fields
        var draft = ElicitationDraft()
        XCTAssertFalse(question.canSend(draft))
        draft.texts["question_0_custom"] = "closed row"
        XCTAssertFalse(question.canSend(draft))
        draft.toggleOther(fields[1])
        XCTAssertTrue(question.canSend(draft))
        draft = ElicitationDraft()
        draft.toggle(fields[0], fields[0].options[0])
        XCTAssertTrue(question.canSend(draft))

        let older = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.replacingOccurrences(of: #""needsAnswer":true,"#, with: "").utf8))
        guard case .elicitation(let oldQuestion) = older.content else { return XCTFail("\(older.content)") }
        XCTAssertTrue(oldQuestion.canSend(ElicitationDraft()))
    }

    func testPickedOptionCarriesTheOtherNote() throws {
        let json = #"{"mode":"form","message":"Which store?","fields":[{"key":"question_0","type":"choice","options":[{"value":"pg","label":"Postgres"},{"value":"redis","label":"Redis"}]},{"key":"question_0_custom","type":"text","title":"Other"}]}"#
        let request = try JSONDecoder.clubmate.decode(Elicitation.Request.self, from: Data(json.utf8))
        var draft = ElicitationDraft()
        draft.toggleOther(request.fields[1])
        draft.texts["question_0_custom"] = "only as a cache"
        let answers = draft.answers(request.fields, picking: request.fields[0].options[1], in: request.fields[0])
        XCTAssertEqual(answers, [Elicitation.Answer(key: "question_0", value: "redis"), Elicitation.Answer(key: "question_0_custom", value: "only as a cache")])
    }

    func testEachQuestionTakesItsOtherAndAFreeFieldStandsAlone() throws {
        let json = #"{"kind":"elicitation","id":"elicitation-1","turn":1,"elicitation":{"mode":"form","fields":[{"key":"question_0","type":"choice","options":[{"value":"a","label":"A"}]},{"key":"question_0_custom","type":"text","title":"Other"},{"key":"question_1","type":"multi","options":[{"value":"b","label":"B"}]},{"key":"question_1_custom","type":"text","title":"Other"},{"key":"name","type":"text"}]}}"#
        let entry = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(json.utf8))
        guard case .elicitation(let question) = entry.content else { return XCTFail("\(entry.content)") }
        XCTAssertEqual(question.questions.map(\.field.key), ["question_0", "question_1", "name"])
        XCTAssertEqual(question.questions.map(\.other?.key), ["question_0_custom", "question_1_custom", nil])
        XCTAssertNil(question.singleChoice)
    }

    func testClosedOtherKeepsItsTextButSendsNothing() throws {
        let json = #"{"mode":"form","fields":[{"key":"question_0","type":"choice","options":[{"value":"a","label":"A"}]},{"key":"question_0_custom","type":"text","title":"Other"}]}"#
        let request = try JSONDecoder.clubmate.decode(Elicitation.Request.self, from: Data(json.utf8))
        var draft = ElicitationDraft()
        draft.toggleOther(request.fields[1])
        draft.texts["question_0_custom"] = "neither"
        XCTAssertEqual(draft.answers(request.fields), [Elicitation.Answer(key: "question_0_custom", value: "neither")])
        draft.toggleOther(request.fields[1])
        XCTAssertEqual(draft.answers(request.fields), [])
        XCTAssertEqual(draft.texts["question_0_custom"], "neither")
    }

    private static let openEntry = #"{"kind":"openItems","id":"open-1","turn":3,"later":true,"overflow":[211],"items":[{"id":"oi-1","kind":"decision","header":"Lists","question":"How?","quote":"Q","options":[{"label":"All","description":"Every list"},{"label":"Prop","description":"Only here"}],"state":"answered","answer":"All"},{"id":"oi-2","kind":"decision","header":"Tags","question":"Which?","quote":"Q","multiSelect":true,"options":[{"label":"A","description":"a"},{"label":"B","description":"b"}],"state":"open","at":"2026-11-04T10:00:00.123456Z"},{"id":"oi-3","kind":"offer","label":"Add a screen","quote":"Q","state":"open"},{"id":"oi-4","kind":"offer","label":"Close it","quote":"Q","state":"open"}]}"#

    private func openItems() throws -> OpenItems {
        guard case .openItems(let items) = try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: Data(Self.openEntry.utf8)).content else { throw RemoteError.badResponse }
        return items
    }

    func testDecodesWhatThePluginSendsForOpenItems() throws {
        let wire = try JSONDecoder.clubmate.decode(OpenItemsWire.self, from: fixture("agents-open-items"))
        let session = try XCTUnwrap(wire.sessions.first)
        XCTAssertEqual(session.group, .open)
        XCTAssertEqual(session.openCount, 3)
        XCTAssertNotNil(session.openSince)
        XCTAssertEqual(session.glyph, .open)
        guard case .open(let card) = SessionScreen(entries: wire.transcript.entries, session: session).dock else { return XCTFail("no card") }
        XCTAssertEqual(card.items[0].options.map(\.description), ["First", "Second"])
        XCTAssertTrue(card.items[0].multiSelect)
        XCTAssertEqual(wire.queue.map(\.session), [session.id])
        XCTAssertEqual(session.stateLine(now: .now).text, "Done · 2 decisions, 1 offer")
    }

    func testOpenCardKeepsTheReportsNumbersAndFoldsOffersIntoOneSection() throws {
        let items = try openItems()
        let card = try XCTUnwrap(OpenCard([items]))
        XCTAssertEqual(card.sections.map(\.number), [2, 3])
        XCTAssertEqual(card.sections.map(\.folded), ["Which?", "2 offers"])
        XCTAssertTrue(card.later)
        XCTAssertEqual(card.overflowLine, "+1 more went to the backlog: #211")
        XCTAssertEqual(items.items[1].at, Date.parseRFC3339("2026-11-04T10:00:00.123Z"))
    }

    func testAnsweringOpenItemsSendsPicksOtherAndOffersAndForgetsThem() async throws {
        let runner = ScriptedRunner(replies: [#"{"session":{"id":"s","status":"running"},"entries":[]}"#])
        let store = AgentsStore(workspace: "w", runner: runner)
        let items = try openItems().open
        var draft = OpenDraft()
        draft.pick(items[0].options[1], in: items[0])
        draft.pick(items[0].options[0], in: items[0])
        draft.toggleOther(items[0])
        draft.others["oi-2"] = " Both "
        draft.toggleOffer(items[2])
        store.setOpenDraft(draft, session: "s")
        try await store.answerOpen("s", items: items, note: "Go on")
        XCTAssertEqual(runner.sessionArgs.first, .object([
            "session": .string("s"),
            "answers": .array([.object(["item": .string("oi-2"), "options": .array([.string("A"), .string("B")]), "other": .string("Both")])]),
            "offers": .array([.string("oi-4")]),
            "note": .string("Go on"),
        ]))
        XCTAssertTrue(store.openDraft("s").isEmpty)
    }

    func testNoToAllClosesTheItemsOnceTheUndoWindowEnds() async throws {
        let runner = ScriptedRunner(replies: [#"{"session":{"id":"s","status":"done"},"entries":[]}"#])
        let store = AgentsStore(workspace: "w", runner: runner, sleep: { _ in })
        store.noOpenSoon("s") { XCTFail("\($0)") }
        XCTAssertEqual(store.closingOpen, ["s"])
        for _ in 0..<100 where !store.closingOpen.isEmpty { await Task.yield() }
        XCTAssertEqual(runner.sessionArgs.first, .object(["session": .string("s")]))
        XCTAssertTrue(store.closingOpen.isEmpty)
    }

    func testUndoKeepsTheItemsOpenAndAMessageClosesThemFirst() async throws {
        let runner = ScriptedRunner(replies: [#"{"session":{"id":"s","status":"done"},"entries":[]}"#])
        let store = AgentsStore(workspace: "w", runner: runner, sleep: { _ in try await Task.sleep(for: .seconds(60)) })
        store.noOpenSoon("s") { XCTFail("\($0)") }
        store.undoNoOpen("s")
        XCTAssertTrue(store.closingOpen.isEmpty)
        XCTAssertTrue(runner.sessionArgs.isEmpty)
        store.noOpenSoon("s") { XCTFail("\($0)") }
        try await store.flushNoOpen("s")
        XCTAssertEqual(runner.sessionArgs.first, .object(["session": .string("s")]))
        XCTAssertTrue(store.closingOpen.isEmpty)
    }

    func testOpenPassSkipsFiledItemsAndDropsOnesAnsweredElsewhere() throws {
        let entry = { (session: String, ids: [String]) in
            #"{"session":"\#(session)","title":"\#(session)","items":[\#(ids.map { #"{"id":"\#($0)","kind":"offer","label":"\#($0)","state":"open"}"# }.joined(separator: ","))]}"#
        }
        let queue = { (entries: [String]) in try JSONDecoder.clubmate.decode([OpenQueueEntry].self, from: Data("[\(entries.joined(separator: ","))]".utf8)) }
        var pass = OpenPass(try queue([entry("a", ["oi-1", "oi-2"]), entry("b", ["oi-1"])]))
        XCTAssertEqual(pass.sessions, ["a", "b"])
        pass.next()
        XCTAssertEqual(pass.current?.id, "a/oi-2")
        pass.file(try XCTUnwrap(pass.current))
        pass.next()
        XCTAssertEqual(pass.current?.id, "b/oi-1")
        XCTAssertTrue(pass.isLast)
        pass.previous()
        XCTAssertEqual(pass.current?.id, "a/oi-1")
        pass.update(try queue([entry("b", ["oi-1"])]))
        XCTAssertEqual(pass.steps.map(\.id), ["a/oi-2", "b/oi-1"])
        XCTAssertEqual(pass.current?.id, "b/oi-1")
        XCTAssertEqual(pass.items("a"), [])
    }

    func testDraftAtALaterPageIsNotEmpty() async throws {
        let store = AgentsStore(workspace: "w", runner: ScriptedRunner(replies: []))
        var draft = ElicitationDraft()
        draft.page = 1
        store.setElicitationDraft(draft, session: "s", request: "elicitation-1")
        XCTAssertEqual(store.elicitationDraft("s", request: "elicitation-1").page, 1)
    }
}

private struct OpenItemsWire: Decodable {
    let sessions: [AgentSession]
    let transcript: AgentTranscript
    let queue: [OpenQueueEntry]
}

@MainActor
final class AnswerRunner: CommandRunner {
    let pending: [String]
    let failure: RemoteError?
    private(set) var commands: [String] = []
    private(set) var sessionArgs: [JSONValue?] = []
    private(set) var listedAtPermission: [String] = []
    weak var store: AgentsStore?

    init(pending: [String], failure: RemoteError?) {
        self.pending = pending
        self.failure = failure
    }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        commands.append(id)
        switch id {
        case "agents.sessions":
            sessionArgs.append(args)
            let other: [JSONValue] = args?["session"] == nil ? [.object(["id": .string("other"), "status": .string("done")])] : []
            return .array([.object(["id": .string("s"), "status": .string("waiting"), "pending": .array(pending.map(JSONValue.string))])] + other)
        case "agents.permission":
            listedAtPermission = store?.sessions.map(\.id) ?? []
            if let failure { throw failure } else { return .null }
        default: return .array([])
        }
    }
}

@MainActor
func XCTAssertThrowsAsync<E: Error & Equatable>(_ work: @autoclosure @MainActor () async throws -> Void, _ expected: E, file: StaticString = #filePath, line: UInt = #line) async {
    do {
        try await work()
        XCTFail("no error", file: file, line: line)
    } catch {
        XCTAssertEqual(error as? E, expected, file: file, line: line)
    }
}

@MainActor
final class ScriptedRunner: CommandRunner {
    private var replies: [String]
    private var sessions: [String]
    private(set) var args: [JSONValue] = []
    private(set) var sessionArgs: [JSONValue] = []

    init(replies: [String], sessions: [String] = []) {
        self.replies = replies
        self.sessions = sessions
    }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        guard id == "agents.transcript" else {
            sessionArgs.append(args ?? .null)
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

@MainActor
final class SearchRunner: CommandRunner {
    private var held: CheckedContinuation<Void, Never>?
    private(set) var queries: [String] = []

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        let query = args?["query"]?.stringValue ?? ""
        queries.append(query)
        if query == "cart" { await withCheckedContinuation { held = $0 } }
        return try JSONDecoder().decode(JSONValue.self, from: Data(#"[{"session":"s-\#(query)","title":"","status":"closed","worktree":"/w","updatedAt":"2026-10-30T09:41:00Z","hits":[{"kind":"title","snippet":"t"},{"kind":"prompt","turn":2,"snippet":"p"},{"kind":"reply","turn":2,"snippet":"r"}]}]"#.utf8))
    }

    func release() {
        held?.resume()
        held = nil
    }
}

@MainActor
final class AgentSearchTests: XCTestCase {
    func testDecodesResultsAndDropsAStaleReply() async throws {
        let runner = SearchRunner()
        let store = AgentsStore(workspace: "w", runner: runner, sleep: { _ in })
        store.searchQuery = "cart"
        try await waitUntil { runner.queries == ["cart"] }
        store.searchQuery = "checkout"
        try await waitUntil { store.searchResults != nil }
        runner.release()
        try await Task.sleep(for: .milliseconds(20))
        let result = try XCTUnwrap(store.searchResults?.first)
        XCTAssertEqual(store.searchResults?.count, 1)
        XCTAssertEqual(result.session, "s-checkout")
        XCTAssertEqual(result.status, .closed)
        XCTAssertEqual(result.displayTitle, "New Session")
        XCTAssertEqual(result.snippets.map(\.kind), [.prompt, .reply])
        XCTAssertEqual(result.snippets.map(\.turn), [2, 2])
    }
}
