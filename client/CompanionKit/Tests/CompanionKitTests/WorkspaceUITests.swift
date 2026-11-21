import Foundation
import Synchronization
import XCTest

import CompanionKit

@MainActor
final class WorkspaceUITests: XCTestCase {
    func testShowWatchesTheUnionAndFetchesOnlyWhatIsNew() async throws {
        let server = try UIServer()
        let ui = server.connection.workspaceUI("w")
        XCTAssertTrue(ui === server.connection.workspaceUI("w"))
        ui.show(["cron.sidebar"], owner: "list")
        XCTAssertFalse(ui.sectionsLoaded)
        server.connection.start()
        try await waitUntil { ui.view("cron.sidebar") != nil }
        XCTAssertTrue(server.connection.supportsUI)
        XCTAssertEqual(ui.sections.map(\.id), ["editor-nvim", "terminals", "agents", "backlog", "processes", "cron", "triggers", "docs"])
        XCTAssertTrue(ui.sectionsLoaded)

        ui.show(["cron.main"], owner: "detail")
        try await waitUntil { ui.view("cron.main") != nil }
        ui.hide(owner: "list")
        try await waitUntil { server.watches.count == 3 }
        ui.hide(owner: "detail")
        try await waitUntil { server.watches.last?.slots == [] }

        XCTAssertEqual(server.watches.map(\.slots), [["cron.sidebar"], ["cron.main", "cron.sidebar"], ["cron.main"], []])
        XCTAssertEqual(Set(server.watches.map(\.stream)), ["s1"])
        XCTAssertEqual(server.gets, [["cron.sidebar"], ["cron.main"]])
        server.connection.stop()
    }

