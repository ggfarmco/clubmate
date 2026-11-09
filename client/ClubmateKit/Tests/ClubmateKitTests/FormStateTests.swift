import XCTest

@testable import ClubmateKit

@MainActor
final class FormStateTests: XCTestCase {
    func testNeedsAnyHoldsSubmitUntilOneFieldHasAValue() {
        let form = FormState()
        form.needsAny = ["choice", "multi", "other"]
        form.values = ["multi": .array([]), "other": .string("  ")]
        XCTAssertTrue(form.unanswered)
        form.submit()
        XCTAssertEqual(form.submissions, 0)

        form.values["multi"] = .array([.string("a")])
        XCTAssertFalse(form.unanswered)
        form.submit()
        XCTAssertEqual(form.submissions, 1)
    }

    func testAPickMovesOnToTheNextUnansweredQuestion() {
        let form = FormState()
        form.questions = ["q1", "q2", "q3"]
        form.values = ["q1": .string("a"), "q2": .string("b")]
        XCTAssertEqual(form.question(after: "q1"), "q3")
        form.values["q3"] = .array([.string("c")])
        XCTAssertNil(form.question(after: "q1"))
        XCTAssertNil(form.question(after: "other"))
    }

    func testAFormWithoutNeedsAnySubmitsEmpty() {
        let form = FormState()
        XCTAssertFalse(form.unanswered)
        form.submit()
        XCTAssertEqual(form.submissions, 1)
    }
}
