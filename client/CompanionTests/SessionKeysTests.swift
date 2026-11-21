import XCTest

@testable import Companion

@MainActor
final class SessionKeysTests: XCTestCase {
    func testKeysCompareByScopeAndLabelsNotClosures() {
        func keys(scope: String = "agt_1|req", answers: [Int: String] = [1: "Allow once"]) -> SessionKeyActions {
            SessionKeyActions(scope: scope, enabled: true, answers: answers.mapValues { label in KeyedAction(label: label) {} }, send: KeyedAction(label: "Send") {})
        }

        XCTAssertEqual(keys(), keys())
        XCTAssertNotEqual(keys(), keys(scope: "agt_2|req"))
        XCTAssertNotEqual(keys(), keys(answers: [1: "Allow once", 3: "Reject"]))
    }
}
