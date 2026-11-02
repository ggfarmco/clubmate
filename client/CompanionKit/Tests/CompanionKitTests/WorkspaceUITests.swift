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
        server.connection.start()
        try await waitUntil { ui.view("cron.sidebar") != nil }
        XCTAssertTrue(server.connection.supportsUI)
        XCTAssertEqual(ui.sections.map(\.id), ["editor-nvim", "terminals", "agents", "backlog", "processes", "cron", "triggers", "docs"])

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
        let sections = try JSONDecoder().decode(JSONValue.self, from: fixture("sdui"))["sections"] ?? .array([])
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

    func testSendPassesTheEventAndItsResult() async throws {
        let server = try UIServer()
        let ui = try await server.started(["cron.sidebar"])
        let result = try await ui.send(slot: "cron.sidebar", nodeId: "jobs", action: "select", value: .string("cj_check"))
        XCTAssertEqual(result, .object(["ok": .bool(true)]))
        XCTAssertEqual(server.events, [.object(["workspace": .string("w"), "slot": .string("cron.sidebar"), "nodeId": .string("jobs"), "action": .string("select"), "value": .string("cj_check")])])
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
    let connection: ComputerConnection

    init() throws {
        let fixture = try JSONDecoder().decode(JSONValue.self, from: fixture("sdui"))
        let sections = fixture["sections"] ?? .array([])
        let views = fixture["views"]?.objectValue ?? [:]
        let (watches, gets, events, streams, disabled) = (watchLog, getLog, eventLog, streams, disabled)
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
                watches.add(Watch(stream: params["stream"]?.stringValue ?? "", slots: params["slots"]?.arrayValue?.compactMap(\.stringValue) ?? []))
                return (FakeTransport.result("null"), 200)
            case "uiGet":
                let slots = params["slots"]?.arrayValue?.compactMap(\.stringValue) ?? []
                gets.add(slots)
                let found = slots.compactMap { slot in
                    views[slot].map { JSONValue.object(["workspaceId": params["workspace"] ?? .null, "slot": .string(slot), "owner": .string("p"), "rev": .number(1), "tree": $0]) }
                }
                return (try JSONEncoder().encode(JSONValue.object(["result": .array(found)])), 200)
            case "uiEvent":
                events.add(params)
                return (FakeTransport.result(#"{"ok":true}"#), 200)
            default: return (Data(), 404)
            }
        }, stream: { _ in
            (200, AsyncThrowingStream {
                streams.add($0)
                $0.yield(Data("event: stream\ndata: {\"id\":\"s\(streams.values.count)\"}\n\n".utf8))
            })
        })
        connection = ComputerConnection(computer: Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621), token: "t", transport: transport, sleep: { _ in })
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

    func disable(_ plugin: String) {
        disabled.add(plugin)
        streams.values.last?.yield(Data("event: bus\ndata: {\"seq\":1,\"topic\":\"plugin.changed\",\"payload\":{\"id\":\"\(plugin)\",\"running\":false,\"enabled\":false}}\n\n".utf8))
    }

    func drop() {
        streams.values.last?.finish()
    }
}