    func testHigherRevReplacesAndLowerIsIgnored() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.main"])
        server.push("cron.main", rev: 5, tree: #"{"type":"text","props":{"text":"five"}}"#)
        server.push("cron.main", rev: 4, tree: #"{"type":"text","props":{"text":"four"}}"#)
        server.push("cron.main", rev: 9, tree: #"{"type":"text","props":{"text":"other"}}"#, workspace: "w2")
        server.push("cron.main", rev: 6, tree: #"{"type":"text","props":{"text":"six"}}"#)
        try await waitUntil { ui.view("cron.main")?.rev == 6 }
        XCTAssertEqual(ui.view("cron.main")?.tree.string("text"), "six")
        server.connection.stop()
    }

    func testReconnectRewatches() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.main"])
        server.push("cron.main", rev: 7, tree: #"{"type":"text","props":{"text":"before"}}"#)
        try await waitUntil { ui.view("cron.main")?.rev == 7 }

        server.drop()

        try await waitUntil { server.watches.last?.stream == "s2" && ui.view("cron.main")?.rev == 1 }
        XCTAssertEqual(server.watches.last?.slots, ["cron.main"])
        XCTAssertEqual(server.gets.last, ["cron.main"])
        XCTAssertEqual(ui.view("cron.main")?.tree.type, "stack")
        server.connection.stop()
    }

    func testAFailedWatchIsRetried() async throws {
        let server = try UIServer()
        server.failWatches(2)
        let ui = try await server.started(["cron.main"])
        XCTAssertNil(ui.failure)
        XCTAssertEqual(server.watches.map(\.slots), [["cron.main"]])
        server.connection.stop()
    }

    func testASlotGoneAfterAReconnectLosesItsTree() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.main"])

        server.remove("cron.main")
        server.drop()

        try await waitUntil { server.watches.last?.stream == "s2" && ui.view("cron.main") == nil }
        server.connection.stop()
    }

    func testPluginChangeRefetchesTheSections() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.main"])
        XCTAssertTrue(ui.sections.contains { $0.id == "cron" })

        server.disable("cron")

        try await waitUntil { !ui.sections.contains { $0.id == "cron" } }
        XCTAssertEqual(ui.sections.count, 7)
        server.connection.stop()
    }

    func testMoreListsTheSectionsWithoutANativeScreen() throws {
        let wiki = JSONValue.object(["id": .string("wiki"), "title": .string("Wiki"), "icon": .string("book"), "order": .number(40), "scope": .string("workspace"), "autoNew": .bool(false), "plugin": .string("wiki")])
        let sections = JSONValue.array((try JSONDecoder().decode(JSONValue.self, from: fixture("sdui"))["sections"]?.arrayValue ?? []) + [wiki])
        let decoded = try JSONDecoder().decode([SectionInfo].self, from: JSONEncoder().encode(sections))
        XCTAssertEqual(PhoneSections.more(decoded.reversed()).map(\.id), ["processes", "cron", "triggers"])
    }

    func testASectionWithoutASidebarOpensItsMainSlot() {
        let main = SlotView(workspaceId: "w", slot: "cron.main", owner: "cron", rev: 1, tree: SDUINode(type: "stack"))
        let message = SlotView(workspaceId: "w", slot: "cron.sidebar", owner: "cron", rev: 1, tree: SDUINode(type: "empty", props: ["text": .string("No cron jobs yet.")]))
        let cleared = SlotView(workspaceId: "w", slot: "cron.sidebar", owner: "cron", rev: 2, tree: SDUINode(type: "empty", props: ["text": .string("")]))
        XCTAssertEqual(PhoneSections.root("cron", sidebar: message, main: main), "cron.sidebar")
        XCTAssertEqual(PhoneSections.root("cron", sidebar: cleared, main: main), "cron.main")
        XCTAssertEqual(PhoneSections.root("cron", sidebar: nil, main: main), "cron.main")
        XCTAssertNil(PhoneSections.root("cron", sidebar: nil, main: nil))
    }

    func testAWikiPageShowsOnlyTheMainSlotOfItsOwnPage() {
        func main(_ path: String) -> SDUINode {
            SDUINode(type: "stack", children: [SDUINode(type: "header"), SDUINode(type: "document", id: "page", props: ["path": .string(path)])])
        }
        XCTAssertTrue(PhoneSections.shows(main("/w/wiki/notes/guide.md"), page: "shop/notes/guide.md", path: ""))
        XCTAssertFalse(PhoneSections.shows(main("/w/wiki/notes/other.md"), page: "shop/notes/guide.md", path: ""))
        XCTAssertTrue(PhoneSections.shows(main("/w/wiki/notes/guide.md@abc1"), page: "shop/notes/guide.md", path: "/w/wiki/notes/guide.md"))
        XCTAssertFalse(PhoneSections.shows(main("/x/wiki/notes/guide.md"), page: "shop/notes/guide.md", path: "/w/wiki/notes/guide.md"))
        XCTAssertTrue(PhoneSections.shows(SDUINode(type: "empty"), page: "shop/notes/guide.md", path: ""))
    }

    func testAPageSeenBeforeShowsFromMemoryByItsPageAndSlot() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.sidebar", "cron.main"])
        var rev = 1
        func open(_ id: String, _ path: String, _ html: String) {
            rev += 1
            server.push("cron.sidebar", rev: rev, tree: UIServer.list(selected: id))
            server.push("cron.main", rev: rev, tree: UIServer.page(path, html))
        }
        open("shop/a.md", "/w/shop/a.md", "shop")
        open("farm/a.md", "/w/farm/a.md", "farm")
        open("shop/b.md", "/w/shop/b.md", "b1")
        try await waitUntil { ui.view("cron.main")?.rev == Int64(rev) }
        XCTAssertEqual(ui.page("shop/a.md", path: "", in: "cron.main")?.tree.string("html"), "shop")
        XCTAssertEqual(ui.page("farm/a.md", path: "", in: "cron.main")?.tree.string("html"), "farm")
        XCTAssertNil(ui.page("shop/c.md", path: "", in: "cron.main"))
        XCTAssertNil(ui.page("shop/a.md", path: "", in: "other.main"))

        open("shop/a.md", "/w/shop/a.md", "shop2")

        try await waitUntil { ui.page("shop/a.md", path: "", in: "cron.main")?.tree.string("html") == "shop2" }
        server.connection.stop()
    }

