import XCTest

import ClubmateProtocol

import ClubmateKit

final class SDUIPaneFocusTests: XCTestCase {
    func testFirstFocusableNodeInTreeOrderOwnsPaneFocus() {
        let tree = SDUINode(type: "stack", children: [
            SDUINode(type: "header", id: "h"),
            SDUINode(type: "scroll", children: [
                SDUINode(type: "stack", children: [SDUINode(type: "text"), SDUINode(type: "flow", id: "tr_1/flow")]),
                SDUINode(type: "lanes", id: "tr_1/lanes"),
                SDUINode(type: "table", id: "history"),
            ]),
        ])
        XCTAssertEqual(tree.paneFocusOwner, "tr_1/flow")
        XCTAssertNil(SDUINode(type: "stack", children: [SDUINode(type: "text")]).paneFocusOwner)
    }
}
