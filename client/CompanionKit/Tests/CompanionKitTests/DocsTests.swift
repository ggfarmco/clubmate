import Foundation
import XCTest

import CompanionKit

@MainActor
final class DocsTests: XCTestCase {
    func testOpenPushesAndBackPopsDownToOnePage() async {
        let server = DocsServer()
        let store = DocsStore(connection: server.connection, workspace: "w")
        await store.open("/r/a.md")
        await store.open("/r/b.md")
        XCTAssertEqual(store.stack.map(\.title), ["a", "b"])
        store.back()
        store.back()
        XCTAssertEqual(store.stack.map(\.path), ["/r/a.md"])
    }

    func testOpenKeepsTheAnchorOnThePage() async {
        let server = DocsServer()
        let store = DocsStore(connection: server.connection, workspace: "w")
        await store.open("/r/b.md#intro")
        XCTAssertEqual(server.reads, ["/r/b.md"])
        XCTAssertEqual(store.stack.last?.anchor, "intro")
        await store.open("/r/b.md#usage")
        XCTAssertEqual(server.reads, ["/r/b.md"])
        XCTAssertEqual(store.stack.map(\.anchor), ["usage"])
        XCTAssertFalse(store.stack[0].isArtifact)
    }

    func testOpenArtifactKeepsTheAnchorAndKind() async {
        let server = DocsServer()
        let store = DocsStore(connection: server.connection, workspace: "w")
        await store.open("/r/report.html#q3")
        XCTAssertEqual(server.reads, ["/r/report.html"])
        XCTAssertEqual(store.stack.last?.anchor, "q3")
        XCTAssertEqual(store.stack.last?.isArtifact, true)
    }

    func testChangedRefetchesOnlyTheOpenPathAtAHigherRev() async throws {
        let server = DocsServer()
        let store = DocsStore(connection: server.connection, workspace: "w")
        await store.open("/r/a.md")
        server.revs.add(3)
        store.handle(.event(BusEvent(seq: 1, topic: "docs.changed", payload: .object(["workspace": .string("other"), "path": .string("/r/a.md"), "rev": .number(3)]))))
        await store.changed(path: "/r/b.md", rev: 3)
        await store.changed(path: "/r/a.md", rev: 1)
        XCTAssertEqual(server.reads, ["/r/a.md"])
        store.handle(.event(BusEvent(seq: 2, topic: "docs.changed", payload: .object(["workspace": .string("w"), "path": .string("/r/a.md"), "rev": .number(3)]))))
        try await waitUntil { store.stack.last?.rev == 3 }
        XCTAssertEqual(store.stack.count, 1)
        XCTAssertEqual(server.reads, ["/r/a.md", "/r/a.md"])
    }

    func testBackRefetchesAPageThatChangedUnderTheTop() async throws {
        let server = DocsServer()
        let store = DocsStore(connection: server.connection, workspace: "w")
        await store.open("/r/a.md")
        await store.open("/r/b.md")
        server.revs.add(2)
        await store.changed(path: "/r/a.md", rev: 2)
        XCTAssertEqual(server.reads, ["/r/a.md", "/r/b.md"])
        store.back()
        try await waitUntil { store.stack.last?.rev == 2 }
        XCTAssertEqual(store.stack.map(\.path), ["/r/a.md"])
    }

    func testReadErrorSetsErrorAndKeepsTheStack() async {
        let server = DocsServer()
        let store = DocsStore(connection: server.connection, workspace: "w")
        await store.open("/r/a.md")
        await store.open("/etc/passwd.md")
        XCTAssertEqual(store.error, "not in workspace")
        XCTAssertEqual(store.stack.map(\.path), ["/r/a.md"])
    }

