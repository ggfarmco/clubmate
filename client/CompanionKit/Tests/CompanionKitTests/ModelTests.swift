import Foundation
import XCTest

import CompanionKit

final class ModelTests: XCTestCase {
    struct Plugin: CompanionPlugin {
        let id: String
        let title = ""
        let systemImage = ""
        let requiredCommands: [String]
    }

    func testRegistryKeepsPluginsTheComputerSupports() throws {
        let registry = PluginRegistry([Plugin(id: "agents", requiredCommands: ["agents.sessions", "agents.prompt"]), Plugin(id: "git", requiredCommands: ["git.status"]), Plugin(id: "inbox", requiredCommands: [])])
        let hello = Hello(computer: ComputerInfo(id: "m", name: "M"), version: "1", commands: ["agents.prompt", "agents.sessions"], topics: [])
        XCTAssertEqual(registry.supported(by: hello).map(\.id), ["agents", "inbox"])
        XCTAssertEqual(registry.supported(by: nil).map(\.id), [])
    }

    func testInboxKeepsNewestFirstWithoutDuplicates() throws {
        var inbox = Inbox()
        let base = Date(timeIntervalSince1970: 1_000)
        for index in 0..<205 {
            inbox.add(InboxItem(id: "n\(index)", computerID: "m", workspace: "w", message: "", date: base.addingTimeInterval(TimeInterval(index))))
        }
        XCTAssertFalse(inbox.add(InboxItem(id: "n204", computerID: "m", workspace: "w", message: "", date: base)))
        XCTAssertEqual(inbox.items.count, 200)
        XCTAssertEqual(inbox.items.first?.id, "n204")
        XCTAssertEqual(inbox.items.last?.id, "n5")
        XCTAssertEqual(inbox.unread.count, 200)
    }

    func testInboxItemFromNotifyEvent() throws {
        let event = BusEvent(seq: 3, topic: "notify.posted", workspace: "w", payload: .object([
            "id": .string("ntf_1"), "workspace": .string("w"), "session": .string("agt_1"), "title": .string("Tests"),
            "message": .string("Need a decision"), "kind": .string("needs-input"), "agent": .bool(true), "ts": .string("2026-10-30T09:41:00.123456789Z"),
        ]))
        let item = try XCTUnwrap(InboxItem(event, computerID: "m", received: .distantPast))
        XCTAssertEqual(item, InboxItem(id: "ntf_1", computerID: "m", workspace: "w", session: "agt_1", title: "Tests", message: "Need a decision", kind: .needsInput, viaAgent: true, date: item.date))
        XCTAssertEqual(item.date.timeIntervalSince1970, 1_793_353_260.123, accuracy: 0.0005)
    }

    func testLocalAlertOnlyForRecentActionableItems() throws {
        let now = Date(timeIntervalSince1970: 10_000)
        let item = { (kind: NotificationKind, age: TimeInterval) in
            InboxItem(id: "ntf_1", computerID: "m", workspace: "w", session: "agt_1", message: "Need a decision", kind: kind, date: now.addingTimeInterval(-age))
        }
        let alert = try XCTUnwrap(LocalAlert(item(.permission, 5), computerName: "Mac Studio", now: now))
        XCTAssertEqual([alert.id, alert.title, alert.subtitle, alert.body, alert.session], ["m|ntf_1", "Permission needed", "Mac Studio", "Need a decision", "agt_1"])
        XCTAssertNil(LocalAlert(item(.info, 5), computerName: "", now: now))
        XCTAssertNil(LocalAlert(item(.permission, LocalAlert.window), computerName: "", now: now))
    }

