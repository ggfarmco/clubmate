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
}
