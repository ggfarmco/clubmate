import Foundation
import XCTest

import CompanionKit

@MainActor
final class AsidesTests: XCTestCase {
    func testDecodesAsideFields() throws {
        let sessions = try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(#"""
        [{"id":"p","status":"waiting","canAside":true,"composerDraft":{"text":"> q\n\nnote","rev":2}},
         {"id":"a","parentSession":"p","status":"done","aside":{"state":"sent","label":"Aside · B","detail":"sent","anchor":{"elicitation":"e1","field":0,"option":"B"}}},
         {"id":"k","status":"done","forkedFrom":"p"}]
        """#.utf8))
        XCTAssertTrue(sessions[0].canAside)
        XCTAssertEqual(sessions[0].composerDraft, ComposerDraft(text: "> q\n\nnote", rev: 2))
        XCTAssertEqual(sessions[1].aside, AsideInfo(state: .sent, label: "Aside · B", detail: "sent", anchor: AsideAnchor(elicitation: "e1", field: 0, option: "B")))
        XCTAssertEqual(sessions[2].forkedFrom, "p")

        let transcript = try JSONDecoder.clubmate.decode(AgentTranscript.self, from: Data(#"""
        {"session":{"id":"a","status":"done"},"entries":[],
         "pinned":{"type":"choice","id":"a/pinned","props":{"style":"radio","text":"Store","action":"asidePick","pinned":true,"options":[{"value":"A","label":"A"},{"value":"B","label":"B","on":true,"description":"two"}]}},
         "asidePanel":{"question":1,"option":2,"text":"Store","label":"B","answer":true}}
        """#.utf8))
        XCTAssertEqual(transcript.pinned?.options.map(\.on), [false, true])
        XCTAssertEqual(transcript.pinned?.text, "Store")
        XCTAssertEqual(transcript.asidePanel, AsidePanel(question: 1, option: 2, text: "Store", label: "B", answer: true))
        XCTAssertEqual(transcript.asidePanel?.place, "question 1 · option 2")
    }

    func testPrefillMergesOnceAndKeepsLaterEdits() async throws {
        let runner = AsideRunner()
        let store = AgentsStore(workspace: "w", runner: runner)
        runner.prefill = #"{"elicitation":"e1","field":"question_0","value":"B","note":"too heavy"}"#
        await store.loadTranscript("p")
        var draft = store.elicitationDraft("p", request: "e1")
        XCTAssertEqual(draft.picks["question_0"], ["B"])
        XCTAssertEqual(draft.texts["question_0_custom"], "too heavy")
        XCTAssertEqual(draft.opened, ["question_0_custom"])

        draft.picks["question_0"] = ["C"]
        store.setElicitationDraft(draft, session: "p", request: "e1")
        await store.loadTranscript("p")
        XCTAssertEqual(store.elicitationDraft("p", request: "e1").picks["question_0"], ["C"])

        runner.prefill = #"{"elicitation":"e1","field":"question_0","value":"A"}"#
        await store.loadTranscript("p")
        XCTAssertEqual(store.elicitationDraft("p", request: "e1").picks["question_0"], ["A"])
    }

    func testOpenOtherPrefillOnlyFillsTheNote() throws {
        let item = try JSONDecoder.clubmate.decode(OpenItem.self, from: Data(#"{"id":"oi-1","kind":"decision","options":[{"label":"One","description":""},{"label":"Two","description":""}]}"#.utf8))
        var draft = OpenDraft()
        draft.picks["oi-1"] = ["One"]
        let merged = draft.merging(AsidePrefill(open: "oi-1", value: "", note: "neither"), item: item)
        XCTAssertNil(merged.picks["oi-1"])
        XCTAssertEqual(merged.other(item), "neither")
    }

    func testComposerDraftApply() {
        let draft = ComposerDraft(text: "> q\n\nnote", rev: 2)
        XCTAssertEqual(draft.apply(local: "", seen: nil), "> q\n\nnote")
        XCTAssertNil(draft.apply(local: "typed", seen: nil))
        XCTAssertNil(draft.apply(local: "", seen: 2))
        XCTAssertEqual(draft.apply(local: "typed", seen: 1), "> q\n\nnote")
    }

    func testAsidesHangAfterChildrenOldestFirstAndAnAskingOneStaysUnfolded() throws {
        let sessions = try JSONDecoder.clubmate.decode([AgentSession].self, from: Data(#"""
        [{"id":"p","status":"done","group":"needsYou","createdAt":"2026-11-16T09:00:00Z"},
         {"id":"old","parentSession":"p","status":"done","createdAt":"2026-11-16T10:00:00Z","aside":{"state":"open","detail":"open"}},
         {"id":"kid","parentSession":"p","status":"done","createdAt":"2026-11-16T09:30:00Z"},
         {"id":"asks","parentSession":"p","status":"waiting","needsYou":true,"createdAt":"2026-11-16T11:00:00Z","aside":{"state":"asks","detail":"asks you"}}]
        """#.utf8))
        let root = try XCTUnwrap(AgentSession.grouped(sessions).first?.sessions.first)
        XCTAssertEqual(root.children.map(\.id), ["kid", "old", "asks"])
        XCTAssertEqual(root.rows(expanded: []).map(\.id), ["p", "asks"])
        XCTAssertEqual(root.session.stateLine(now: .now, waitingChildren: root.waitingDescendants, waitingAsides: root.waitingAsides), SessionStateLine(text: "An aside asks you"))
        XCTAssertEqual(sessions[1].glyph, .aside)
        XCTAssertEqual(sessions[1].stateLine(now: .now), SessionStateLine(text: "open"))
        XCTAssertEqual(sessions[1].subagentLabel, "Aside")
    }

    func testAsideCommands() async throws {
        let runner = AsideRunner()
        let store = AgentsStore(workspace: "w", runner: runner)
        let id = try await store.askAside("p", anchor: AsideAnchor(elicitation: "e1", field: 0, option: "B"))
        XCTAssertEqual(id, "a")
        XCTAssertEqual(runner.sent.first?.args?["anchor"], .array([
            .object(["key": .string("elicitation"), "value": .string("e1")]), .object(["key": .string("field"), "value": .string("0")]), .object(["key": .string("option"), "value": .string("B")]),
        ]))
        store.asidePresentation["p"] = .sheet("a")
        try await store.asideSend("a", parent: "p", to: .answer, text: "note", close: true)
        XCTAssertEqual(runner.sent.last?.args, .object(["session": .string("a"), "to": .string("answer"), "text": .string("note"), "close": .bool(true)]))
        XCTAssertNil(store.asidePresentation["p"])
        try await store.asideSummary("a")
        try await store.asideSummary("a")
        XCTAssertEqual(runner.sent.filter { $0.id == AsideCommand.summary }.count, 1)
        store.forgetSummary("a")
        XCTAssertNil(store.summaryAsked["a"])
        runner.busy = true
        let ask = try await store.closeAsk("a")
        XCTAssertEqual(ask, "Close without sending?")
    }
}

@MainActor
private final class AsideRunner: CommandRunner {
    var prefill = ""
    var busy = false
    private(set) var sent: [(id: String, args: JSONValue?)] = []

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        switch id {
        case AgentCommand.sessions:
            return .array([])
        case "agents.transcript":
            return try JSONDecoder().decode(JSONValue.self, from: Data("""
            {"session":{"id":"p","status":"waiting"},"entries":[{"kind":"elicitation","id":"e1","turn":1,"elicitation":{"mode":"form","message":"Which?","fields":[
              {"key":"question_0","type":"choice","title":"Store","options":[{"value":"A","label":"A"},{"value":"B","label":"B"},{"value":"C","label":"C"}]},
              {"key":"question_0_custom","type":"text","title":"Other"}]}}],
             "prefills":[\(prefill)]}
            """.utf8))
        case AsideCommand.ask:
            sent.append((id, args))
            return .object(["session": .string("a"), "reopened": .bool(false)])
        case AsideCommand.busy:
            return .object(["busy": .bool(busy), "message": .string("Close without sending?")])
        default:
            sent.append((id, args))
            return .null
        }
    }
}
