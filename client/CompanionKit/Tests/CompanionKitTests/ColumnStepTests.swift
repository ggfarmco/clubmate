import Foundation
import XCTest

import CompanionKit

final class ColumnStepTests: XCTestCase {
    func testListSitsBesideOnlyWhenTheDetailKeeps640() {
        XCTAssertEqual([1210, 1376, 1032, 834].map { ColumnStep.Room(width: $0) }, [.beside, .beside, .beside, .over])
    }

    func testEachTabTogglesItsOwnTwoStates() {
        var stacks = TabStacks()
        stacks.toggleColumns(for: .backlog)
        XCTAssertEqual([stacks.column(for: .backlog), stacks.column(for: .agents)], [.detail, .listDetail])
        stacks.toggleColumns(for: .backlog)
        XCTAssertEqual(stacks.column(for: .backlog), .listDetail)
    }

    func testSavedThreeStepStateReadsAsListAndDetail() throws {
        let steps = try JSONDecoder().decode([ColumnStep].self, from: Data(#"["all","detail","listDetail"]"#.utf8))
        XCTAssertEqual(steps, [.listDetail, .detail, .listDetail])
    }
}
