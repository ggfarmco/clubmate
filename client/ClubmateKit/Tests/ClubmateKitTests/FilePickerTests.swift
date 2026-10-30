import XCTest

import ClubmateKit

@MainActor
final class FilePickerTests: XCTestCase {
    private var paths: [String] = []

    func testFilePickerDropsStaleReplies() async throws {
        let search = FakeSearch(held: ["ma"], replies: [
            "ma": [match("main.go"), match("map.go")],
            "mai": [match("main.go")],
        ])
        let model = FilePickerModel(debounce: .zero, retry: .zero, run: search.run, dismiss: {}) { self.paths.append($0) }

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

    func testComposerAttachUsesFilePicker() async throws {
        XCTAssertEqual(ComposerModel().handle(.attach), .attachFile)
        let search = FakeSearch(replies: ["x": [match("x.go")]])
        var dismissed = false
        var picked: [UIAction.Trigger] = []
        let model = FilePickerModel(debounce: .zero, retry: .zero, run: search.run, dismiss: { dismissed = true }) { picked.append(.attachFile($0)) }

        model.update("x")
        try await waitUntil { !model.results.isEmpty }
        await model.choose()

        XCTAssertTrue(dismissed)
        XCTAssertEqual(picked, [.named("attachFile", .object(["path": .string("/w/x.go")]))])
        XCTAssertEqual(search.calls.map(\.id), ["search.files"])
    }

    func testFilePickerChooseWaitsForCurrentQuery() async throws {
        let search = FakeSearch(replies: ["ma": [match("map.go"), match("main.go")], "mai": [match("main.go")]])
        let model = FilePickerModel(debounce: .milliseconds(50), retry: .zero, run: search.run, dismiss: {}) { self.paths.append($0) }

        model.update("ma")
        try await waitUntil { model.results.count == 2 }
        model.update("mai")
        await model.choose()

        XCTAssertEqual(paths, ["/w/main.go"])
    }

    func testFilePickerRetriesWhileIndexing() async throws {
        let search = FakeSearch(indexing: 2, replies: ["": [match("main.go")]])
        let model = FilePickerModel(debounce: .zero, retry: .milliseconds(10), run: search.run, dismiss: {}) { self.paths.append($0) }

        model.update("")
        try await waitUntil { model.results.map(\.path) == ["main.go"] }
        XCTAssertEqual(search.calls.count, 3)
        XCTAssertEqual(model.status, .ready)

        let stuck = FakeSearch(indexing: .max, replies: [:])
        let stopped = FilePickerModel(debounce: .zero, retry: .milliseconds(10), run: stuck.run, dismiss: {}) { self.paths.append($0) }
        stopped.update("")
        try await waitUntil { stuck.calls.count >= 2 }
        XCTAssertEqual(stopped.status, .indexing)
        stopped.cancel()
        let calls = stuck.calls.count
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertEqual(stuck.calls.count, calls)
    }

    func testFilePickerCancelDuringPendingChoosePicksNothing() async throws {
        let search = FakeSearch(held: ["ab"], replies: ["a": [match("a.go")], "ab": [match("ab.go")]])
        var dismissed = false
        let model = FilePickerModel(debounce: .zero, retry: .zero, run: search.run, dismiss: { dismissed = true }) { self.paths.append($0) }

        model.update("a")
        try await waitUntil { !model.results.isEmpty }
        model.update("ab")
        try await waitUntil { search.waiting["ab"] != nil }
        let choosing = Task { await model.choose() }
        try await waitUntil { model.choosing }
        model.cancel()
        search.release("ab")
        await choosing.value

        XCTAssertFalse(dismissed)
        XCTAssertEqual(paths, [])
    }

    func testFilePickerChooseWhileIndexingPicksNothing() async throws {
        let search = FakeSearch(indexing: 2, replies: ["": [match("main.go")]])
        let model = FilePickerModel(debounce: .zero, retry: .milliseconds(30), run: search.run, dismiss: {}) { self.paths.append($0) }

        model.update("")
        try await waitUntil { model.status == .indexing }
        await model.choose()
        try await waitUntil { !model.results.isEmpty }
        try await Task.sleep(for: .milliseconds(50))

        XCTAssertEqual(paths, [])
    }

    func testFilePickerHighlightsScalars() {
        let match = FilePickerMatch(path: "a/𝒳b.go", absPath: "/w/a/𝒳b.go", positions: [0, 2, 3])

        XCTAssertEqual(match.nameStart, 2)
        XCTAssertEqual(match.runs(in: 2..<7), [HighlightRun(text: "𝒳b", matched: true), HighlightRun(text: ".go", matched: false)])
        XCTAssertEqual(match.runs(in: 0..<1), [HighlightRun(text: "a", matched: true)])
    }

    private func match(_ path: String) -> JSONValue {
        .object(["path": .string(path), "absPath": .string("/w/" + path), "positions": .array([.number(0)])])
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