    func testAPageResendsAListTapSelectThatFailed() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.sidebar"])
        server.failEvents(1)
        let tap = Task { try await ui.send(slot: "cron.sidebar", nodeId: "jobs", action: "select", value: .string("cj_other")) }
        try await waitUntil { server.events.count == 1 }
        await ui.select("cj_other", in: "cron.sidebar")
        let tapped = await tap.result
        XCTAssertThrowsError(try tapped.get())
        XCTAssertEqual(server.events.map { $0["value"] }, [.string("cj_other"), .string("cj_other")])
        server.connection.stop()
    }

    func testUpdatingStaysUntilTheSidebarShowsTheSelect() async throws {
        let server = try UIServer(sleep: { if $0 == .seconds(2) { try await Task.sleep(for: .seconds(60)) } })
        let ui = try await server.started(["cron.sidebar"])
        await ui.select("cj_other", in: "cron.sidebar")
        try await waitUntil { ui.updating == ["cj_other"] }

        server.push("cron.sidebar", rev: 2, tree: UIServer.list(selected: "cj_other"))

        try await waitUntil { ui.updating.isEmpty }
        XCTAssertTrue(ui.missing.isEmpty)
        server.connection.stop()
    }

    func testAPageTheMacDidNotSelectIsMissingAndDropped() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.sidebar", "cron.main"])
        server.push("cron.sidebar", rev: 2, tree: UIServer.list(selected: "shop/a.md"))
        server.push("cron.main", rev: 2, tree: UIServer.page("/w/shop/a.md", "a"))
        server.push("cron.sidebar", rev: 3, tree: UIServer.list(selected: "shop/b.md"))
        server.push("cron.main", rev: 3, tree: UIServer.page("/w/shop/b.md", "b"))
        try await waitUntil { ui.view("cron.main")?.rev == 3 }

        await ui.select("shop/a.md", in: "cron.sidebar")

        try await waitUntil { ui.missing == ["shop/a.md"] }
        XCTAssertNil(ui.page("shop/a.md", path: "", in: "cron.main"))
        server.connection.stop()
    }

    func testASelectStillOnItsWayIsNotSentAgain() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.sidebar"])
        let first = Task { await ui.select("cj_other", in: "cron.sidebar") }
        await ui.select("cj_other", in: "cron.sidebar")
        await first.value
        XCTAssertEqual(server.events.map { $0["value"] }, [.string("cj_other")])
        server.connection.stop()
    }

    func testSendPassesTheEventAndItsResult() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.sidebar"])
        let result = try await ui.send(slot: "cron.sidebar", nodeId: "jobs", action: "select", value: .string("cj_check"))
        XCTAssertEqual(result, .object(["ok": .bool(true)]))
        let sent = try XCTUnwrap(server.events.first)
        XCTAssertEqual(server.events.count, 1)
        XCTAssertNotNil(sent["requestId"]?.stringValue.flatMap(UUID.init(uuidString:)))
        XCTAssertEqual(sent, .object(["workspace": .string("w"), "slot": .string("cron.sidebar"), "nodeId": .string("jobs"), "action": .string("select"), "value": .string("cj_check"), "requestId": sent["requestId"]!]))
        server.connection.stop()
    }
}

@MainActor
private final class UIServer {
    struct Watch: Equatable, Sendable {
        let stream: String
        let slots: [String]
    }

    private let watchLog = Recorder<Watch>()
    private let getLog = Recorder<[String]>()
    private let eventLog = Recorder<JSONValue>()
    private let streams = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
    private let disabled = Recorder<String>()
    private let removed = Recorder<String>()
    private let failing = Recorder<Bool>()
    private let failed = Recorder<Bool>()
    private let failingEvents = Recorder<Bool>()
    private let failedEvents = Recorder<Bool>()
    let connection: ComputerConnection

