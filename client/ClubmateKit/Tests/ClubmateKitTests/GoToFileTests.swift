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

    func testGoToFileChooseWaitsForCurrentQuery() async throws {
        let search = FakeSearch(replies: ["ma": [match("map.go"), match("main.go")], "mai": [match("main.go")]])
        let model = GoToFileModel(debounce: .milliseconds(50), retry: .zero, run: search.run, dismiss: {}, report: { _ in })

        model.update("ma")
        try await waitUntil { model.results.count == 2 }
        model.update("mai")
        await model.choose()

        XCTAssertEqual(search.calls.last?.args.objectValue?["path"], .string("/w/main.go"))
    }

    func testGoToFileRetriesWhileIndexing() async throws {
        let search = FakeSearch(indexing: 2, replies: ["": [match("main.go")]])
        let model = GoToFileModel(debounce: .zero, retry: .milliseconds(10), run: search.run, dismiss: {}, report: { _ in })

        model.update("")
        try await waitUntil { model.results.map(\.path) == ["main.go"] }
        XCTAssertEqual(search.calls.count, 3)
        XCTAssertEqual(model.status, .ready)

        let stuck = FakeSearch(indexing: .max, replies: [:])
        let stopped = GoToFileModel(debounce: .zero, retry: .milliseconds(10), run: stuck.run, dismiss: {}, report: { _ in })
        stopped.update("")
        try await waitUntil { stuck.calls.count >= 2 }
        XCTAssertEqual(stopped.status, .indexing)
        stopped.cancel()
        let calls = stuck.calls.count
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertEqual(stuck.calls.count, calls)
    }

    func testGoToFileHighlightsScalars() {
        let match = GoToFileMatch(path: "a/𝒳b.go", absPath: "/w/a/𝒳b.go", positions: [0, 2, 3])

        XCTAssertEqual(match.nameStart, 2)
        XCTAssertEqual(match.runs(in: 2..<7), [HighlightRun(text: "𝒳b", matched: true), HighlightRun(text: ".go", matched: false)])
        XCTAssertEqual(match.runs(in: 0..<1), [HighlightRun(text: "a", matched: true)])
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
    private var indexing: Int
    private(set) var calls: [CommandInvocation] = []
    private(set) var waiting: [String: CheckedContinuation<Void, Never>] = [:]
    private(set) var answered: Set<String> = []

    init(held: Set<String> = [], indexing: Int = 0, replies: [String: [JSONValue]]) {
        self.held = held
        self.indexing = indexing
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
        if indexing > 0 {
            indexing -= 1
            return .success(.object(["matches": .array([]), "indexing": .bool(true)]))
        }
        return .success(.object(["matches": .array(replies[query] ?? []), "truncated": .bool(false), "capped": .bool(false)]))
    }

    func release(_ query: String) {
        waiting.removeValue(forKey: query)?.resume()
    }
}
