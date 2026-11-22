import XCTest

import CompanionKit

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

    func testTheBoardGivesTheNewestKeysOfTheTopPageOfTheAskedTabOnly() {
        func keys(_ scope: String) -> SessionKeyActions {
            SessionKeyActions(scope: scope, enabled: true, answers: [:], send: nil)
        }
        let (session, pushed, task) = (StackEntry(.session("agt_1")), StackEntry(.session("agt_3")), StackEntry(.task(7)))
        let board = SessionKeyBoard()
        let (agents, inbox, aside, top) = (UUID(), UUID(), UUID(), UUID())
        board.set(keys("agt_1|"), id: agents, tab: .agents, page: session)
        board.set(keys("agt_2|"), id: inbox, tab: .backlog, page: StackEntry(.session("agt_2")))
        XCTAssertEqual(board.keys(for: .backlog, top: StackEntry(.session("agt_2")))?.scope, "agt_2|")
        XCTAssertEqual(board.keys(for: .agents, top: session)?.scope, "agt_1|")
        XCTAssertNil(board.keys(for: .agents, top: task))
        board.set(keys("agt_9|"), id: aside, tab: .agents, page: session)
        XCTAssertEqual(board.keys(for: .agents, top: session)?.scope, "agt_9|")
        board.remove(aside)
        board.set(keys("agt_3|"), id: top, tab: .agents, page: pushed)
        board.set(keys("agt_1|req"), id: agents, tab: .agents, page: session)
        XCTAssertEqual(board.keys(for: .agents, top: pushed)?.scope, "agt_3|")
        board.remove(top)
        XCTAssertEqual(board.keys(for: .agents, top: session)?.scope, "agt_1|req")
        XCTAssertNil(board.keys(for: .wiki, top: nil))
    }
}
