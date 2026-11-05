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

    func testAFormWithoutNeedsAnySubmitsEmpty() {
        let form = FormState()
        XCTAssertFalse(form.unanswered)
        form.submit()
        XCTAssertEqual(form.submissions, 1)
    }
}
