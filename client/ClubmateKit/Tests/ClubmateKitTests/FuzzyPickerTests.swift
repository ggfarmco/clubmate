import XCTest

import ClubmateKit

@MainActor
final class FuzzyPickerTests: XCTestCase {
    func testLocalPickerFiltersOrdersAndMoves() async throws {
        let items = ["main", "DUE-970-cards", "feature/device", "develop"].map { PickerItem(id: $0, label: $0) }
        let model = FuzzyPickerModel(placeholder: "", debounce: .zero, source: FuzzyPickerModel.local(items))

        model.update("dev")
        let first = await model.choose()

        XCTAssertEqual(model.items.map(\.id), ["develop", "feature/device"])
        XCTAssertEqual(first?.id, "develop")
        model.move(1)
        let second = await model.choose()
        XCTAssertEqual(second?.id, "feature/device")
        model.move(1)
        let wrapped = await model.choose()
        XCTAssertEqual(wrapped?.id, "develop")
    }

    func testRefsPickerAsksTheGitPluginAndKeepsWantedKinds() async throws {
        var calls: [CommandInvocation] = []
        let refs: JSONValue = .array([
            .object(["ref": .string("/r/.worktrees/a"), "kind": .string("worktree"), "label": .string("a")]),
            .object(["ref": .string("develop"), "kind": .string("branch"), "label": .string("develop")]),
        ])
        let run: FuzzyPickerModel.Run = { invocation in
            calls.append(invocation)
            return .success(refs)
        }
        let model = FuzzyPickerModel(placeholder: "", debounce: .zero, source: FuzzyPickerModel.refs(run: run, kinds: ["worktree"]))

        model.update("a")
        let chosen = await model.choose()

        XCTAssertEqual(calls, [CommandInvocation(id: "git.review-refs", args: .object(["query": .string("a")]))])
        XCTAssertEqual(model.items, [PickerItem(id: "/r/.worktrees/a", label: "a", detail: "worktree")])
        XCTAssertEqual(chosen?.id, "/r/.worktrees/a")
    }
}
