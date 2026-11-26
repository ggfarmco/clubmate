import Foundation
import XCTest

import ClubmateProtocol

import CompanionKit

final class GuideTests: XCTestCase {
    private func props(_ name: String) throws -> JSONValue {
        let samples = URL(filePath: #filePath).deletingLastPathComponent().appending(path: "../../../../plugins/agents/testdata").standardized
        return try JSONDecoder().decode(JSONValue.self, from: Data(contentsOf: samples.appending(path: name + ".json")))["props"] ?? .null
    }

    private func transcript() throws -> AgentTranscript {
        let step: JSONValue = .object(["kind": .string("step"), "id": .string("step-12"), "turn": .number(5), "step": .object(["step": .string("step-12"), "label": .string("Step 5 · Cap at burst"), "node": .string("guide")])])
        let json: JSONValue = .object(["session": .object(["id": .string("agt_1")]), "entries": .array([step]), "steps": try props("guide-steps"), "tour": try props("guide-tour")])
        return try JSONDecoder.clubmate.decode(AgentTranscript.self, from: JSONEncoder().encode(json))
    }

    func testTranscriptCarriesTheStepHeaderControlsAndMap() throws {
        let transcript = try transcript()
        let screen = SessionScreen(entries: transcript.entries, session: nil)
        guard case .entry(let entry) = screen.rows.first, case .step(let step) = entry.content else { return XCTFail("no step row: \(screen.rows)") }
        XCTAssertEqual(step.label, "Step 5 · Cap at burst")
        XCTAssertEqual(step.node, "guide")

        let steps = try XCTUnwrap(transcript.steps)
        XCTAssertEqual(steps.actions.map(\.id), ["continue", "test_me", "show_me", "go_deeper", "smaller_steps"])
        XCTAssertEqual(steps.actions.map { $0.key?.label }, ["⌘↩", "⌥T", "⌥S", "⌥D", "⌥M"])
        XCTAssertEqual(steps.actions.filter(\.primary).map(\.id), ["continue"])

        let tour = try XCTUnwrap(transcript.tour)
        XCTAssertEqual(tour.concepts.map(\.state), [.known, .known, .shaky, .missed, .current, .notYet, .notYet, .notYet])
        XCTAssertEqual(tour.concepts.map(\.depth), [0, 1, 2, 2, 3, 2, 3, 0])
        XCTAssertEqual(tour.stripLabel, "5 Cap at burst")
        XCTAssertEqual(tour.concepts[3].note, "line 32 · think so; answer line 30")
        XCTAssertEqual(tour.concepts[3].offers.map(\.id), ["ask_again", "go_back"])
        XCTAssertEqual(tour.concepts[7].links.map(\.label), ["ErrRateLimited"])
        XCTAssertFalse(tour.concepts[5].opens)
    }

    func testOpenAsideMakesGoDeeperPressedAndOpensThatSession() throws {
        let steps = try XCTUnwrap(try transcript().steps)
        let deeper = try XCTUnwrap(steps.actions.first { $0.id == StepControls.goDeeper })
        let show = try XCTUnwrap(steps.actions.first { $0.id == "show_me" })
        XCTAssertTrue(steps.on(deeper))
        XCTAssertFalse(steps.on(show))
        XCTAssertEqual(steps.openAside(deeper), "agt_2")
        XCTAssertNil(steps.openAside(show))
    }

    func testUnknownStateFallsBackAndKeyParsesModifiers() throws {
        XCTAssertEqual(TourState("later"), .other)
        let key = try XCTUnwrap(GuideKey("cmd+shift+m"))
        XCTAssertEqual(key.key, "m")
        XCTAssertTrue(key.command && key.shift && !key.option)
        XCTAssertEqual(key.label, "⇧⌘M")
        XCTAssertNil(GuideKey(""))
    }
}
