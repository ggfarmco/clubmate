import Foundation
import Synchronization
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
        XCTAssertEqual(AgentsTree.sections(sessions).map(\.id), ["inbox", "wt:0", "closed"])
        XCTAssertTrue(AgentsTree.supported(sessions))
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

    func testPausedSectionsPerResetAfterNeedsYou() throws {
        let json = #"""
        [
          {"id":"done","status":"done","group":"done"},
          {"id":"wed","status":"error","group":"paused","paused":true,"pausedUntil":1763892000,"failure":{"word":"resumes Wed","badge":"Stopped · resumes Wed","line":"Stopped by the usage limit · resumes Wed"}},
          {"id":"soon","status":"error","group":"paused","paused":true,"pausedUntil":1763319780,"failure":{"word":"resumes 19:03","badge":"Stopped · resumes 19:03","line":"Stopped by the usage limit · resumes 19:03"}},
          {"id":"parent","status":"done","group":"paused","pausedUntil":1763319780},
          {"id":"kid","parentSession":"parent","status":"error","group":"paused","paused":true,"pausedUntil":1763319780},
          {"id":"ask","status":"done","needsYou":true,"group":"needsYou"}
        ]
        """#
        let sessions = try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(json.utf8))
        let sections = AgentSession.grouped(sessions)
        XCTAssertEqual(sections.map(\.group), [.needsInput, .paused, .paused, .idle])
        XCTAssertEqual(sections[1].sessions.map(\.id).sorted(), ["parent", "soon"])
        XCTAssertEqual(sections[2].sessions.map(\.id), ["wed"])
        XCTAssertNotEqual(sections[1].id, sections[2].id)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        calendar.locale = Locale(identifier: "en_GB")
        let now = Date(timeIntervalSince1970: 1763312400)
        XCTAssertEqual(sections.map { $0.title(now: now, calendar: calendar) }, ["Needs you", "Paused until 19:03", "Paused until Sun", "Done"])
        XCTAssertEqual(sessions.map(\.glyph), [.done, .paused, .paused, .done, .paused, .question])
        XCTAssertEqual(sessions[2].stateLine(now: now).text, "Stopped by the usage limit · resumes 19:03")
        let old = try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(#"[{"id":"x","status":"error","group":"later"}]"#.utf8))
        XCTAssertEqual(old[0].group, .idle)
    }

    func testRunningDoneAndChildrenKeepThePluginsStartOrder() throws {
        let json = #"""
        [
          {"id":"b","status":"running","group":"running","listKey":"20261116T100002.000000000Z b","updatedAt":"2026-11-16T09:00:00Z"},
          {"id":"a","status":"running","group":"running","listKey":"20261116T100001.000000000Z a","updatedAt":"2026-11-16T11:00:00Z"},
          {"id":"kid2","parentSession":"a","status":"running","group":"running","listKey":"20261116T100004.000000000Z kid2","updatedAt":"2026-11-16T12:00:00Z"},
          {"id":"kid1","parentSession":"a","status":"done","group":"done","listKey":"20261116T100003.000000000Z kid1"},
          {"id":"new","status":"done","group":"done","createdAt":"2026-11-16T10:00:09Z","updatedAt":"2026-11-16T10:00:09Z"},
          {"id":"old","status":"done","group":"done","createdAt":"2026-11-16T10:00:08Z","updatedAt":"2026-11-16T12:00:00Z"}
        ]
        """#
        let groups = AgentSession.grouped(try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(json.utf8)))
        XCTAssertEqual(groups.map(\.group), [.running, .idle])
        XCTAssertEqual(groups[0].sessions.map(\.id), ["a", "b"])
        XCTAssertEqual(groups[0].sessions[0].children.map(\.id), ["kid1", "kid2"])
        XCTAssertEqual(groups[1].sessions.map(\.id), ["old", "new"])
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

    private func gateCalls() throws -> [TranscriptEntry] {
        try JSONDecoder.clubmate.decode([TranscriptEntry].self, from: fixture("agents-gate-calls"))
    }

    func testHeldCallEntryDecodes() throws {
        let entries = try gateCalls()
        guard case .tool(let one) = entries[0].content, case .tool(let two) = entries[1].content, case .tool(let agent) = entries[4].content else { return XCTFail("unexpected \(entries)") }
        let block = try XCTUnwrap(one.gate)
        XCTAssertTrue(one.isHeldBlock)
        XCTAssertEqual([block.title, block.caption, block.tool, block.input], ["Held by a gate", "shop · git push · round 1 of 2", "Bash", "git push origin fix/cart"])
        XCTAssertEqual(block.lines.map(\.text), ["Fable review → approved / changes"])
        XCTAssertEqual(block.reviewOnTitle, "agt_rev")
        XCTAssertEqual(two.gate?.lines.map(\.state), ["approved · waits for gh pr create", "reviewing · <1 min"])
        XCTAssertNil(two.gate?.reviewOnTitle)
        XCTAssertEqual(agent.held.map(\.id), ["s1"])
        XCTAssertEqual(agent.held.first?.gate?.title, "Held by a gate")
    }

    func testRefusedCallIsNotCountedFailed() throws {
        let rows = SessionScreen(entries: try gateCalls(), session: nil).rows
        guard let group = rows.compactMap({ row -> ToolGroup? in if case .tools(_, let group) = row, group.calls.first?.id == "t4" { group } else { nil } }).first else { return XCTFail("no t4 row in \(rows.map(\.id))") }
        XCTAssertFalse(group.failed)
        XCTAssertEqual(group.calls.first?.gate?.word, "refused")
        XCTAssertFalse(group.calls.first?.isHeldBlock ?? true)
    }

    func testGateMessageDecodes() throws {
        guard case .gateMessage(let message) = try gateCalls()[5].content else { return XCTFail("not a gate message") }
        XCTAssertEqual(message.groups.map(\.title), ["From the gate · shop · git push · round 1 of 2 · changes"])
        XCTAssertEqual(message.groups.map(\.findings), [["total.go:5 — discount first"]])
        XCTAssertEqual(message.groups.map(\.deferred), [["naming"]])
        XCTAssertEqual([message.tail, message.call], ["The next git push goes through the gate again, as round 2.", "t3"])
        guard case .user(let text, _, _, _) = try gateCalls()[6].content else { return XCTFail("a message with no groups is not a user message") }
        XCTAssertEqual(text, "[Clubmate] From the gate · shop · git push · round 1 of 2 · changes")
    }

    func testGateMessageSitsUnderItsRefusedCall() throws {
        let rows = SessionScreen(entries: try gateCalls(), session: nil).rows
        XCTAssertEqual(rows.map(\.id), ["tool/t1", "tool/t2", "tool/t3", "user/gate-t3", "tool/t4", "tool/sub-1", "tool/sub-1/held/s1", "user/gate-empty"])
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

    func testLandReviewersHaveNoRowUnlessTheyAskAndLandersSayTheirLanding() async throws {
        let runner = ScriptedRunner(replies: [], sessions: [#"""
        {"sessions":[
          {"id":"lander","status":"idle","group":"needsYou","childStatus":"waiting","landing":{"round":2,"of":2,"verdict":"reviewing","words":"review 2/2…","tone":"accent","badge":"reviewing","into":"main"}},
          {"id":"asking","status":"waiting","needsYou":true,"group":"needsYou","pending":["perm-1"],"rowUnder":"lander","gateRound":{"round":2,"label":"Round 2 review","verdict":"reviewing"}},
          {"id":"old","status":"closed","group":"closed","hidden":true,"gateRound":{"round":1,"label":"Round 1 review","verdict":"changes"}},
          {"id":"fixing","status":"done","group":"done","landing":{"round":1,"of":2,"verdict":"changes","findings":3,"words":"3 changes","tone":"plain","badge":"3 changes","into":"main"}}
        ],"removed":[],"cursor":"e.1"}
        """#])
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.refresh()
        XCTAssertEqual(store.sessions.map(\.id), ["lander", "asking", "fixing"])
        XCTAssertEqual(store.session("old")?.displayTitle, "Round 1 review")
        let groups = store.groups
        XCTAssertEqual(groups.map(\.group), [.needsInput, .idle])
        let lander = try XCTUnwrap(groups.first?.sessions.first)
        XCTAssertEqual(lander.children.map(\.session.displayTitle), ["Round 2 review"])
        XCTAssertEqual([lander.session.stateLine(now: .now).text, groups[1].sessions[0].session.stateLine(now: .now).text], ["review 2/2…", "3 changes"])
        XCTAssertEqual([lander.session.stateLine(now: .now).tone, groups[1].sessions[0].session.stateLine(now: .now).tone], [.accent, .secondary])
        let activity = WorkspaceActivity(store.sessions)
        XCTAssertEqual([activity.waiting, activity.closed], [1, 0])
    }

    func testHeldSessionReadsItsLine() async throws {
        let runner = ScriptedRunner(replies: [], sessions: [#"""
        {"sessions":[
          {"id":"held","status":"running","group":"running","turnHeld":true,"heldRound":{"text":"Plan pipeline · round 1 reviewing","run":"gr_1"},"landing":{"round":1,"of":1,"verdict":"reviewing","words":"held","tone":"accent","badge":"held · plans and specs","held":"plans and specs","line":"Held · plans and specs · Plan pipeline…","hint":"Held by a gate — messages queue"}}
        ],"removed":[],"cursor":"e.1"}
        """#])
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.refresh()
        let line = try XCTUnwrap(store.session("held")).stateLine(now: .now)
        XCTAssertEqual([line.text], ["Held · plans and specs · Plan pipeline…"])
        XCTAssertEqual(line.tone, .accent)
        XCTAssertEqual(try XCTUnwrap(store.session("held")).composerPlaceholder, "Held by a gate — messages queue")
        XCTAssertTrue(try XCTUnwrap(store.session("held")).turnHeld)
        XCTAssertEqual(try XCTUnwrap(store.session("held")).heldRound, HeldRound(text: "Plan pipeline · round 1 reviewing", run: "gr_1"))
    }

    func testUngatedSessionReadsAPlainLineAndKeepsItsHeaderBadge() async throws {
        let runner = ScriptedRunner(replies: [], sessions: [#"""
        {"sessions":[
          {"id":"bg","status":"running","group":"running","landing":{"round":0,"of":0,"verdict":"","words":"not gated yet","tone":"plain","badge":"not gated yet · after its background command","line":"Not gated yet · after its background command","ungated":true},"ungated":{"chip":"not gated yet","badge":"not gated yet · after its background command","line":"Not gated yet · after its background command"}}
        ],"removed":[],"cursor":"e.1"}
        """#])
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.refresh()
        let session = try XCTUnwrap(store.session("bg"))
        let line = session.stateLine(now: .now)
        XCTAssertEqual(line.text, "Not gated yet · after its background command")
        XCTAssertEqual(line.tone, .secondary)
        XCTAssertEqual(session.ungated?.badge, "not gated yet · after its background command")
    }

    func testStopSendsItsTurnAndAStaleOneRefreshesTheSession() async throws {
        let runner = ScriptedRunner(replies: [], sessions: [
            #"{"sessions":[{"id":"b","status":"running","turns":4}],"removed":[],"cursor":"e.3"}"#,
            ServerReason.stale,
            #"{"sessions":[{"id":"b","status":"idle","turns":4}],"removed":[],"cursor":"e.4","delta":true}"#,
        ])
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.refresh()
        do {
            try await store.cancel("b")
            XCTFail("a stop for an ended turn is refused")
        } catch {}
        XCTAssertEqual(runner.sessionArgs[1], .object(["session": .string("b"), "turn": .number(4)]))
        XCTAssertEqual(store.session("b")?.status, .idle)
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

    func testLeavingASessionDropsItsImagesAndKeepsTheLastFewTranscripts() async throws {
        let runner = ImageRunner()
        runner.data = Data("png".utf8).base64EncodedString()
        let store = AgentsStore(workspace: "w", runner: runner)
        let sessions = (0...AgentsStore.keptTranscripts).map { "s\($0)" }
        for session in sessions {
            store.watch(session)
            try await waitUntil { store.transcripts[session] != nil }
        }
        await store.loadImage("/w/a.png", entry: "e", in: "s0")
        await store.loadImage("/w/a.png", entry: "e", in: "t")

        for session in sessions { store.unwatch(session) }

        XCTAssertNil(store.image("/w/a.png", entry: "e", in: "s0"))
        XCTAssertEqual(store.image("/w/a.png", entry: "e", in: "t"), .loaded(Data("png".utf8)))
        XCTAssertNil(store.transcripts["s0"])
        XCTAssertEqual(sessions.dropFirst().filter { store.transcripts[$0] != nil }, Array(sessions.dropFirst()))
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

    func testAnImageOnScreenIsNeverEvicted() async {
        let runner = ImageRunner()
        runner.data = Data("png".utf8).base64EncodedString()
        let store = AgentsStore(workspace: "w", runner: runner)
        store.showImage("/w/0.png", entry: "e", in: "s", shown: true)
        for index in 0...AgentsStore.imageLimit {
            await store.loadImage("/w/\(index).png", entry: "e", in: "s")
        }
        XCTAssertEqual(store.image("/w/0.png", entry: "e", in: "s"), .loaded(Data("png".utf8)))
        XCTAssertNil(store.image("/w/1.png", entry: "e", in: "s"))
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

    func testAHiddenRunningReviewerOpenedByIDKeepsFollowingItsTranscript() async throws {
        let first = #"{"session":{"id":"r","status":"running"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"review"}]}"#
        let second = #"{"session":{"id":"r","status":"done"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"review"},{"kind":"agent","id":"a1","turn":1,"text":"approved"}]}"#
        let runner = ScriptedRunner(replies: [first, second], sessions: [#"{"sessions":[{"id":"r","status":"running","hidden":true}],"removed":[],"cursor":"e.1"}"#])
        let store = AgentsStore(workspace: "w", runner: runner, sleep: { _ in try await Task.sleep(for: .milliseconds(10)) })
        await store.refresh()
        XCTAssertTrue(store.sessions.isEmpty)
        store.watch("r")
        try await waitUntil { store.transcripts["r"]?.entries.count == 2 }
        store.unwatch("r")
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

    func testACursorTheMacNoLongerKnowsRefetchesTheWholeTranscript() async throws {
        let first = #"{"session":{"id":"s","status":"done"},"cursor":"e.2","entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"},{"kind":"user","id":"prompt-2","turn":2,"text":"two"}]}"#
        let restarted = #"{"session":{"id":"s","status":"done"},"cursor":"f.1","entries":[{"kind":"user","id":"prompt-2","turn":2,"text":"two"}]}"#
        let full = #"{"session":{"id":"s","status":"done"},"cursor":"f.1","entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one, edited"},{"kind":"user","id":"prompt-2","turn":2,"text":"two"}]}"#
        let runner = ScriptedRunner(replies: [first, restarted, full])
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.loadTranscript("s")
        await store.loadTranscript("s", incremental: true)
        XCTAssertEqual(runner.args.last, .object(["session": .string("s"), "compact": .bool(true)]))
        XCTAssertEqual(store.transcripts["s"]?.entries.first?.content, .user(text: "one, edited", fromAgent: false, images: []))
    }

    func testReopeningALeftSessionAsksOnlyForWhatChanged() async throws {
        let reply = #"{"session":{"id":"s","status":"done"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"}],"cursor":"e.1"}"#
        let delta = #"{"session":{"id":"s","status":"done"},"entries":[],"cursor":"e.1","delta":true}"#
        let runner = ScriptedRunner(replies: [reply, delta])
        let store = AgentsStore(workspace: "w", runner: runner)
        store.watch("s")
        try await waitUntil { store.transcripts["s"] != nil }
        store.unwatch("s")
        store.watch("s")
        try await waitUntil { runner.args.count == 2 }
        XCTAssertEqual(runner.args[1]["since"], .string("e.1"))
        store.unwatch("s")
    }

    func testAnUnchangedRefetchLeavesTheTranscriptStateUntouched() async throws {
        let reply = #"{"session":{"id":"s","status":"running"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"}]}"#
        let again = #"{"session":{"id":"s","status":"running","seenAt":"now"},"entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one","extra":1}]}"#
        let runner = ScriptedRunner(replies: [reply, again])
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.loadTranscript("s")
        XCTAssertNotNil(store.transcripts["s"])
        let changed = Mutex(false)
        withObservationTracking {
            _ = store.transcripts["s"]
            _ = store.transcriptErrors["s"]
            _ = store.entries("s")
        } onChange: {
            changed.withLock { $0 = true }
        }
        await store.loadTranscript("s", incremental: true)
        XCTAssertFalse(changed.withLock { $0 })
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
    private(set) var toolCalls: [JSONValue] = []
    var durable = false
    var held = false
    private(set) var waiting: CheckedContinuation<Void, Never>?

    init(replies: [String], sessions: [String] = []) {
        self.replies = replies
        self.sessions = sessions
    }

    func supports(_ command: String) -> Bool {
        command != AgentCommand.toolCall || durable
    }

    func release() {
        held = false
        waiting?.resume()
        waiting = nil
    }

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        if id == AgentCommand.toolCall {
            toolCalls.append(args ?? .null)
            return .object(["tool": .object(["title": .string("Read a.go"), "kind": .string("read"), "content": .array([.object(["type": .string("content"), "content": .object(["type": .string("text"), "text": .string("package a")])])])])])
        }
        if id == "agents.transcript", held { await withCheckedContinuation { waiting = $0 } }
        guard id == "agents.transcript" else {
            sessionArgs.append(args ?? .null)
            guard !sessions.isEmpty else { return .array([]) }
            let reply = sessions.removeFirst()
            if reply == ServerReason.stale { throw RemoteError.server(code: reply, message: "turn 4 is over") }
            return try JSONDecoder().decode(JSONValue.self, from: Data(reply.utf8))
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

@MainActor
final class TranscriptCacheTests: XCTestCase {
    private let slimReply = #"{"session":{"id":"s","status":"done"},"cursor":"7:3:e:k","entries":[{"kind":"user","id":"prompt-1","turn":1,"text":"one"},{"kind":"tool","id":"t1","turn":1,"tool":{"title":"Edit a.go","kind":"edit","status":"completed","locations":[{"path":"/w/a.go"}]},"toolSummary":{"diffs":[{"path":"/w/a.go","added":2,"removed":1}]}}]}"#
    private let emptyDelta = #"{"session":{"id":"s","status":"done"},"cursor":"7:3:e:k","delta":true,"entries":[]}"#

    private func cache() -> TranscriptCache {
        TranscriptCache(lists: ListCache(dir: FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)))
    }

    private func transcript(_ json: String) throws -> AgentTranscript {
        try JSONDecoder.clubmate.decode(AgentTranscript.self, from: Data(json.utf8))
    }

    func testALeftTranscriptShowsFromDiskBeforeTheMacAnswersAndAsksOnlySinceItsCursor() async throws {
        let disk = cache()
        let first = ScriptedRunner(replies: [slimReply])
        first.durable = true
        let before = AgentsStore(workspace: "w", runner: first, computer: "mac", transcriptCache: disk)
        before.watch("s")
        try await waitUntil { before.transcripts["s"] != nil }
        XCTAssertEqual(first.args.first?["slim"], .bool(true))
        before.unwatch("s")
        try await waitUntilAsync { await disk.cached().count == 1 }

        let second = ScriptedRunner(replies: [emptyDelta])
        second.held = true
        let after = AgentsStore(workspace: "w", runner: second, computer: "mac", transcriptCache: disk)
        after.watch("s")
        try await waitUntil { after.transcripts["s"] != nil }
        XCTAssertTrue(second.args.isEmpty)
        let shown = try XCTUnwrap(after.transcripts["s"])
        XCTAssertEqual(shown, try transcript(slimReply))
        guard case .tool(let call) = shown.entries[1].content else { return XCTFail("not a tool") }
        XCTAssertEqual(call.changes.added, 2)
        XCTAssertTrue(call.hasDetails)
        try await waitUntil { second.waiting != nil }
        second.release()
        try await waitUntil { second.args.count == 1 }
        XCTAssertEqual(second.args.first?["since"], .string("7:3:e:k"))
        after.unwatch("s")
    }

    func testALongDiskCopyWindowsLikeTheFetchedTranscriptAndSlimToolsCountTheSameRows() async throws {
        func reply(slim: Bool) -> String {
            let entries = (1...90).flatMap { turn -> [String] in
                let tool = slim
                    ? #"{"kind":"tool","id":"t\#(turn)","turn":\#(turn),"tool":{"title":"Edit a.go","kind":"edit","status":"completed"},"toolSummary":{"diffs":[{"path":"/w/a.go","added":1,"removed":1}]}}"#
                    : #"{"kind":"tool","id":"t\#(turn)","turn":\#(turn),"tool":{"title":"Edit a.go","kind":"edit","status":"completed","content":[{"type":"diff","path":"/w/a.go","oldText":"a\n","newText":"b\n"}]}}"#
                return [#"{"kind":"user","id":"prompt-\#(turn)","turn":\#(turn),"text":"go \#(turn)"}"#, tool, #"{"kind":"agent","id":"a\#(turn)","turn":\#(turn),"text":"reply \#(turn)"}"#]
            }
            return #"{"session":{"id":"s","status":"done"},"cursor":"9:0:e:k","entries":["# + entries.joined(separator: ",") + "]}"
        }
        let disk = cache()
        let slim = try transcript(reply(slim: true))
        await disk.save(slim, TranscriptCache.Key(computer: "mac", workspace: "w", session: "s"))
        let runner = ScriptedRunner(replies: [emptyDelta])
        runner.held = true
        let store = AgentsStore(workspace: "w", runner: runner, computer: "mac", transcriptCache: disk)
        store.watch("s")
        try await waitUntil { store.transcripts["s"] != nil }
        let rows = { (entries: [TranscriptEntry]) in SessionScreen(entries: entries, session: AgentSession(id: "s", status: .done)).rows }
        let restored = rows(store.entries("s"))
        XCTAssertEqual(restored, rows(slim.entries))
        XCTAssertEqual(restored.count, rows(try transcript(reply(slim: false)).entries).count)
        var window = TranscriptWindow()
        window.update(count: restored.count, atBottom: true)
        XCTAssertEqual(window.skip(restored), restored.count - TranscriptWindow.size)
        try await waitUntil { runner.waiting != nil }
        runner.release()
        store.unwatch("s")
    }

    func testAMacWithoutTheDurableCursorGetsNoSlimRequestAndNothingOnDisk() async throws {
        let disk = cache()
        let runner = ScriptedRunner(replies: [slimReply])
        let cursorless = ScriptedRunner(replies: [#"{"session":{"id":"s","status":"done"},"entries":[]}"#])
        cursorless.durable = true
        let other = AgentsStore(workspace: "w", runner: cursorless, computer: "mac", transcriptCache: disk)
        other.watch("s")
        try await waitUntil { other.transcripts["s"] != nil }
        other.unwatch("s")
        let store = AgentsStore(workspace: "w", runner: runner, computer: "mac", transcriptCache: disk)
        store.watch("s")
        try await waitUntil { store.transcripts["s"] != nil }
        store.unwatch("s")
        XCTAssertNil(runner.args.first?["slim"])
        let cached = await disk.cached()
        XCTAssertTrue(cached.isEmpty)
    }

    func testTheOldestTranscriptLeavesTheDiskPastTheCap() async throws {
        let disk = TranscriptCache(lists: ListCache(dir: FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)), maxCount: 2)
        let reply = try transcript(slimReply)
        let key = { (s: String) in TranscriptCache.Key(computer: "mac", workspace: "w", session: s) }
        await disk.save(reply, key("a"))
        await disk.save(reply, key("b"))
        _ = await disk.load(key("a"))
        await disk.save(reply, key("c"))
        let kept = await disk.cached()
        XCTAssertEqual(Set(kept), [key("a"), key("c")])
        let gone = await disk.load(key("b"))
        XCTAssertNil(gone)
    }

    func testASessionRemovedOnTheMacLeavesTheDisk() async throws {
        let disk = cache()
        let key = TranscriptCache.Key(computer: "mac", workspace: "w", session: "s")
        await disk.save(try transcript(slimReply), key)
        let runner = ScriptedRunner(replies: [], sessions: [#"{"sessions":[{"id":"s","status":"done"}],"removed":[],"cursor":"e.1"}"#, #"{"sessions":[],"removed":["s"],"cursor":"e.2","delta":true}"#])
        let store = AgentsStore(workspace: "w", runner: runner, computer: "mac", transcriptCache: disk)
        await store.refresh()
        let listed = await disk.cached()
        XCTAssertEqual(listed, [key])
        await store.refresh()
        try await waitUntilAsync { await disk.cached().isEmpty }
    }

    func testAnOpenedSlimCallIsFetchedAgainOnlyAfterItChanged() async throws {
        let changed = #"{"session":{"id":"s","status":"done"},"cursor":"8:0:e:k","delta":true,"entries":[{"kind":"tool","id":"t1","turn":1,"tool":{"title":"Edit a.go","kind":"edit","status":"failed"},"toolSummary":{}}]}"#
        let runner = ScriptedRunner(replies: [slimReply, emptyDelta, changed])
        runner.durable = true
        let store = AgentsStore(workspace: "w", runner: runner)
        await store.loadTranscript("s")
        let call = await store.toolCall("t1", in: "s")
        XCTAssertEqual(call?.output, "package a")
        await store.loadTranscript("s", incremental: true)
        _ = await store.toolCall("t1", in: "s")
        XCTAssertEqual(runner.toolCalls.count, 1)
        await store.loadTranscript("s", incremental: true)
        _ = await store.toolCall("t1", in: "s")
        XCTAssertEqual(runner.toolCalls.count, 2)
    }
}

func waitUntilAsync(timeout: Duration = .seconds(5), _ condition: @Sendable () async -> Bool) async throws {
    let deadline = ContinuousClock.now + timeout
    while await !condition() {
        guard ContinuousClock.now < deadline else { throw CancellationError() }
        try await Task.sleep(for: .milliseconds(5))
    }
}
