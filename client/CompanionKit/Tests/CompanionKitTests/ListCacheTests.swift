import Foundation
import XCTest

import CompanionKit

@MainActor
final class ListCacheTests: XCTestCase {
    private let dir = URL.temporaryDirectory.appending(path: "list-cache-" + UUID().uuidString, directoryHint: .isDirectory)

    override func tearDown() {
        try? FileManager.default.removeItem(at: dir)
    }

    func testBacklogShowsCacheBeforeLoad() async throws {
        let cache = ListCache(dir: dir)
        let runner = BacklogRunner()
        let first = BacklogStore(workspace: "w", runner: runner, cache: cache, computer: "m")
        await first.refresh()
        XCTAssertEqual(runner.calls.first?.args?["slim"], .bool(true))
        let synced = try XCTUnwrap(first.syncedAt)

        runner.failure = .unreachable("context deadline exceeded")
        let second = BacklogStore(workspace: "w", runner: runner, cache: cache, computer: "m")
        XCTAssertTrue(second.loaded)
        XCTAssertEqual(second.tasks.map(\.id), first.tasks.map(\.id))
        await second.refresh()
        XCTAssertEqual(second.tasks.map(\.id), first.tasks.map(\.id))
        XCTAssertNotNil(second.error)
        XCTAssertNil(second.ageLine(now: synced.addingTimeInterval(60)))
        XCTAssertEqual(second.ageLine(now: synced.addingTimeInterval(601)), "as of " + ComputerConnection.clock(synced))
    }

    func testDocsKeepsListOnUnchanged() async throws {
        let cache = ListCache(dir: dir)
        let runner = ScriptedRunner(replies: [], sessions: [
            #"{"docs":[{"path":"/r/a.md","rel":"a.md"}],"truncated":true,"etag":"e1"}"#,
            #"{"unchanged":true,"etag":"e1"}"#,
        ])
        await DocsListStore(workspace: "w", session: nil, runner: runner, cache: cache, computer: "m").refresh()
        let store = DocsListStore(workspace: "w", session: nil, runner: runner, cache: cache, computer: "m")
        XCTAssertEqual(store.docs.map(\.rel), ["a.md"])
        await store.refresh()
        XCTAssertEqual(runner.sessionArgs, [.object(["since": .string("")]), .object(["since": .string(""), "ifNoneMatch": .string("e1")])])
        XCTAssertEqual(store.docs.map(\.rel), ["a.md"])
        XCTAssertTrue(store.truncated)
    }

    func testCorruptCacheIsIgnored() throws {
        let cache = ListCache(dir: dir)
        cache.save(Envelope(tasks: .array([.object(["title": .string("no id")])]), at: .now), ListCache.Key(computer: "m", workspace: "w", collection: "backlog"))
        XCTAssertFalse(BacklogStore(workspace: "w", runner: BacklogRunner(), cache: cache, computer: "m").loaded)
        let files = try XCTUnwrap(FileManager.default.enumerator(at: dir, includingPropertiesForKeys: nil)).compactMap { $0 as? URL }.filter { $0.pathExtension == "json" }
        XCTAssertEqual(files.count, 1)
        try Data("{garbage".utf8).write(to: files[0])
        XCTAssertFalse(BacklogStore(workspace: "w", runner: BacklogRunner(), cache: cache, computer: "m").loaded)
    }

    func testForgetDropsOneMacAndRefusesItsLateSaves() {
        let cache = ListCache(dir: dir)
        let keys = [ListCache.Key(computer: "m", workspace: "w", collection: "backlog"), ListCache.Key(computer: "m", workspace: "w2", collection: "backlog"), ListCache.Key(computer: "m2", workspace: "w", collection: "backlog")]
        for (value, key) in keys.enumerated() { cache.save(value, key) }
        XCTAssertEqual(keys.map { cache.load($0, as: Int.self) }, [0, 1, 2])
        cache.forget(computer: "m")
        cache.save(9, keys[0])
        XCTAssertEqual(keys.map { cache.load($0, as: Int.self) }, [nil, nil, 2])
        cache.allow(computer: "m")
        cache.save(9, keys[0])
        XCTAssertEqual(cache.load(keys[0], as: Int.self), 9)
    }

