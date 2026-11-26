import Foundation
import XCTest

import ClubmateProtocol

import CompanionKit

final class QuizTests: XCTestCase {
    private func props(_ name: String) throws -> JSONValue {
        let samples = URL(filePath: #filePath).deletingLastPathComponent().appending(path: "../../../../plugins/agents/testdata").standardized
        return try JSONDecoder().decode(JSONValue.self, from: Data(contentsOf: samples.appending(path: name + ".json")))["props"] ?? .null
    }

    private func entry(_ name: String) throws -> TranscriptEntry {
        let entry: JSONValue = .object(["kind": .string("elicitation"), "id": .string("elicitation-7"), "turn": .number(4), "quiz": try props(name)])
        return try JSONDecoder.clubmate.decode(TranscriptEntry.self, from: JSONEncoder().encode(entry))
    }

    private func quiz(_ name: String) throws -> Quiz {
        guard let quiz = Quiz(props: try props(name).objectValue ?? [:]) else { throw XCTSkip("no quiz") }
        return quiz
    }

    func testAskingQuizDocksAboveTheComposer() throws {
        let screen = SessionScreen(entries: [try entry("quiz-asking")], session: nil)
        guard case .quiz(let quiz) = screen.dock else { return XCTFail("not docked: \(String(describing: screen.dock))") }
        XCTAssertTrue(screen.rows.isEmpty)
        XCTAssertEqual(screen.quizzes, ["elicitation-7"])
        XCTAssertEqual(quiz.request, "elicitation-7")
        XCTAssertEqual(quiz.excerpt?.lines.count, 18)
        XCTAssertEqual(quiz.excerpt?.pickedRange, 32...32)
        XCTAssertEqual(quiz.confidence?.levels.map(\.key), ["1", "2", "3"])
        XCTAssertEqual(quiz.confidence?.sends, true)
        XCTAssertEqual(quiz.confidence?.holdMs, 2000)
        XCTAssertEqual(quiz.labels.undo, "Undo")
        XCTAssertEqual(quiz.labels.sayIt, "Say it in words…")
        XCTAssertEqual(quiz.labels.range, "Range")
        XCTAssertEqual(quiz.confidence?.needsRating, true)
    }

    func testAnsweredQuizIsATranscriptRecordWithMarksAndShortRef() throws {
        let screen = SessionScreen(entries: [try entry("quiz-answered")], session: nil)
        XCTAssertNil(screen.dock)
        XCTAssertTrue(screen.quizzes.isEmpty)
        guard case .asked(let question) = screen.rows.first, let quiz = question.quiz, let excerpt = quiz.excerpt, let reveal = quiz.reveal else {
            return XCTFail("no record: \(screen.rows)")
        }
        XCTAssertEqual(excerpt.rows.first, .fold("lines 23–27"))
        XCTAssertEqual(excerpt.lines.filter { $0.tone != nil }.map { "\($0.n) \($0.tone ?? "") \($0.label ?? "")" }, ["30 success answer", "32 danger you · think so"])
        XCTAssertEqual(reveal.verdict, "wrong")
        XCTAssertEqual(reveal.shortRef, "limiter.go:30")
        XCTAssertFalse(reveal.sureWrong)
    }

    func testCommonIndentIsDroppedForThePhone() throws {
        let excerpt = try XCTUnwrap(try quiz("quiz-asking").excerpt)
        let line32 = try XCTUnwrap(excerpt.lines.first { $0.n == 32 })
        XCTAssertEqual(excerpt.commonIndent, 4)
        XCTAssertEqual(excerpt.text(line32, dropIndent: true), "    b.tokens = l.burst")
        XCTAssertEqual(excerpt.text(line32, dropIndent: false), "        b.tokens = l.burst")
    }

    func testPeekShowsThreeLinesAroundThePickAndStaysInside() throws {
        let excerpt = try XCTUnwrap(try quiz("quiz-asking").excerpt)
        XCTAssertEqual(excerpt.window(3).map(\.n), [31, 32, 33])
        let last = try XCTUnwrap(try quiz("quiz-answered").excerpt)
        XCTAssertEqual(last.window(3).map(\.n), [28, 29, 30])
    }

    func testTapPicksALineOrEndsARange() throws {
        let excerpt = try XCTUnwrap(try quiz("quiz-asking").excerpt)
        XCTAssertEqual(excerpt.tap(34, range: false), 34...34)
        XCTAssertEqual(excerpt.tap(29, range: true), 29...32)
        XCTAssertEqual(excerpt.nudge(1, range: false), 33...33)
        XCTAssertEqual(excerpt.nudge(1, range: true), 32...33)
        XCTAssertNil(excerpt.nudge(-1, range: true))
        XCTAssertNil(excerpt.step(40, 1))
    }

    func testKeysMoveTheCursorAndShiftExtendsTheSelection() throws {
        let excerpt = try XCTUnwrap(try quiz("quiz-asking").excerpt)
        var draft = QuizDraft()
        draft.move(-1, extend: false, in: excerpt)
        XCTAssertEqual(draft.selection(excerpt), 31...31)
        draft.move(-1, extend: true, in: excerpt)
        draft.move(-1, extend: true, in: excerpt)
        XCTAssertEqual(draft.selection(excerpt), 29...31)
        draft.move(1, extend: false, in: excerpt)
        XCTAssertEqual(draft.selection(excerpt), 30...30)
    }

    func testActionsCarryTheContractArgs() {
        XCTAssertEqual(QuizAction.pick(30...32).args(session: "agt_1", request: "elicitation-7"), ["session": .string("agt_1"), "request": .string("elicitation-7"), "action": .string("pick"), "from": .number(30), "to": .number(32)])
        XCTAssertEqual(QuizAction.send(text: "line 30", confidence: "sure").fields(request: "r"), ["request": .string("r"), "text": .string("line 30"), "confidence": .string("sure")])
        XCTAssertEqual(QuizAction.option("b").event, "quizPick")
        XCTAssertNil(QuizAction.send(text: "x", confidence: nil).event)
    }
}
