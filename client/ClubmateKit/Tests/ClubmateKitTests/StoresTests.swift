import Darwin
import Foundation
import XCTest

import ClubmateKit

@MainActor
final class StoresTests: XCTestCase {
    private var core: TestCore?

    override func tearDown() async throws {
        await core?.stop()
        core = nil
    }

    func testTerminalsFlow() async throws {
        let client = try await startCore()
        let projects = ProjectsStore(client: client)
        try await projects.start()
        let project = try await projects.create(name: "demo", repos: [])
        XCTAssertEqual(projects.projects.map(\.id), [project.id])
        let other = try await client.createProject(name: "other")
        try await waitUntil { projects.projects.map(\.id).contains(other.id) }

        let store = ProjectUIStore(client: client, projectID: project.id)
        try await store.start()
        XCTAssertEqual(store.sections.map(\.id), ["terminals", "echo"])
        XCTAssertEqual(store.selectedSection, "terminals")
        let button = try await waitFor { store.slots["terminals.sidebar"]?.tree.find(id: "new") }
        _ = try await store.send(UIAction.event(for: button, trigger: .click, slot: "terminals.sidebar", projectID: project.id)).get()
        try await waitUntil { store.slots["terminals.main"]?.tree.type == "surface" }
        XCTAssertEqual(store.slots["terminals.sidebar"]?.tree.find(id: "terminals")?.items().map(\.label), ["Terminal 1"])

        let close = SDUINode(type: "button", id: "close")
        _ = try await store.send(UIAction.event(for: close, trigger: .click, slot: "terminals.main", projectID: project.id)).get()
        try await waitUntil { store.slots["terminals.main"]?.tree.type == "empty" }
        await store.stop()
        await projects.stop()
        await client.close()
    }

    func testSelectedSectionPersists() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        let first = ProjectUIStore(client: client, projectID: project.id)
        try await first.start()
        XCTAssertEqual(first.selectedSection, "terminals")
        XCTAssertNil(first.sidebarWidth)
        first.sidebarWidth = 240
        first.selectedSection = "echo"
        await first.stop()
        let state = try await client.uiStateGet(projectID: project.id)
        XCTAssertEqual(state, .object(["sidebarWidth": .number(240), "section": .string("echo")]))