    @MainActor
    func testCatchUpResumesFromTheSavedCursor() async throws {
        let defaults = try XCTUnwrap(UserDefaults(suiteName: UUID().uuidString))
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621), token: "t")
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": return (FakeTransport.result(workspacesJSON), 200)
            case NotificationPolicy.sessionsCommand:
                try await Task.sleep(for: .milliseconds(300))
                return (FakeTransport.result(#"[{"id":"agt_2","notifyEffective":"muted"}]"#), 200)
            default: return (FakeTransport.result(helloJSON), 200)
            }
        }, stream: { request in
            let after = request.value(forHTTPHeaderField: "Last-Event-ID")
            let wire = after == nil
                ? ": connected\n\nid: 7\nevent: bus\ndata: {\"seq\":7,\"topic\":\"agents.changed\",\"payload\":{}}\n\n"
                : ": connected\n\nid: 8\nevent: bus\ndata: {\"seq\":8,\"topic\":\"notify.posted\",\"workspace\":\"w\",\"payload\":{\"id\":\"n8\",\"session\":\"agt_2\",\"message\":\"m\",\"kind\":\"done\"}}\n\n"
            return (200, AsyncThrowingStream { $0.yield(Data(wire.utf8)) })
        })
        let device = DeviceIdentity(id: "d", name: "Phone", model: "iPhone")
        let first = CompanionModel(store: store, transport: transport, device: device, cursors: defaults, sleep: { _ in })
        first.start()
        try await waitUntil { first.connection("mac1")?.lastEventID == 7 }
        first.connection("mac1")?.stop()

        let second = CompanionModel(store: store, transport: transport, device: device, cursors: defaults, sleep: { _ in })
        let posted = Recorder<String>()
        second.posted = { posted.add($0.id) }
        await second.catchUp(quiet: .milliseconds(50), timeout: .seconds(5))

        XCTAssertEqual(second.connection("mac1")?.state, .online)
        XCTAssertEqual(transport.requests.first { $0.url?.path == "/v1/events" && $0.value(forHTTPHeaderField: "Last-Event-ID") != nil }?.value(forHTTPHeaderField: "Last-Event-ID"), "7")
        XCTAssertEqual(second.inbox.items.map(\.id), ["n8"])
        XCTAssertEqual(second.inbox.unread, [])
        XCTAssertEqual(posted.values, [])
        second.connection("mac1")?.stop()
    }

    @MainActor
    func testComputerStoreKeepsTokensInVault() throws {
        let file = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "computers.json")
        let vault = MemoryTokenVault()
        let store = ComputerStore(file: file, vault: vault)
        try store.save(Computer(id: "m", name: "Mac", hosts: ["h"], port: 1, pairedAt: Date(timeIntervalSince1970: 0)), token: "cmt_secret")
        XCTAssertFalse(try String(contentsOf: file, encoding: .utf8).contains("cmt_secret"))
        XCTAssertEqual(ComputerStore(file: file, vault: vault).computers.map(\.id), ["m"])
        try store.remove("m")
        XCTAssertNil(vault.token(for: "m"))
    }

    @MainActor
    func testInboxHoldsBackWhatTheSessionPolicyExcludes() async throws {
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "m", name: "Mac", hosts: ["100.64.0.1"], port: 1), token: "t")
        let sessions = #"[{"id":"agt_1","notifyEffective":"all"},{"id":"agt_2","notifyEffective":"muted"}]"#
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case NotificationPolicy.sessionsCommand: (FakeTransport.result(sessions), 200)
            default: (FakeTransport.result(helloJSON), 200)
            }
        }, stream: { _ in
            let wire = (1...2).map { "id: \($0)\nevent: bus\ndata: {\"seq\":\($0),\"topic\":\"notify.posted\",\"workspace\":\"w\",\"payload\":{\"id\":\"n\($0)\",\"session\":\"agt_\($0)\",\"message\":\"m\",\"kind\":\"done\"}}\n\n" }.joined()
            return (200, AsyncThrowingStream { $0.yield(Data((": connected\n\n" + wire).utf8)) })
        })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), sleep: { _ in })
        let posted = Recorder<String>()
        model.posted = { posted.add($0.id) }

        model.start()

        try await waitUntil { model.inbox.items.count == 2 }
        XCTAssertEqual(posted.values, ["n1"])
        XCTAssertEqual(model.inbox.unread, ["n1"])
        try model.remove("m")
    }
}
