import XCTest

import ClubmateKit

@MainActor
final class DocsPickerTests: XCTestCase {
    func testKeepsPluginOrderRanksQueryAndMapsIcons() async throws {
        let list = try JSONDecoder().decode(JSONValue.self, from: Data(#"""
        {"docs":[
         {"path":"/w/notes/spec-plan.md","rel":"notes/spec-plan.md","kind":"markdown","modified":"2026-11-01T10:00:00Z","agent":true},
         {"path":"/w/report.html","rel":"report.html","kind":"artifact","modified":"2026-11-01T09:00:00Z","agent":false},
         {"path":"/w/README.md","rel":"README.md","kind":"markdown","modified":"2026-10-01T09:00:00Z","agent":false},
         {"path":"/w/specs/design.md","rel":"specs/design.md","kind":"markdown","modified":"2026-10-01T09:00:00Z","agent":false}
        ],"truncated":false}
        """#.utf8))
        var calls: [CommandInvocation] = []
        let picker = DocsPicker { calls.append($0); return .success(list) }
        let model = FuzzyPickerModel(placeholder: "", debounce: .zero, source: picker.source)

        model.update("")
        _ = await model.choose()
        XCTAssertEqual(model.items.map(\.label), ["notes/spec-plan.md", "report.html", "README.md", "specs/design.md"])
        XCTAssertEqual(model.items.map(\.icon), ["sparkles", "chart.bar.doc.horizontal", "doc.richtext", "doc.richtext"])

        model.update("spec")
        let picked = await model.choose()
        let chosen = try XCTUnwrap(picked)
        XCTAssertEqual(model.items.map(\.label), ["specs/design.md", "notes/spec-plan.md"])
        XCTAssertEqual(calls, [CommandInvocation(id: "docs.list", args: .object([:]))])
        XCTAssertEqual(DocsPicker.open(chosen), CommandInvocation(id: "docs.open", args: .object(["path": .string("/w/specs/design.md")])))
    }
}
