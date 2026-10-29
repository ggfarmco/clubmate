import XCTest

import ClubmateKit

@MainActor
final class FindInFilesTests: XCTestCase {
    func testStaleBatchesIgnored() async throws {
        let find = FakeFind(ids: ["a": "grep-1", "b": "grep-2"], held: ["grep-1"], polls: [
            "grep-1": [page("grep-1", [match("old.txt", 1)], done: true)],
            "grep-2": [page("grep-2", [match("new.txt", 1)], done: true)],
        ])
        let model = makeModel(find)

        model.update(query: "a")
        try await waitUntil { find.waiting["grep-1"] != nil }
        model.update(query: "b")
        try await waitUntil { model.state == .done }
        find.release("grep-1")
        try await waitUntil { find.answered.contains("grep-1") }
        try await Task.sleep(for: .milliseconds(50))

        XCTAssertEqual(model.groups.map(\.path), ["new.txt"])
        XCTAssertEqual(find.polls(of: "grep-1"), 1)
        XCTAssertTrue(find.calls.contains(CommandInvocation(id: "search.cancel", args: .object(["id": .string("grep-1")]))))
    }

    func testFlattenedMoveAndOpen() async throws {
        let find = FakeFind(ids: ["foo": "grep-1"], polls: [
            "grep-1": [
                page("grep-1", [match("a.txt", 1, col: 5), match("a.txt", 2, col: 6), match("b.txt", 3, col: 7)], done: false),
                page("grep-1", [match("b.txt", 4, col: 8)], done: true),
            ],
        ])
        var opened = false
        let model = makeModel(find, opened: { opened = true })

        model.update(query: "foo")
        try await waitUntil { model.state == .done }
        XCTAssertEqual(model.groups.map { $0.matches.count }, [2, 2])
        for _ in 0..<3 { model.move(1) }
        await model.open()

        XCTAssertEqual(find.calls.last, CommandInvocation(id: "editor.open", args: .object([
            "path": .string("/w/b.txt"), "line": .number(4), "col": .number(8),
        ])))
        XCTAssertTrue(opened)
    }

    func testQuickfixSendsAll() async throws {
        let find = FakeFind(ids: ["foo": "grep-1"], polls: [
            "grep-1": [page("grep-1", [match("a.txt", 1), match("a.txt", 2), match("b.txt", 3)], done: true)],
        ])
        let model = makeModel(find)

        model.update(query: "foo")
        try await waitUntil { model.state == .done }
        await model.quickfix()

        let call = try XCTUnwrap(find.calls.last)
        XCTAssertEqual(call.id, "editor.quickfix")
        XCTAssertEqual(call.args.objectValue?["title"], .string("Search: foo"))
        XCTAssertEqual(call.args.objectValue?["items"]?.arrayValue?.first, .object([
            "path": .string("/w/a.txt"), "line": .number(1), "col": .number(1), "text": .string("text"),
        ]))
        XCTAssertEqual(call.args.objectValue?["items"]?.arrayValue?.count, 3)
    }

    private func makeModel(_ find: FakeFind, opened: @escaping @MainActor () -> Void = {}) -> FindInFilesModel {
        FindInFilesModel(debounce: .zero, previewDebounce: .zero, run: find.run, dismiss: {}, report: { _ in }, opened: opened)
    }

    private func match(_ path: String, _ line: Int, col: Int = 1) -> JSONValue {
        .object([
            "path": .string(path), "absPath": .string("/w/" + path), "line": .number(Double(line)), "col": .number(Double(col)),
            "text": .string("text"), "ranges": .array([.array([.number(0), .number(2)])]),
        ])
    }

    private func page(_ id: String, _ matches: [JSONValue], done: Bool) -> JSONValue {
        .object(["id": .string(id), "matches": .array(matches), "next": .number(Double(matches.count)), "done": .bool(done)])
    }
}

@MainActor
private final class FakeFind {
    private let ids: [String: String]
    private let held: Set<String>
    private var pages: [String: [JSONValue]]
    private(set) var calls: [CommandInvocation] = []
    private(set) var waiting: [String: CheckedContinuation<Void, Never>] = [:]
    private(set) var answered: Set<String> = []

    init(ids: [String: String], held: Set<String> = [], polls: [String: [JSONValue]]) {
        self.ids = ids
        self.held = held
        pages = polls
    }

    func polls(of id: String) -> Int {
        calls.filter { $0.id == "search.poll" && $0.args.objectValue?["id"] == .string(id) }.count
    }

    func release(_ id: String) {
        waiting.removeValue(forKey: id)?.resume()
    }

    func run(_ invocation: CommandInvocation) async -> Result<JSONValue, ClubmateError> {
        calls.append(invocation)
        let args = invocation.args.objectValue ?? [:]
        switch invocation.id {
        case "search.start":
            return .success(.object(["id": .string(ids[args["query"]?.stringValue ?? ""] ?? "grep-0")]))
        case "search.poll":
            let id = args["id"]?.stringValue ?? ""
            if held.contains(id), !answered.contains(id) {
                await withCheckedContinuation { waiting[id] = $0 }
            }
            answered.insert(id)
            guard var queue = pages[id], !queue.isEmpty else {
                return .success(.object(["id": .string(id), "matches": .array([]), "next": .number(0), "done": .bool(true)]))
            }
            let reply = queue.removeFirst()
            pages[id] = queue
            return .success(reply)
        default:
            return .success(.object([:]))
        }
    }
}
