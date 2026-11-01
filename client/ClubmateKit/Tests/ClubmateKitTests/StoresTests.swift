import Darwin
import Foundation
import Observation
import Synchronization
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
        let workspaces = WorkspacesStore(client: client)
        try await workspaces.start()
        let workspace = try await workspaces.create(name: "demo", repos: [])
        XCTAssertEqual(workspaces.workspaces.map(\.id), [workspace.id])
        let other = try await client.createWorkspace(name: "other")
        try await waitUntil { workspaces.workspaces.map(\.id).contains(other.id) }

        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await store.start()
        XCTAssertEqual(store.sections.map(\.id), ["terminals", "echo"])
        XCTAssertEqual(store.selectedSection, "terminals")
        let button = try await waitFor { store.slot("terminals.sidebar")?.tree.find(id: "new") }
        _ = try await store.send(UIAction.event(for: button, trigger: .click, slot: "terminals.sidebar", workspaceID: workspace.id)).get()
        try await waitUntil { store.slot("terminals.main")?.tree.type == "surface" }
        XCTAssertEqual(store.slot("terminals.sidebar")?.tree.find(id: "terminals")?.items().map(\.label), ["Terminal 1"])
        try await waitUntil { try await client.uiGet(workspaceID: workspace.id).allSatisfy { store.slot($0.slot) == $0 } }

        let close = SDUINode(type: "button", id: "close")
        _ = try await store.send(UIAction.event(for: close, trigger: .click, slot: "terminals.main", workspaceID: workspace.id)).get()
        try await waitUntil { store.slot("terminals.main")?.tree.type == "empty" }
        await store.stop()
        await workspaces.stop()
        await client.close()
    }

    func testSelectedSectionPersists() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let fresh = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await fresh.start()
        await fresh.stop()
        let initial = try await client.uiStateGet(workspaceID: workspace.id)
        XCTAssertEqual(initial, .object(["section": .string("terminals")]))
        let first = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await first.start()
        XCTAssertEqual(first.selectedSection, "terminals")
        XCTAssertNil(first.sidebarWidth)
        first.sidebarWidth = 240
        first.selectedSection = "echo"
        await first.stop()
        let state = try await client.uiStateGet(workspaceID: workspace.id)
        XCTAssertEqual(state, .object(["sidebarWidth": .number(240), "section": .string("echo")]))

        let second = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await second.start()
        XCTAssertEqual(second.selectedSection, "echo")
        XCTAssertEqual(second.sidebarWidth, 240)
        await second.stop()
        await client.close()
    }

    func testConcurrentStoresKeepEachOthersKeys() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let first = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        let second = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await first.start()
        try await second.start()
        for width in 1...20 {
            first.sidebarWidth = Double(width)
            second.sidebarHidden["s\(width)"] = true
        }
        await first.stop()
        await second.stop()
        let state = try await client.uiStateGet(workspaceID: workspace.id)?.objectValue
        XCTAssertEqual(state?["sidebarWidth"], .number(20))
        XCTAssertEqual(state?["sidebarHidden"]?.objectValue?.count, 20)
        await client.close()
    }

    func testFreshWorkspaceOpensFirstSectionInUserOrder() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id) { $0.reversed() }
        try await store.start()
        XCTAssertEqual(store.selectedSection, "echo")
        await store.stop()
        await client.close()
    }

    func testShownSectionWinsOverSaved() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        try await client.uiStateSet(workspaceID: workspace.id, state: .object(["section": .string("echo")]))
        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        store.show(section: "terminals")
        try await store.start()
        XCTAssertEqual(store.selectedSection, "terminals")
        await store.stop()
        await client.close()
    }

    func testRecentsSkipHomeAndOrderByLastOpened() throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let workspaces = try decoder.decode([Workspace].self, from: Data(#"""
        [{"id":"a","name":"never","folders":[],"createdAt":"2026-01-03T00:00:00Z"},
         {"id":"b","name":"old","folders":[],"createdAt":"2026-01-01T00:00:00Z","home":false,"lastOpenedAt":"2026-02-01T00:00:00Z"},
         {"id":"c","name":"~","folders":[],"createdAt":"2026-01-01T00:00:00Z","home":true,"lastOpenedAt":"2026-03-01T00:00:00Z"},
         {"id":"d","name":"new","folders":[],"createdAt":"2026-01-02T00:00:00Z","home":false,"lastOpenedAt":"2026-02-02T00:00:00Z"}]
        """#.utf8))

        XCTAssertEqual(Workspace.recents(workspaces).map(\.id), ["d", "b", "a"])
        XCTAssertEqual(Workspace.recents(workspaces, hiddenBefore: try Date("2026-02-01T12:00:00Z", strategy: .iso8601)).map(\.id), ["d"])
    }

    func testWorkspaceFoldersDecode() throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let workspace = try decoder.decode(Workspace.self, from: Data(#"""
        {"id":"a","name":"demo","folders":[{"path":"/a"},{"path":"/b","worktree":"/b2"},{"path":"/c","worktree":"/gone","missing":true}],"createdAt":"2026-01-01T00:00:00Z"}
        """#.utf8))

        XCTAssertEqual(workspace.folders.map(\.path), ["/a", "/b", "/c"])
        XCTAssertEqual(workspace.folders.map(\.worktree), [nil, "/b2", "/gone"])
        XCTAssertEqual(workspace.folders.map(\.missing), [false, false, true])
    }

    func testSidebarHiddenPersists() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let first = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await first.start()
        XCTAssertEqual(first.sidebarHidden, [:])
        first.sidebarHidden["terminals"] = true
        await first.stop()
        let state = try await client.uiStateGet(workspaceID: workspace.id)
        XCTAssertEqual(state?.objectValue?["sidebarHidden"], .object(["terminals": .bool(true)]))

        let second = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await second.start()
        XCTAssertEqual(second.sidebarHidden, ["terminals": true])
        await second.stop()
        await client.close()
    }

    func testCodeSectionHasNoSidebar() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await store.start()
        XCTAssertEqual(store.sidebarSlot, "terminals.sidebar")
        store.toggleSidebar()
        XCTAssertNil(store.sidebarSlot)

        store.selectedSection = CodeSection.id
        XCTAssertNil(store.sidebarSlot)
        store.toggleSidebar()
        XCTAssertEqual(store.sidebarHidden, ["terminals": true])
        XCTAssertNil(store.sidebarSlot)
        store.revealSidebar(CodeSection.id)
        XCTAssertEqual(store.sidebarHidden, ["terminals": true, CodeSection.id: false])
        XCTAssertNil(store.sidebarSlot)
        await store.stop()
        await client.close()
    }

    func testLegacyChatMovesIntoUIStateOnce() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let first = WorkspaceUIStore(client: client, workspaceID: workspace.id, legacyChat: LegacyChat(shown: true, width: 420))
        try await first.start()
        XCTAssertEqual(first.sidebarHidden, [CodeSection.id: false])
        XCTAssertEqual(first.chatWidth, 420)
        await first.stop()
        let state = try await client.uiStateGet(workspaceID: workspace.id)
        XCTAssertEqual(state?.objectValue?["sidebarHidden"], .object([CodeSection.id: .bool(false)]))
        XCTAssertEqual(state?.objectValue?["chatWidth"], .number(420))

        let second = WorkspaceUIStore(client: client, workspaceID: workspace.id, legacyChat: LegacyChat(shown: false, width: 300))
        try await second.start()
        XCTAssertEqual(second.sidebarHidden, [CodeSection.id: false])
        XCTAssertEqual(second.chatWidth, 420)
        await second.stop()
        await client.close()
    }

    func testFocusRequestFromNotification() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await store.start()
        store.selectedSection = "echo"
        guard let paths = core?.paths else { throw TestFailure(description: "no core") }
        let caller = try await CoreClient.connect(paths: paths)
        _ = try await caller.call(method: "ui.focus", params: .object([
            "workspaceId": .string(workspace.id), "section": .string("terminals"), "slot": .string("terminals.main"),
        ]))
        try await waitUntil { store.focusRequest?.slot == "terminals.main" }
        XCTAssertEqual(store.selectedSection, "terminals")
        await caller.close()
        await store.stop()
        await client.close()
    }

    func testFocusRequestCarriesField() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await store.start()
        guard let paths = core?.paths else { throw TestFailure(description: "no core") }
        let caller = try await CoreClient.connect(paths: paths)
        _ = try await caller.call(method: "ui.focus", params: .object([
            "workspaceId": .string(workspace.id), "section": .string("terminals"), "slot": .string("terminals.sidebar"), "field": .bool(true),
        ]))
        try await waitUntil { store.focusRequest?.field == true }
        XCTAssertEqual(store.focusRequest?.slot, "terminals.sidebar")
        await caller.close()
        await store.stop()
        await client.close()
    }

    func testPluginCrashEmptiesThenRerenders() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await store.start()
        try await waitUntil { store.slot("terminals.main")?.tree.string("text") == "Create a terminal" }

        let pid = try await waitFor { try await terminalsPID(client) }
        XCTAssertEqual(kill(pid, SIGKILL), 0)
        try await waitUntil { mainText(store) == "" }
        try await waitUntil(within: .seconds(10)) { mainText(store).map { !$0.isEmpty } ?? false }
        await store.stop()
        await client.close()
    }

    func testReconnectRefetches() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await store.start()
        try await waitUntil { store.slot("terminals.main") != nil }
        await client.close()
        try await waitUntil { !store.connected }

        guard let paths = core?.paths else { throw TestFailure(description: "no core") }
        let fresh = try await CoreClient.connect(paths: paths)
        try await store.reconnect(using: fresh)
        XCTAssertTrue(store.connected)
        let button = try XCTUnwrap(store.slot("terminals.sidebar")?.tree.find(id: "new"))
        _ = try await store.send(UIAction.event(for: button, trigger: .click, slot: "terminals.sidebar", workspaceID: workspace.id)).get()
        try await waitUntil { store.slot("terminals.main")?.tree.type == "surface" }
        await store.stop()
        await fresh.close()
    }

    func testStopDuringStartLeavesNoSubscription() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        let starting = Task { try await store.start() }
        await Task.yield()
        await store.stop()
        try await starting.value
        XCTAssertFalse(store.connected)

        _ = try await waitFor { try await terminalsPID(client) }
        let stream = client.subscribeNotifications()
        _ = try await client.commandRun(id: "terminals.new", workspaceID: workspace.id)
        let update = await firstUIUpdate(stream, within: .milliseconds(500))
        XCTAssertNil(update)
        await client.close()
    }

    func testRestartUnderTrafficKeepsReceivingUpdates() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        weak var released: WorkspaceUIStore?
        do {
            let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
            released = store
            let select = UIEvent(workspaceId: workspace.id, slot: "terminals.sidebar", nodeId: "terminals", action: "select", value: .string("x"))
            try await store.start()
            try await waitUntil { store.slot("terminals.main") != nil }
            _ = try await client.uiSubscribe(workspaceID: workspace.id)
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
            let rev = try XCTUnwrap(store.slot("terminals.main")?.rev)
            _ = try await client.uiEvent(select)
            try await waitUntil { (store.slot("terminals.main")?.rev ?? 0) > rev }
            await store.stop()
        }
        try await waitUntil { released == nil }
        await client.close()
    }

    func testSlotUpdateNotifiesOnlyItsReaders() async throws {
        let client = try await startCore()
        let workspace = try await client.createWorkspace(name: "demo")
        let store = WorkspaceUIStore(client: client, workspaceID: workspace.id)
        try await store.start()
        let rev = try await waitFor { store.slot("terminals.main")?.rev }
        let main = ObservedFlag()
        let others = ObservedFlag()
        withObservationTracking { _ = store.slot("terminals.main") } onChange: { main.fire() }
        withObservationTracking {
            _ = store.slot("echo.main")
            _ = store.hasSlot("terminals.main")
        } onChange: { others.fire() }
        _ = try await client.uiEvent(UIEvent(workspaceId: workspace.id, slot: "terminals.sidebar", nodeId: "terminals", action: "select", value: .string("x")))
        try await waitUntil { (store.slot("terminals.main")?.rev ?? 0) > rev }
        XCTAssertTrue(main.fired)
        XCTAssertFalse(others.fired)
        await store.stop()
        await client.close()
    }

    func testWorkspacePresenceReportsDeletionAfterSeen() {
        var presence = WorkspacePresence()
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
            let event = UIAction.event(for: node, trigger: trigger, slot: "x.main", workspaceID: "prj_1")
            XCTAssertEqual(event, UIEvent(workspaceId: "prj_1", slot: "x.main", nodeId: node.id ?? "", action: action, value: value), "\(node.type) \(trigger)")
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
          "columns": [{"id": "name", "title": "Name", "truncate": "middle"}, {"id": "cpu", "title": "CPU", "align": "right", "width": 60, "priority": 2}],
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
            SDUIColumn(id: "name", title: "Name", align: .left, width: nil, truncate: .middle),
            SDUIColumn(id: "cpu", title: "CPU", align: .right, width: 60, priority: 2),
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

    func testItemTruncateDecodes() {
        let tree = SDUINode(type: "tree", props: ["items": .array([
            .object(["id": .string("wt"), "label": .string("feat/long"), "truncate": .string("middle")]),
            .object(["id": .string("s"), "label": .string("Session")]),
        ])])

        XCTAssertEqual(tree.treeItems().map(\.item.truncate), [.middle, .tail])
    }

    func testTableColumnsDropByPriority() {
        let columns = [
            SDUIColumn(id: "pid", title: "PID", align: .left, width: 60),
            SDUIColumn(id: "mem", title: "MEM", align: .right, width: 80, priority: 1),
            SDUIColumn(id: "hist", title: "HISTORY", align: .left, width: 80, priority: 2),
            SDUIColumn(id: "cmd", title: "COMMAND", align: .left, width: nil),
        ]
        let all = 60 + 80 + 80 + SDUIColumn.minFlexibleWidth + 3 * 10

        XCTAssertEqual(SDUIColumn.visible(columns, width: 0, gutter: 10), [0, 1, 2, 3])
        XCTAssertEqual(SDUIColumn.visible(columns, width: all, gutter: 10), [0, 1, 2, 3])
        XCTAssertEqual(SDUIColumn.visible(columns, width: all - 1, gutter: 10), [0, 1, 3])
        XCTAssertEqual(SDUIColumn.visible(columns, width: 100, gutter: 10), [0, 3])
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

    func testFormSaveStaysSavingUntilTheLastSubmitIsAnswered() {
        var save = UIFormSave()
        save.edited()
        XCTAssertTrue(save.saving)
        save.sent()
        save.edited()
        save.sent()
        save.answered()
        XCTAssertTrue(save.saving)
        save.answered()
        XCTAssertFalse(save.saving)
    }

    func testPaletteRanking() {
        let entries = ["New Terminal", "Open Terminal Here", "Restart Neovim", "Kill Orphan Processes"].map {
            PaletteEntry(id: $0, title: $0, detail: "")
        }
        XCTAssertEqual(CommandPalette.rank(entries, query: "term").map(\.title), ["New Terminal", "Open Terminal Here"])
        XCTAssertEqual(CommandPalette.rank(entries, query: "rn").first?.title, "Restart Neovim")
        XCTAssertEqual(CommandPalette.rank(entries, query: ""), entries)
    }

    func testPaletteEntries() async throws {
        let commands = try JSONDecoder().decode([Command].self, from: Data(#"""
        [{"id":"editor-nvim.terminal","title":"Open Terminal Here","plugin":"editor-nvim","hidden":false},
         {"id":"editor-nvim.open","title":"Open File","plugin":"editor-nvim","hidden":true},
         {"id":"git.stage","title":"Stage","plugin":"git","hidden":false,"running":false}]
        """#.utf8))
        let sections = try JSONDecoder().decode([SectionInfo].self, from: Data(#"""
        [{"id":"editor-nvim","title":"Code","icon":"","order":10,"scope":"workspace","plugin":"editor-nvim"},
         {"id":"git","title":"Git","icon":"","order":15,"scope":"workspace","plugin":"git","host":"editor-nvim"},
         {"id":"terminals","title":"Terminals","icon":"terminal","order":20,"scope":"workspace","plugin":"terminals"}]
        """#.utf8))
        let worktrees = WorktreeModel(run: { _ in .success(.array([.object(["repo": .string("/r"), "path": .string("/r"), "main": .bool(true)])])) }, confirm: { _, _ in false }, report: { _ in })
        await worktrees.load()

        let entries = CommandPalette.entries(commands: commands, sections: sections, sidebar: true, worktrees: worktrees.worktrees)

        XCTAssertEqual(entries.map(\.title), ["Open Terminal Here", "Go to Code", "Go to Terminals", "Toggle Sidebar", "Open Folder…", "New Home Terminal", "Switch Worktree…", "New Worktree…"])
        XCTAssertEqual(entries.map(\.detail), ["editor-nvim.terminal", "⌘1", "⌘2", "⌘B", "⌘O", "⇧⌘N", "", ""])
        XCTAssertEqual(
            entries.map { PaletteAction(entryID: $0.id) },
            [.command("editor-nvim.terminal"), .section("editor-nvim"), .section("terminals"), .toggleSidebar, .openFolder, .newHomeTerminal, .switchWorktree, .newWorktree]
        )
        XCTAssertFalse(CommandPalette.entries(commands: [], sections: [], sidebar: false).contains { $0.title == "Toggle Sidebar" })
        XCTAssertEqual(CommandPalette.entries(commands: [], sections: [], sidebar: true, chat: true).first { $0.id == PaletteAction.toggleSidebar.entryID }?.title, "Toggle Chat")
    }

    func testPaletteWorkspaceEntries() {
        let workspaces = [PaletteWorkspace(id: "a", name: "api"), PaletteWorkspace(id: "b", name: "web")]

        let titles = CommandPalette.entries(commands: [], sections: [], sidebar: true, workspaces: workspaces, active: "a").map(\.title)

        XCTAssertTrue(titles.contains("Switch to web"))
        XCTAssertFalse(titles.contains("Switch to api"))
        XCTAssertTrue(titles.contains("Remove Workspace…"))
        XCTAssertFalse(titles.contains("Open Workspaces"))
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
        [{"id":"terminals","title":"Terminals","icon":"terminal","order":20,"scope":"workspace","autoNew":true,"plugin":"terminals"},
         {"id":"echo","title":"Echo","icon":"","order":30,"scope":"workspace","plugin":"echo"}]
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
        {"id":"terminals","title":"Terminals","icon":"terminal","order":20,"scope":"workspace","autoNew":true,"plugin":"terminals"}
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

private final class ObservedFlag: Sendable {
    private let state = Mutex(false)

    var fired: Bool { state.withLock { $0 } }

    func fire() { state.withLock { $0 = true } }
}

@MainActor
private func mainText(_ store: WorkspaceUIStore) -> String? {
    guard let tree = store.slot("terminals.main")?.tree, tree.type == "empty" else { return nil }
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