    func testDocsListLoadsInPluginOrderAndMatchesOnRel() async throws {
        let runner = DocsListRunner()
        let store = DocsListStore(workspace: "w", session: "agt_0000000000000001", runner: runner)
        await store.refresh()
        XCTAssertEqual(runner.calls.map(\.args), [.object(["session": .string("agt_0000000000000001")])])
        XCTAssertEqual(store.docs.map(\.rel), ["docs/checkout-retries.md", "docs/conversion.html"])
        let all = DocsListStore(workspace: "w", session: nil, runner: runner)
        await all.refresh()
        XCTAssertNil(runner.calls.last?.args)
        XCTAssertEqual(all.docs.filter(\.agent).count, 3)
        XCTAssertEqual(all.docs[1].isArtifact, true)
        XCTAssertEqual(all.docs[1].modified, Date.parseRFC3339("2026-11-01T09:12:00.250Z"))
        XCTAssertNil(all.docs.last?.modified)
        XCTAssertEqual(all.matching("PAY").map(\.rel), ["docs/payments.md", "docs/payments-flow.mmd"])
        XCTAssertEqual(all.matching(" ").count, 10)
    }

    func testDocsListRefreshesOnDocsAndAgentsChangesForItsWorkspace() async throws {
        let runner = DocsListRunner()
        let store = DocsListStore(workspace: "w", session: nil, runner: runner)
        store.handle(.event(BusEvent(seq: 1, topic: "docs.changed", payload: .object(["workspace": .string("other"), "path": .string("/a.md"), "rev": .number(2)]))))
        store.handle(.event(BusEvent(seq: 2, topic: "backlog.changed", payload: .object(["workspace": .string("w")]))))
        store.handle(.event(BusEvent(seq: 2, topic: "agents.changed", payload: .object(["workspace": .string("w"), "kind": .string("resolved")]))))
        XCTAssertTrue(runner.calls.isEmpty)
        store.handle(.event(BusEvent(seq: 3, topic: "docs.changed", payload: .object(["workspace": .string("w"), "path": .string("/a.md"), "rev": .number(2)]))))
        try await waitUntil { runner.calls.count == 1 }
        store.handle(.event(BusEvent(seq: 4, topic: "agents.changed", payload: .object(["workspace": .string("w"), "session": .string("agt_1"), "kind": .string("status")]))))
        try await waitUntil { runner.calls.count == 2 }
        XCTAssertTrue(store.loaded)
    }
}

@MainActor
private final class DocsListRunner: CommandRunner {
    private(set) var calls: [(id: String, args: JSONValue?)] = []

    func command(_ id: String, workspace: String?, args: JSONValue?) async throws -> JSONValue {
        calls.append((id, args))
        let list = try JSONDecoder().decode(JSONValue.self, from: fixture("docs-list"))
        guard let session = args?["session"]?.stringValue else { return list }
        let paths = Set(list["sessions"]?[session]?.arrayValue?.compactMap(\.stringValue) ?? [])
        return .object(["docs": .array((list["docs"]?.arrayValue ?? []).filter { paths.contains($0["path"]?.stringValue ?? "") }), "truncated": .bool(false)])
    }
}

@MainActor
private final class DocsServer {
    let revs = Recorder<Int>()
    private let transport: FakeTransport
    let connection: ComputerConnection

    init() {
        let revs = revs
        transport = FakeTransport(reply: { request in
            let path = Self.path(request)
            guard path.hasPrefix("/r/") else { return (Data(#"{"error":{"code":"command_failed","message":"not in workspace"}}"#.utf8), 200) }
            let name = URL(filePath: path).deletingPathExtension().lastPathComponent
            let value = revs.values.last ?? 1
            let kind = path.hasSuffix(".html") ? #","kind":"artifact""# : ""
            return (FakeTransport.result(#"{"path":"\#(path)","title":"\#(name)","html":"<p>\#(value)</p>","rev":\#(value)\#(kind)}"#), 200)
        })
        connection = ComputerConnection(computer: Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621), token: "t", transport: transport)
    }

    var reads: [String] {
        transport.requests.filter { FakeTransport.rpc($0) == "docs.read" }.map(Self.path)
    }

    private nonisolated static func path(_ request: URLRequest) -> String {
        guard let body = request.httpBody, let value = try? JSONDecoder().decode(JSONValue.self, from: body) else { return "" }
        return value["params"]?["args"]?["path"]?.stringValue ?? ""
    }
}