    init(sleep: @escaping @Sendable (Duration) async throws -> Void = { _ in }) throws {
        let fixture = try JSONDecoder().decode(JSONValue.self, from: fixture("sdui"))
        let sections = fixture["sections"] ?? .array([])
        let views = fixture["views"]?.objectValue ?? [:]
        let (watches, gets, events, streams, disabled, removed, failing, failed, failingEvents, failedEvents) = (watchLog, getLog, eventLog, streams, disabled, removed, failing, failed, failingEvents, failedEvents)
        let hello = #"{"computer":{"id":"mac1","name":"Mac Studio"},"version":"0.0.21","commands":[],"topics":[],"ui":true}"#
        let transport = FakeTransport(reply: { request in
            let body = (try? JSONDecoder().decode(JSONValue.self, from: request.httpBody ?? Data())) ?? .null
            let params = body["params"] ?? .null
            switch FakeTransport.rpc(request) {
            case "hello": return (FakeTransport.result(hello), 200)
            case "workspaces": return (FakeTransport.result(workspacesJSON), 200)
            case "uiSections":
                let off = Set(disabled.values)
                let on = sections.arrayValue?.filter { !off.contains($0["id"]?.stringValue ?? "") } ?? []
                return (try JSONEncoder().encode(JSONValue.object(["result": .array(on)])), 200)
            case "uiWatch":
                if failing.values.count > failed.values.count {
                    failed.add(true)
                    return (Data(), 503)
                }
                watches.add(Watch(stream: params["stream"]?.stringValue ?? "", slots: params["slots"]?.arrayValue?.compactMap(\.stringValue) ?? []))
                return (FakeTransport.result("null"), 200)
            case "uiGet":
                let slots = params["slots"]?.arrayValue?.compactMap(\.stringValue) ?? []
                gets.add(slots)
                let gone = Set(removed.values)
                let found = slots.filter { !gone.contains($0) }.compactMap { slot in
                    views[slot].map { JSONValue.object(["workspaceId": params["workspace"] ?? .null, "slot": .string(slot), "owner": .string("p"), "rev": .number(1), "tree": $0]) }
                }
                return (try JSONEncoder().encode(JSONValue.object(["result": .array(found)])), 200)
            case "uiEvent":
                events.add(params)
                if failingEvents.values.count > failedEvents.values.count {
                    failedEvents.add(true)
                    return (Data(), 503)
                }
                return (FakeTransport.result(#"{"ok":true}"#), 200)
            default: return (Data(), 404)
            }
        }, stream: { _ in
            (200, AsyncThrowingStream {
                streams.add($0)
                $0.yield(Data("event: stream\ndata: {\"id\":\"s\(streams.values.count)\"}\n\n".utf8))
            })
        })
        connection = ComputerConnection(computer: Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621), token: "t", transport: transport, sleep: sleep)
    }

    static func list(selected: String) -> String {
        #"{"type":"list","id":"jobs","props":{"selected":"\#(selected)"}}"#
    }

    static func page(_ path: String, _ html: String) -> String {
        #"{"type":"document","id":"page","props":{"path":"\#(path)","html":"\#(html)"}}"#
    }

    var watches: [Watch] { watchLog.values }
    var gets: [[String]] { getLog.values }
    var events: [JSONValue] { eventLog.values }

    func started(_ slots: Set<String>) async throws -> WorkspaceUI {
        let ui = connection.workspaceUI("w")
        ui.show(slots, owner: "screen")
        connection.start()
        try await waitUntil { slots.allSatisfy { ui.view($0) != nil } }
        return ui
    }

    func push(_ slot: String, rev: Int, tree: String, workspace: String = "w") {
        streams.values.last?.yield(Data("event: ui\ndata: {\"workspace\":\"\(workspace)\",\"slot\":\"\(slot)\",\"rev\":\(rev),\"tree\":\(tree)}\n\n".utf8))
    }

    func failEvents(_ count: Int) {
        for _ in 0..<count { failingEvents.add(true) }
    }

    func failWatches(_ count: Int) {
        for _ in 0..<count { failing.add(true) }
    }

    func remove(_ slot: String) {
        removed.add(slot)
    }

    func disable(_ plugin: String) {
        disabled.add(plugin)
        streams.values.last?.yield(Data("event: bus\ndata: {\"seq\":1,\"topic\":\"plugin.changed\",\"payload\":{\"id\":\"\(plugin)\",\"running\":false,\"enabled\":false}}\n\n".utf8))
    }

    func drop() {
        streams.values.last?.finish()
    }
}
