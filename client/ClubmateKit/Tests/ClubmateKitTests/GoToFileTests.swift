import XCTest

import ClubmateKit

@MainActor
final class GoToFileTests: XCTestCase {
    func testGoToFileDropsStaleReplies() async throws {
        let search = FakeSearch(held: ["ma"], replies: [
            "ma": [match("main.go"), match("map.go")],
            "mai": [match("main.go")],
        ])
        let model = GoToFileModel(debounce: .zero, retry: .zero, run: search.run, dismiss: {}, report: { _ in })

        model.update("ma")
        try await waitUntil { search.waiting["ma"] != nil }
        model.update("mai")
        try await waitUntil { model.results.map(\.path) == ["main.go"] }
        search.release("ma")
        try await waitUntil { search.answered.contains("ma") }
        try await Task.sleep(for: .milliseconds(50))

        XCTAssertEqual(model.results.map(\.path), ["main.go"])
        XCTAssertEqual(model.query, "mai")
    }

    func testGoToFileChooseOpensAtLine() async throws {
        let search = FakeSearch(replies: ["x.go:7": [match("x.go", line: 7)]])
        var dismissed = false
        let model = GoToFileModel(debounce: .zero, retry: .zero, run: search.run, dismiss: { dismissed = true }, report: { _ in })

        model.update("x.go:7")
        try await waitUntil { !model.results.isEmpty }
        await model.choose()

        XCTAssertTrue(dismissed)
        XCTAssertEqual(search.calls.last, CommandInvocation(id: "editor.open", args: .object(["path": .string("/w/x.go"), "line": .number(7)])))
    }

    private func match(_ path: String, line: Int? = nil) -> JSONValue {
        var fields: [String: JSONValue] = ["path": .string(path), "absPath": .string("/w/" + path), "positions": .array([.number(0)])]
        if let line { fields["line"] = .number(Double(line)) }
        return .object(fields)
    }
}

@MainActor
private final class FakeSearch {
    private let held: Set<String>
    private let replies: [String: [JSONValue]]
    private(set) var calls: [CommandInvocation] = []
    private(set) var waiting: [String: CheckedContinuation<Void, Never>] = [:]
    private(set) var answered: Set<String> = []

    init(held: Set<String> = [], replies: [String: [JSONValue]]) {
        self.held = held
        self.replies = replies
    }

    func run(_ invocation: CommandInvocation) async -> Result<JSONValue, ClubmateError> {
        calls.append(invocation)
        guard invocation.id == "search.files" else { return .success(.object([:])) }
        let query = invocation.args.objectValue?["query"]?.stringValue ?? ""
        if held.contains(query) {
            await withCheckedContinuation { waiting[query] = $0 }
        }
        answered.insert(query)
        return .success(.object(["matches": .array(replies[query] ?? []), "truncated": .bool(false), "capped": .bool(false)]))
    }

    func release(_ query: String) {
        waiting.removeValue(forKey: query)?.resume()
    }
}
