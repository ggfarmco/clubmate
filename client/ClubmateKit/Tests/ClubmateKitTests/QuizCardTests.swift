import XCTest

import ClubmateProtocol
@testable import ClubmateKit

@MainActor
final class QuizCardTests: XCTestCase {
    private func excerpt(lines: ClosedRange<Int>, pick: String = "range", picked: [Int]? = nil) throws -> SDUIQuizExcerpt {
        let rows = lines.map { "{\"n\": \($0), \"text\": \"l\($0)\"}" }.joined(separator: ",")
        let span = picked.map { ", \"picked\": \($0)" } ?? ""
        let excerpt = "{\"file\": \"a.go\", \"commit\": \"abc1234\", \"pick\": \"\(pick)\", \"rows\": [\(rows)]\(span)}"
        let json = "{\"type\": \"request.quiz\", \"id\": \"s/elicitation/q\", \"props\": {\"request\": \"q\", \"type\": \"range\", \"state\": \"asking\", \"title\": \"t\", \"question\": \"q\", \"excerpt\": \(excerpt)}}"
        let node = try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8))
        return try XCTUnwrap(node.quiz()?.excerpt)
    }

    func testShiftMoveBuildsARangeAndAPlainMoveDropsIt() throws {
        let excerpt = try excerpt(lines: 29...34, picked: [32, 32])
        let model = QuizCardModel()

        XCTAssertEqual(model.at(excerpt), 32)
        model.move(1, extend: true, in: excerpt)
        model.move(1, extend: true, in: excerpt)
        XCTAssertEqual(model.pick(excerpt), 32...34)
        model.move(-1, extend: false, in: excerpt)
        XCTAssertEqual(model.pick(excerpt), 33...33)
    }

    func testPeekShowsSixLinesAroundTheCursorOnlyWhenTheCardIsTall() throws {
        let excerpt = try excerpt(lines: 1...18, picked: [10, 10])
        let model = QuizCardModel()

        XCTAssertTrue(model.peeks(excerpt, rowHeight: 19, chrome: 160, pane: 800))
        XCTAssertFalse(model.peeks(excerpt, rowHeight: 19, chrome: 160, pane: 1400))
        XCTAssertEqual(model.window(excerpt).compactMap(\.n), [7, 8, 9, 10, 11, 12])
        model.cursor = 1
        XCTAssertEqual(model.window(excerpt).compactMap(\.n), [1, 2, 3, 4, 5, 6])
        model.full = true
        XCTAssertFalse(model.peeks(excerpt, rowHeight: 19, chrome: 160, pane: 800))
    }

    func testRevealAndConfidenceDecode() throws {
        let json = #"{"type": "request.quiz", "id": "s/elicitation/q", "props": {"request": "q", "type": "line", "state": "answered", "title": "Answered", "question": "q", "reveal": {"verdict": "wrong", "label": "Not this line", "yours": "line 32", "flag": "sure_wrong", "note": "n"}, "confidence": {"label": "How sure?", "levels": [{"value": "sure", "label": "sure", "key": "3"}], "enabled": false, "sends": true, "holdMs": 2000}}}"#
        let quiz = try XCTUnwrap(JSONDecoder().decode(SDUINode.self, from: Data(json.utf8)).quiz())

        XCTAssertFalse(quiz.asking)
        XCTAssertEqual(quiz.reveal?.flag, "sure_wrong")
        XCTAssertEqual(quiz.confidence?.levels.map(\.key), ["3"])
        XCTAssertEqual(quiz.confidence?.enabled, false)
    }

    func testDockedQuizIsTheDockedCardAndAnsweredOneIsATranscriptCard() throws {
        let asking = try JSONDecoder().decode(SDUINode.self, from: Data(#"{"type": "stack", "id": "s", "children": [{"type": "transcript", "id": "s/t"}, {"type": "request.quiz", "id": "s/elicitation/q", "props": {"dock": true}}]}"#.utf8))
        let answered = try JSONDecoder().decode(SDUINode.self, from: Data(#"{"type": "stack", "id": "s", "children": [{"type": "request.quiz", "id": "s/elicitation/q"}]}"#.utf8))

        XCTAssertEqual(TranscriptBlocks.docked(in: asking)?.id, "s/elicitation/q")
        XCTAssertNil(TranscriptBlocks.docked(in: answered))
        XCTAssertTrue(TranscriptBlocks.cardTypes.contains("request.quiz"))
    }

    func testQuizTranscriptTextCarriesTheQuestionAndTheVerdict() throws {
        let json = #"{"type": "transcript", "id": "s/t", "children": [{"type": "request.quiz", "id": "s/elicitation/q", "props": {"request": "q", "type": "line", "state": "answered", "title": "Answered", "question": "Which line?", "labels": {"skip": "Skip", "undo": "Undo", "sending": "Sending", "fold": "Fold", "answerHint": "", "you": "You:", "answer": "Answer:", "allLines": "All {n} lines", "secondsLeft": "{n} s left"}, "reveal": {"verdict": "wrong", "label": "Not this line", "yours": "line 32", "answer": "line 30", "why": "30 grows it."}}}]}"#
        let transcript = try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8))

        XCTAssertEqual(TranscriptBlocks.markdown("s/elicitation/q", in: transcript), "**Answered**\n\nWhich line?\n\n**Not this line** · You: line 32 · Answer: line 30\n\n30 grows it.")
    }

    func testComposerNeedsARatingFollowsThePlugin() throws {
        let node = { (state: String, confidence: String) in try JSONDecoder().decode(SDUINode.self, from: Data(("{\"type\": \"request.quiz\", \"id\": \"s/elicitation/q\", \"props\": {\"request\": \"q\", \"type\": \"line\", \"state\": \"" + state + "\", \"title\": \"t\", \"question\": \"q\", \"confidence\": " + confidence + "}}").utf8)) }

        XCTAssertEqual(try node("asking", #"{"label": "How sure?", "needsRating": true}"#).quiz()?.composerNeedsRating, true)
        XCTAssertEqual(try node("asking", #"{"label": "How sure?", "value": "sure", "needsRating": false}"#).quiz()?.composerNeedsRating, false)
        XCTAssertEqual(try node("answered", #"{"label": "How sure?", "needsRating": true}"#).quiz()?.composerNeedsRating, false)
    }
}