    func testTaskDetailComesFromTheSavedCopy() async throws {
        let cache = ListCache(dir: dir)
        let runner = ScriptedRunner(replies: [], sessions: [#"{"id":7,"title":"t","status":"todo","description":"body"}"#])
        _ = try await BacklogStore(workspace: "w", runner: runner, cache: cache, computer: "m").get(7)
        let store = BacklogStore(workspace: "w", runner: runner, cache: cache, computer: "m")
        XCTAssertEqual(store.saved(7)?.description, "body")
        XCTAssertNil(store.saved(8))
    }

    func testUnsyncedSessionsCopyNeedsNobody() async throws {
        let cache = ListCache(dir: dir)
        let waiting = #"{"sessions":[{"id":"a","status":"waiting"}],"removed":[],"cursor":"e.1"}"#
        let runner = ScriptedRunner(replies: [], sessions: [waiting, waiting])
        await AgentsStore(workspace: "w", runner: runner, computer: "m", cache: cache).refresh()
        let store = AgentsStore(workspace: "w", runner: runner, computer: "m", cache: cache)
        XCTAssertEqual(store.sessions.map(\.id), ["a"])
        XCTAssertEqual(store.needsInputCount, 0)
        await store.refresh()
        XCTAssertEqual(store.needsInputCount, 1)
    }

    func testOfflineColdStartShowsCachedWorkspaceAndBacklog() async throws {
        let computers = ComputerStore(file: nil, vault: MemoryTokenVault())
        try computers.save(Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621), token: "t")
        let device = DeviceIdentity(id: "d", name: "Phone", model: "iPhone")
        let online = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case BacklogCommand.list: (FakeTransport.result(!String(decoding: request.httpBody ?? Data(), as: UTF8.self).contains(#""status""#) ? #"{"tasks":[{"id":7,"title":"Retry","status":"todo"}]}"# : #"{"tasks":[]}"#), 200)
            case AgentCommand.sessions: (FakeTransport.result(#"{"sessions":[{"id":"a","status":"waiting"}],"removed":[],"cursor":"e.1"}"#), 200)
            default: (FakeTransport.result(helloJSON), 200)
            }
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let first = CompanionModel(store: computers, transport: online, device: device, listsDir: dir, sleep: { _ in })
        first.start()
        try await waitUntil { first.connection("mac1")?.state == .online }
        await first.backlog(computer: "mac1", workspace: "prj_00000000000000f1")?.refresh()
        await first.agents(computer: "mac1", workspace: "prj_00000000000000f1")?.refresh()
        first.connection("mac1")?.stop()

        let offline = FakeTransport(reply: { _ in throw URLError(.cannotConnectToHost) }, stream: { _ in throw URLError(.cannotConnectToHost) })
        let second = CompanionModel(store: computers, transport: offline, device: device, listsDir: dir, sleep: { _ in })
        let connection = try XCTUnwrap(second.connection("mac1"))
        XCTAssertEqual(connection.workspaces.map(\.name), ["shop"])
        XCTAssertTrue(connection.supports(AgentCommand.sessions))
        XCTAssertEqual(second.backlog(computer: "mac1", workspace: "prj_00000000000000f1")?.tasks.map(\.id), [7])
        XCTAssertEqual(second.agents(computer: "mac1", workspace: "prj_00000000000000f1")?.sessions.map(\.id), ["a"])
        XCTAssertTrue(second.needsYou().isEmpty)

        try second.remove("mac1")
        XCTAssertTrue(try FileManager.default.contentsOfDirectory(atPath: dir.path()).isEmpty)
    }

    func testSessionsCursorSurvivesRestart() async throws {
        let cache = ListCache(dir: dir)
        let runner = ScriptedRunner(replies: [], sessions: [
            #"{"sessions":[{"id":"a","status":"done"}],"removed":[],"cursor":"e.3"}"#,
            #"{"sessions":[{"id":"b","status":"running"}],"removed":[],"cursor":"e.4","delta":true}"#,
        ])
        await AgentsStore(workspace: "w", runner: runner, computer: "m", cache: cache).refresh()
        let store = AgentsStore(workspace: "w", runner: runner, computer: "m", cache: cache)
        XCTAssertEqual(store.sessions.map(\.id), ["a"])
        await store.refresh()
        XCTAssertEqual(runner.sessionArgs.last, .object(["since": .string("e.3")]))
        XCTAssertEqual(store.sessions.map(\.id), ["a", "b"])
    }
}

private struct Envelope: Codable {
    let tasks: JSONValue
    let at: Date
}