        let second = ProjectUIStore(client: client, projectID: project.id)
        try await second.start()
        XCTAssertEqual(second.selectedSection, "echo")
        XCTAssertEqual(second.sidebarWidth, 240)
        await second.stop()
        await client.close()
    }

    func testPluginCrashEmptiesThenRerenders() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        let store = ProjectUIStore(client: client, projectID: project.id)
        try await store.start()
        try await waitUntil { store.slots["terminals.main"]?.tree.string("text") == "Create a terminal" }

        let pid = try await waitFor { try await terminalsPID(client) }
        XCTAssertEqual(kill(pid, SIGKILL), 0)
        try await waitUntil { mainText(store) == "" }
        try await waitUntil(within: .seconds(10)) { mainText(store).map { !$0.isEmpty } ?? false }
        await store.stop()
        await client.close()
    }

    func testReconnectRefetches() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        let store = ProjectUIStore(client: client, projectID: project.id)
        try await store.start()
        try await waitUntil { store.slots["terminals.main"] != nil }
        await client.close()
        try await waitUntil { !store.connected }

        guard let paths = core?.paths else { throw TestFailure(description: "no core") }
        let fresh = try await CoreClient.connect(paths: paths)
        try await store.reconnect(using: fresh)
        XCTAssertTrue(store.connected)
        let button = try XCTUnwrap(store.slots["terminals.sidebar"]?.tree.find(id: "new"))
        _ = try await store.send(UIAction.event(for: button, trigger: .click, slot: "terminals.sidebar", projectID: project.id)).get()
        try await waitUntil { store.slots["terminals.main"]?.tree.type == "surface" }
        await store.stop()
        await fresh.close()
    }

    func testStopDuringStartLeavesNoSubscription() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        let store = ProjectUIStore(client: client, projectID: project.id)
        let starting = Task { try await store.start() }
        await Task.yield()
        await store.stop()
        try await starting.value
        XCTAssertFalse(store.connected)

        let stream = client.subscribeNotifications()
        _ = try await client.commandRun(id: "terminals.new", projectID: project.id)
        let update = await firstUIUpdate(stream, within: .milliseconds(500))
        XCTAssertNil(update)
        await client.close()
    }

    func testRestartUnderTrafficKeepsReceivingUpdates() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        weak var released: ProjectUIStore?
        do {
            let store = ProjectUIStore(client: client, projectID: project.id)
            released = store
            let select = UIEvent(projectId: project.id, slot: "terminals.sidebar", nodeId: "terminals", action: "select", value: .string("x"))
            try await store.start()
            try await waitUntil { store.slots["terminals.main"] != nil }
            _ = try await client.uiSubscribe(projectID: project.id)
            let traffic = Task {
                while !Task.isCancelled { _ = try? await client.uiEvent(select) }
            }
            for _ in 0..<50 {
                await store.stop()
                let first = Task { try await store.start() }
                await Task.yield()
                await store.stop()
                try await store.start()
                try await first.value
            }
            traffic.cancel()
            await traffic.value
            let rev = try XCTUnwrap(store.slots["terminals.main"]?.rev)
            _ = try await client.uiEvent(select)
            try await waitUntil { (store.slots["terminals.main"]?.rev ?? 0) > rev }
            await store.stop()
        }
        try await waitUntil { released == nil }
        await client.close()
    }

    func testUIActionContract() {
        let rows: [(SDUINode, UIAction.Trigger, String, JSONValue)] = [
            (node("button", "new"), .click, "new", .null),
            (node("button", "b", action: "go"), .click, "go", .null),
            (node("list", "l"), .select("i1"), "select", .string("i1")),
            (node("list", "l", action: "pick"), .select("i1"), "pick", .string("i1")),
            (node("tree", "t", action: "open"), .select("i1"), "open", .string("i1")),
            (node("tree", "t", action: "open"), .expand("i1"), "expand", .string("i1")),
            (node("tree", "t", action: "open"), .collapse("i1"), "collapse", .string("i1")),
            (node("tabs", "tabs"), .select("t2"), "select", .string("t2")),
            (node("textField", "q"), .submit(.string("hi")), "submit", .string("hi")),
            (node("textField", "q", action: "search"), .submit(.string("hi")), "search", .string("hi")),
            (node("toggle", "on"), .toggle(true), "toggle", .bool(true)),
            (node("select", "s"), .change("v"), "select", .string("v")),
            (node("form", "f"), .submit(.object(["name": .string("x")])), "submit", .object(["name": .string("x")])),
            (node("form", "f", action: "create"), .submit(.object([:])), "create", .object([:])),
            (node("folderField", "repos"), .pick(["/a"]), "pick", .array([.string("/a")])),
            (node("folderField", "repos", action: "add"), .pick(["/a"]), "add", .array([.string("/a")])),
        ]
        for (node, trigger, action, value) in rows {
            let event = UIAction.event(for: node, trigger: trigger, slot: "x.main", projectID: "prj_1")
            XCTAssertEqual(event, UIEvent(projectId: "prj_1", slot: "x.main", nodeId: node.id ?? "", action: action, value: value), "\(node.type) \(trigger)")
        }
    }

    func testFolderFieldInForm() {
        let form = SDUINode(type: "form", id: "f", children: [
            SDUINode(type: "textField", id: "name"),
            SDUINode(type: "folderField", id: "repos", props: ["label": .string("Add")]),
            SDUINode(type: "form", id: "inner", children: [SDUINode(type: "folderField", id: "nested")]),
        ])
        let values: [String: JSONValue] = [
            "name": .string("demo"),
            "repos": UIAction.Trigger.pick(["/a", "/b"]).value,
            "nested": .array([]),
            "gone": .string("x"),
        ]
        XCTAssertEqual(UIForm.collect(values, in: form), .object(["name": .string("demo"), "repos": .array([.string("/a"), .string("/b")])]))
    }

    func testPaletteRanking() {
        let entries = ["New Terminal", "Open Terminal Here", "Restart Neovim", "Kill Orphan Processes"].map {
            PaletteEntry(id: $0, title: $0, detail: "")
        }
        XCTAssertEqual(CommandPalette.rank(entries, query: "term").map(\.title), ["New Terminal", "Open Terminal Here"])
        XCTAssertEqual(CommandPalette.rank(entries, query: "rn").first?.title, "Restart Neovim")
        XCTAssertEqual(CommandPalette.rank(entries, query: ""), entries)
    }

    func testAppScopeStore() async throws {
        let client = try await startCore()
        let store = ProjectUIStore(client: client, projectID: "")
        try await store.start()
        XCTAssertEqual(store.sections.map(\.id), ["projects"])
        try await waitUntil { store.slots["projects.main"] != nil }
        await store.stop()
        await client.close()
    }

    func testSectionCommandInvocations() throws {
        let commands = try JSONDecoder().decode([Command].self, from: Data(#"""
        [{"id":"terminals.new","title":"New","plugin":"terminals","hidden":false},
         {"id":"terminals.close","title":"Close","plugin":"terminals","hidden":false},
         {"id":"echo.say","title":"Say","plugin":"echo","hidden":false}]
        """#.utf8))
        let main = SDUINode(type: "stack", children: [SDUINode(type: "surface", props: ["sessionId": .string("ses_1")])])

        XCTAssertEqual(CommandInvocation.new(section: "terminals", commands: commands), CommandInvocation(id: "terminals.new"))
        XCTAssertNil(CommandInvocation.new(section: "echo", commands: commands))
        XCTAssertEqual(
            CommandInvocation.close(section: "terminals", main: main, commands: commands),
            CommandInvocation(id: "terminals.close", args: .object(["sessionId": .string("ses_1")]))
        )
        XCTAssertNil(CommandInvocation.close(section: "terminals", main: SDUINode(type: "empty"), commands: commands))
        XCTAssertNil(CommandInvocation.close(section: "echo", main: main, commands: commands))
    }

    private func startCore() async throws -> CoreClient {
        let core = try await TestCore.start()
        self.core = core
        return try await CoreClient.connect(paths: core.paths)
    }
}

private func node(_ type: String, _ id: String, action: String? = nil) -> SDUINode {
    SDUINode(type: type, id: id, props: action.map { ["action": .string($0)] } ?? [:])
}

private func firstUIUpdate(_ stream: AsyncStream<RPCNotification>, within timeout: Duration) async -> RPCNotification? {
    await withTaskGroup(of: RPCNotification?.self) { group in
        group.addTask {
            for await notification in stream where notification.method == "ui.updated" { return notification }
            return nil
        }
        group.addTask {
            try? await Task.sleep(for: timeout)
            return nil
        }
        let result = await group.next() ?? nil
        group.cancelAll()
        return result
    }
}

@MainActor
private func mainText(_ store: ProjectUIStore) -> String? {
    guard let tree = store.slots["terminals.main"]?.tree, tree.type == "empty" else { return nil }
    return tree.string("text")
}

@MainActor
private func terminalsPID(_ client: CoreClient) async throws -> pid_t? {
    guard case .array(let plugins) = try await client.call(method: "plugin.list") else { return nil }
    for case .object(let plugin) in plugins where plugin["id"] == .string("terminals") {
        if plugin["state"] == .string("running"), case .number(let pid)? = plugin["pid"], pid > 0 {
            return pid_t(pid)
        }
    }
    return nil
}

extension SDUINode {
    func find(id: String) -> SDUINode? {
        if self.id == id { return self }
        for child in children {
            if let found = child.find(id: id) { return found }
        }
        return nil
    }
}

@MainActor
func waitUntil(within timeout: Duration = .seconds(5), _ condition: () async throws -> Bool) async throws {
    _ = try await waitFor(within: timeout) { try await condition() ? true : nil }
}

@MainActor
func waitFor<T>(within timeout: Duration = .seconds(5), _ value: () async throws -> T?) async throws -> T {
    let deadline = ContinuousClock.now + timeout
    while true {
        if let result = try await value() { return result }
        guard ContinuousClock.now < deadline else { throw TestFailure(description: "condition not met within \(timeout)") }
        try await Task.sleep(for: .milliseconds(20))
    }
}

func first<Element: Sendable>(_ stream: AsyncStream<Element>, within timeout: Duration) async -> Element? {
    await withTaskGroup(of: Element?.self) { group in
        group.addTask {
            for await element in stream { return element }
            return nil
        }
        group.addTask {
            try? await Task.sleep(for: timeout)
            return nil
        }
        let result = await group.next() ?? nil
        group.cancelAll()
        return result
    }
}
