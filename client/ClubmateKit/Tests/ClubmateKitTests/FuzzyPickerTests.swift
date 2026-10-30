import XCTest

import ClubmateKit

@MainActor
final class FuzzyPickerTests: XCTestCase {
    func testLocalPickerFiltersOrdersAndMoves() async throws {
        let items = ["main", "DUE-970-cards", "feature/device", "develop"].map { PickerItem(id: $0, label: $0) }
        let model = FuzzyPickerModel(placeholder: "", debounce: .zero, source: FuzzyPickerModel.local { items })

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
}
