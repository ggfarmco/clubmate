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
        let fresh = ProjectUIStore(client: client, projectID: project.id)
        try await fresh.start()
        await fresh.stop()
        let initial = try await client.uiStateGet(projectID: project.id)
        XCTAssertEqual(initial, .object(["section": .string("terminals")]))
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

    func testFreshWorkspaceOpensFirstSectionInUserOrder() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        let store = ProjectUIStore(client: client, projectID: project.id) { $0.reversed() }
        try await store.start()
        XCTAssertEqual(store.selectedSection, "echo")
        await store.stop()
        await client.close()
    }

    func testShownSectionWinsOverSaved() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        try await client.uiStateSet(projectID: project.id, state: .object(["section": .string("echo")]))
        let store = ProjectUIStore(client: client, projectID: project.id)
        store.show(section: "terminals")
        try await store.start()
        XCTAssertEqual(store.selectedSection, "terminals")
        await store.stop()
        await client.close()
    }

    func testRecentsSkipHomeAndOrderByLastOpened() throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let projects = try decoder.decode([Project].self, from: Data(#"""
        [{"id":"a","name":"never","repos":[],"createdAt":"2026-01-03T00:00:00Z"},
         {"id":"b","name":"old","repos":[],"createdAt":"2026-01-01T00:00:00Z","home":false,"lastOpenedAt":"2026-02-01T00:00:00Z"},
         {"id":"c","name":"~","repos":[],"createdAt":"2026-01-01T00:00:00Z","home":true,"lastOpenedAt":"2026-03-01T00:00:00Z"},
         {"id":"d","name":"new","repos":[],"createdAt":"2026-01-02T00:00:00Z","home":false,"lastOpenedAt":"2026-02-02T00:00:00Z"}]
        """#.utf8))

        XCTAssertEqual(Project.recents(projects).map(\.id), ["d", "b", "a"])
        XCTAssertEqual(Project.recents(projects, hiddenBefore: try Date("2026-02-01T12:00:00Z", strategy: .iso8601)).map(\.id), ["d"])
    }

    func testProjectRootsDefaultToRepos() throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let projects = try decoder.decode([Project].self, from: Data(#"""
        [{"id":"a","name":"old","repos":["/a"],"createdAt":"2026-01-01T00:00:00Z"},
         {"id":"b","name":"new","repos":["/a"],"roots":["/a2"],"missing":["/gone"],"createdAt":"2026-01-01T00:00:00Z"}]
        """#.utf8))

        XCTAssertEqual(projects.map(\.roots), [["/a"], ["/a2"]])
        XCTAssertEqual(projects.map(\.missing), [[], ["/gone"]])
    }

    func testSidebarHiddenPersists() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        let first = ProjectUIStore(client: client, projectID: project.id)
        try await first.start()
        XCTAssertEqual(first.sidebarHidden, [:])
        first.sidebarHidden["editor-nvim"] = true
        await first.stop()
        let state = try await client.uiStateGet(projectID: project.id)
        XCTAssertEqual(state?.objectValue?["sidebarHidden"], .object(["editor-nvim": .bool(true)]))

        let second = ProjectUIStore(client: client, projectID: project.id)
        try await second.start()
        XCTAssertEqual(second.sidebarHidden, ["editor-nvim": true])
        await second.stop()
        await client.close()
    }

    func testFocusRequestFromNotification() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        let store = ProjectUIStore(client: client, projectID: project.id)
        try await store.start()
        store.selectedSection = "echo"
        guard let paths = core?.paths else { throw TestFailure(description: "no core") }
        let caller = try await CoreClient.connect(paths: paths)
        _ = try await caller.call(method: "ui.focus", params: .object([
            "projectId": .string(project.id), "section": .string("terminals"), "slot": .string("terminals.main"),
        ]))
        try await waitUntil { store.focusRequest?.slot == "terminals.main" }
        XCTAssertEqual(store.selectedSection, "terminals")
        await caller.close()
        await store.stop()
        await client.close()
    }

    func testFocusRequestCarriesField() async throws {
        let client = try await startCore()
        let project = try await client.createProject(name: "demo")
        let store = ProjectUIStore(client: client, projectID: project.id)
        try await store.start()
        guard let paths = core?.paths else { throw TestFailure(description: "no core") }
        let caller = try await CoreClient.connect(paths: paths)
        _ = try await caller.call(method: "ui.focus", params: .object([
            "projectId": .string(project.id), "section": .string("terminals"), "slot": .string("terminals.sidebar"), "field": .bool(true),
        ]))
        try await waitUntil { store.focusRequest?.field == true }
        XCTAssertEqual(store.focusRequest?.slot, "terminals.sidebar")
        await caller.close()
        await store.stop()
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

        _ = try await waitFor { try await terminalsPID(client) }
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

    func testProjectPresenceReportsDeletionAfterSeen() {
        var presence = ProjectPresence()
        XCTAssertFalse(presence.observe(false))
        XCTAssertFalse(presence.observe(true))
        XCTAssertTrue(presence.observe(false))
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
            (node("tree", "t", action: "open"), .focus("/r/a"), "focus", .string("/r/a")),
            (node("tabs", "tabs"), .select("t2"), "select", .string("t2")),
            (node("textField", "q"), .submit(.string("hi")), "submit", .string("hi")),
            (node("textField", "q", action: "search"), .submit(.string("hi")), "search", .string("hi")),
            (node("toggle", "on"), .toggle(true), "toggle", .bool(true)),
            (node("select", "s"), .change("v"), "select", .string("v")),
            (node("form", "f"), .submit(.object(["name": .string("x")])), "submit", .object(["name": .string("x")])),
            (node("form", "f", action: "create"), .submit(.object([:])), "create", .object([:])),
            (node("folderField", "repos"), .pick(["/a"]), "pick", .array([.string("/a")])),
            (node("folderField", "repos", action: "add"), .pick(["/a"]), "add", .array([.string("/a")])),
            (node("table", "procs", action: "focus"), .sort(column: "cpu", desc: true), "sort", .object(["column": .string("cpu"), "desc": .bool(true)])),
        ]
        for (node, trigger, action, value) in rows {
            let event = UIAction.event(for: node, trigger: trigger, slot: "x.main", projectID: "prj_1")
            XCTAssertEqual(event, UIEvent(projectId: "prj_1", slot: "x.main", nodeId: node.id ?? "", action: action, value: value), "\(node.type) \(trigger)")
        }
    }

    func testConfirmActionTitle() {
        let labels: [String?] = ["Discard…", "Delete", nil]
        let titles = labels.map { label in
            SDUINode(type: "button", id: "b", props: label.map { ["label": .string($0)] } ?? [:]).confirmActionTitle()
        }
        XCTAssertEqual(titles, ["Discard", "Delete", "OK"])
    }

    func testTableDecodes() throws {
        let json = """
        {"type": "table", "id": "procs", "props": {
          "columns": [{"id": "name", "title": "Name"}, {"id": "cpu", "title": "CPU", "align": "right", "width": 60}],
          "rows": [{"id": "42", "depth": 1, "hasChildren": true, "expanded": true, "muted": true, "cells": [
            {"text": "nvim", "spark": {"values": [1, 3], "max": 4}},
            {"text": "35%", "bar": 0.35, "tone": "warn"}
          ]}],
          "sort": {"column": "cpu", "desc": true},
          "selected": "42"
        }}
        """
        let table = try JSONDecoder().decode(SDUINode.self, from: Data(json.utf8))

        XCTAssertEqual(table.columns(), [
            SDUIColumn(id: "name", title: "Name", align: .left, width: nil),
            SDUIColumn(id: "cpu", title: "CPU", align: .right, width: 60),
        ])
        XCTAssertEqual(table.tableRows(), [
            SDUITableRow(id: "42", depth: 1, hasChildren: true, expanded: true, muted: true, cells: [
                SDUICell(text: "nvim", bar: nil, tone: nil, spark: SDUISpark(values: [1, 3], max: 4)),
                SDUICell(text: "35%", bar: 0.35, tone: "warn", spark: nil),
            ]),
        ])
        XCTAssertEqual(table.tableSort(), SDUISort(column: "cpu", desc: true))
        XCTAssertEqual(table.string("selected"), "42")
    }

    func testSparklineHeightDecodes() throws {
        let sized = try JSONDecoder().decode(SDUINode.self, from: Data(#"{"type": "sparkline", "props": {"values": [1, 3], "max": 4, "height": 64}}"#.utf8))
        XCTAssertEqual(sized.spark(), SDUISpark(values: [1, 3], max: 4))
        XCTAssertEqual(sized.sparklineHeight(), 64)
        XCTAssertNil(SDUINode(type: "sparkline", props: ["values": .array([]), "max": .number(1)]).sparklineHeight())
    }

    func testTableSortToggle() {
        let name = SDUIColumn(id: "name", title: "Name", align: .left, width: nil)
        let cpu = SDUIColumn(id: "cpu", title: "CPU", align: .right, width: 60)

        XCTAssertEqual(SDUISort.next(clicking: name, current: nil), SDUISort(column: "name", desc: false))
        XCTAssertEqual(SDUISort.next(clicking: cpu, current: SDUISort(column: "name", desc: false)), SDUISort(column: "cpu", desc: true))
        XCTAssertEqual(SDUISort.next(clicking: cpu, current: SDUISort(column: "cpu", desc: true)), SDUISort(column: "cpu", desc: false))
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

    func testPaletteEntries() throws {
        let commands = try JSONDecoder().decode([Command].self, from: Data(#"""
        [{"id":"editor-nvim.terminal","title":"Open Terminal Here","plugin":"editor-nvim","hidden":false},
         {"id":"editor-nvim.open","title":"Open File","plugin":"editor-nvim","hidden":true},
         {"id":"editor-nvim.navigation","title":"nvim Navigation Keys","plugin":"editor-nvim","hidden":true}]
        """#.utf8))
        let sections = try JSONDecoder().decode([SectionInfo].self, from: Data(#"""
        [{"id":"editor-nvim","title":"Code","icon":"","order":10,"scope":"project","plugin":"editor-nvim"},
         {"id":"git","title":"Git","icon":"","order":15,"scope":"project","plugin":"git","host":"editor-nvim"},
         {"id":"terminals","title":"Terminals","icon":"terminal","order":20,"scope":"project","plugin":"terminals"}]
        """#.utf8))

        let entries = CommandPalette.entries(commands: commands, sections: sections, sidebar: true)

        XCTAssertEqual(entries.map(\.title), ["Open Terminal Here", "Go to Code", "Go to Git", "Go to Terminals", "Toggle Sidebar", "Toggle nvim Navigation Keys", "Open Folder…", "New Home Terminal"])
        XCTAssertEqual(entries.map(\.detail), ["editor-nvim.terminal", "⌘1", "", "⌘2", "⌘B", "", "⌘O", "⇧⌘N"])
        XCTAssertEqual(
            entries.map { PaletteAction(entryID: $0.id) },
            [.command("editor-nvim.terminal"), .section("editor-nvim"), .section("git"), .section("terminals"), .toggleSidebar, .toggleNavigationKeys, .openFolder, .newHomeTerminal]
        )
        XCTAssertFalse(CommandPalette.entries(commands: [], sections: [], sidebar: false).contains { $0.title == "Toggle Sidebar" })
    }

    func testPaletteWorkspaceEntries() {
        let workspaces = [PaletteWorkspace(id: "a", name: "api"), PaletteWorkspace(id: "b", name: "web")]

        let titles = CommandPalette.entries(commands: [], sections: [], sidebar: true, workspaces: workspaces, active: "a").map(\.title)

        XCTAssertTrue(titles.contains("Switch to web"))
        XCTAssertFalse(titles.contains("Switch to api"))
        XCTAssertTrue(titles.contains("Remove Workspace…"))
        XCTAssertFalse(titles.contains("Open Projects"))
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

    func testShouldAutoNew() throws {
        let sections = try JSONDecoder().decode([SectionInfo].self, from: Data(#"""
        [{"id":"terminals","title":"Terminals","icon":"terminal","order":20,"scope":"project","autoNew":true,"plugin":"terminals"},
         {"id":"echo","title":"Echo","icon":"","order":30,"scope":"project","plugin":"echo"}]
        """#.utf8))
        let surface = SDUINode(type: "stack", children: [SDUINode(type: "surface", props: ["sessionId": .string("ses_1")])])
        let empty = SDUINode(type: "empty")

        XCTAssertTrue(CommandInvocation.shouldAutoNew(section: sections[0], main: empty))
        XCTAssertFalse(CommandInvocation.shouldAutoNew(section: sections[0], main: surface))
        XCTAssertFalse(CommandInvocation.shouldAutoNew(section: sections[0], main: nil))
        XCTAssertFalse(CommandInvocation.shouldAutoNew(section: sections[1], main: empty))
        XCTAssertFalse(CommandInvocation.shouldAutoNew(section: sections[0], main: exited))
    }

    func testAutoNewGateFiresOnEachArrival() throws {
        let terminals = try JSONDecoder().decode(SectionInfo.self, from: Data(#"""
        {"id":"terminals","title":"Terminals","icon":"terminal","order":20,"scope":"project","autoNew":true,"plugin":"terminals"}
        """#.utf8))
        let empty = SDUINode(type: "empty")
        let loading = AutoNewGate.Key(section: "terminals", loaded: false, request: 0)
        let shown = AutoNewGate.Key(section: "terminals", loaded: true, request: 0)
        let requested = AutoNewGate.Key(section: "terminals", loaded: true, request: 1)
        var gate = AutoNewGate()

        XCTAssertFalse(gate.fires(from: loading, to: loading, section: terminals, main: nil))
        XCTAssertTrue(gate.fires(from: loading, to: shown, section: terminals, main: empty))
        XCTAssertFalse(gate.fires(from: shown, to: loading, section: terminals, main: nil))
        XCTAssertFalse(gate.fires(from: loading, to: shown, section: terminals, main: empty))
        XCTAssertTrue(gate.fires(from: shown, to: shown, section: terminals, main: empty))
        XCTAssertTrue(gate.fires(from: shown, to: requested, section: terminals, main: empty))
    }

    func testTerminalClose() throws {
        let commands = try JSONDecoder().decode([Command].self, from: Data(#"""
        [{"id":"terminals.close","title":"Close","plugin":"terminals","hidden":false}]
        """#.utf8))
        let sidebar = SDUINode(type: "stack", children: [SDUINode(type: "list", id: "terminals", props: ["selected": .string("ses_1")])])
        let close = CommandInvocation(id: "terminals.close", args: .object(["sessionId": .string("ses_1")]))

        XCTAssertEqual(TerminalsSection.close(sidebar: sidebar, commands: commands), close)
        XCTAssertNil(TerminalsSection.close(sidebar: SDUINode(type: "list", id: "terminals"), commands: commands))
    }

    func testTerminalBusyMessage() {
        XCTAssertEqual(TerminalsSection.busyMessage(.object(["busy": .bool(true), "process": .string("sleep")])), "“sleep” is running. Close the terminal?")
        XCTAssertNil(TerminalsSection.busyMessage(.object(["busy": .bool(false), "process": .string("")])))
    }

    private let exited = SDUINode(type: "stack", children: [SDUINode(type: "text", props: ["text": .string("Terminal exited (code 0)")]), SDUINode(type: "button", id: "restart")])

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
