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
            inbox.add(InboxItem(notificationID: "n\(index)", computerID: "m", workspace: "w", message: "", date: base.addingTimeInterval(TimeInterval(index))))
        }
        XCTAssertFalse(inbox.add(InboxItem(notificationID: "n204", computerID: "m", workspace: "w", message: "", date: base)))
        XCTAssertEqual(inbox.items.count, 200)
        XCTAssertEqual(inbox.items.first?.notificationID, "n204")
        XCTAssertEqual(inbox.items.last?.notificationID, "n5")
        XCTAssertEqual(inbox.unread.count, 200)
    }

    func testInboxUnreadIsPerComputer() {
        var inbox = Inbox()
        let date = Date(timeIntervalSince1970: 1_000)
        let old = InboxItem(notificationID: "ntf_1", computerID: "old", workspace: "w", message: "", date: date)
        let new = InboxItem(notificationID: "ntf_1", computerID: "new", workspace: "w", message: "", date: date)
        XCTAssertTrue(inbox.add(old))
        XCTAssertTrue(inbox.add(new))
        XCTAssertNotEqual(old.id, new.id)

        inbox.markRead(old)
        XCTAssertFalse(inbox.isUnread(old))
        XCTAssertTrue(inbox.isUnread(new))

        inbox.markRead(new)
        inbox.add(InboxItem(notificationID: "ntf_2", computerID: "old", workspace: "w", message: "", date: date))
        inbox.add(InboxItem(notificationID: "ntf_2", computerID: "new", workspace: "w", message: "", date: date))
        inbox.remove(computer: "old")
        XCTAssertEqual(inbox.unread, ["new|ntf_2"])

        for index in 0..<Inbox.capacity {
            inbox.add(InboxItem(notificationID: "ntf_x\(index)", computerID: "old", workspace: "w", message: "", date: date.addingTimeInterval(1)))
        }
        XCTAssertEqual(inbox.unread.count, Inbox.capacity)
        XCTAssertFalse(inbox.unread.contains("new|ntf_2"))
    }

    func testInboxItemFromNotifyEvent() throws {
        let event = BusEvent(seq: 3, topic: "notify.posted", workspace: "w", payload: .object([
            "id": .string("ntf_1"), "workspace": .string("w"), "session": .string("agt_1"), "title": .string("Tests"),
            "message": .string("Need a decision"), "kind": .string("needs-input"), "agent": .bool(true), "ts": .string("2026-10-30T09:41:00.123456789Z"),
        ]))
        let item = try XCTUnwrap(InboxItem(event, computerID: "m", received: .distantPast))
        XCTAssertEqual(item, InboxItem(notificationID: "ntf_1", computerID: "m", workspace: "w", session: "agt_1", title: "Tests", message: "Need a decision", kind: .needsInput, viaAgent: true, date: item.date))
        XCTAssertEqual(item.date.timeIntervalSince1970, 1_793_353_260.123, accuracy: 0.0005)
    }

    func testInboxWaitsOnlyForTheNewestOpenRequestPerSession() {
        var inbox = Inbox()
        let now = Date(timeIntervalSince1970: 10_000)
        let item = { (id: String, session: String?, kind: NotificationKind, age: TimeInterval) in
            InboxItem(notificationID: id, computerID: "m", workspace: "w", session: session, message: "", kind: kind, date: now.addingTimeInterval(-age))
        }
        let question = item("q", "agt_1", .needsInput, 30)
        let older = item("p", "agt_1", .permission, 60)
        let answered = item("a", "agt_2", .permission, 90)
        let unknown = item("u", nil, .needsInput, 100)
        let seen = item("s", nil, .permission, 110)
        let done = item("d", "agt_3", .done, 120)
        for entry in [question, older, answered, unknown, seen, done, InboxItem(notificationID: "x", computerID: "other", workspace: "w", message: "", kind: .permission, date: now)] {
            inbox.add(entry)
        }
        inbox.markRead(question)
        inbox.markRead(seen)
        let sections = inbox.sections(computer: "m") { $0.session.map { $0 == "agt_1" } }
        XCTAssertEqual(sections.waiting.map(\.notificationID), ["q", "u"])
        XCTAssertEqual(sections.earlier.map(\.notificationID), ["p", "a", "s", "d"])
        XCTAssertEqual(sections.waiting.map(inbox.isUnread), [false, true])
        XCTAssertEqual(sections.earlier.map(inbox.isUnread), [true, true, false, true])
    }

    func testResolvedSessionLeavesWaitingAndReturnsItsPermissionAlerts() {
        var inbox = Inbox()
        let item = { (id: String, computer: String, session: String, kind: NotificationKind) in
            InboxItem(notificationID: id, computerID: computer, workspace: "w", session: session, message: "", kind: kind, date: .distantPast)
        }
        let resolved = item("p", "m", "agt_1", .permission)
        let others = [item("q", "m", "agt_1", .needsInput), item("o", "m", "agt_2", .permission), item("x", "other", "agt_1", .permission)]
        for entry in [resolved] + others { inbox.add(entry) }
        XCTAssertEqual(inbox.resolve(computer: "m", session: "agt_1", kind: .permission), [resolved.id])
        XCTAssertFalse(inbox.isUnread(resolved))
        XCTAssertTrue(others.allSatisfy(inbox.isUnread))
    }

    func testGateResolveWithdrawsOnlyItsCardFromTheInbox() {
        var inbox = Inbox()
        let cards = ["lg_1", "lg_2"].map { InboxItem(notificationID: $0, computerID: "m", workspace: "w", session: "agt_1", message: "", kind: .needsInput, request: $0, date: .distantPast) }
        for card in cards { inbox.add(card) }
        XCTAssertEqual(inbox.resolve(computer: "m", session: "agt_1", kind: .needsInput, request: "lg_1"), [cards[0].id])
        XCTAssertTrue(inbox.isUnread(cards[1]))
    }

    func testInboxAgeIsCompact() {
        let now = Date(timeIntervalSince1970: 200_000)
        let ages = [30, 600, 3_700, 90_000].map { InboxItem(notificationID: "n", computerID: "m", workspace: "w", message: "", date: now.addingTimeInterval(-TimeInterval($0))).age(now: now) }
        XCTAssertEqual(ages, ["30 s", "10 min", "1 h", "1 d"])
    }

    func testInboxCommandIsThePendingPermissionsCommand() throws {
        let transcript = try JSONDecoder.clubmate.decode(AgentTranscript.self, from: fixture("agents-transcript"))
        XCTAssertEqual(InboxItem.pendingCommand(transcript.entries), "go test ./...")
        XCTAssertNil(InboxItem.pendingCommand(Array(transcript.entries.prefix(6))))
    }

    func testLocalAlertIdentifierCollapsesOnTheNotificationsGroup() throws {
        let now = Date(timeIntervalSince1970: 10_000)
        let item = { (collapse: String?) in
            InboxItem(notificationID: "ntf_1", computerID: "m", workspace: "w", session: "agt_1", collapse: collapse, message: "Finished", kind: .done, date: now)
        }
        XCTAssertEqual(LocalAlert(item("agt_1"), computerName: "", workspaceName: nil, now: now)?.identifier, "agt_1")
        XCTAssertEqual(LocalAlert(item(nil), computerName: "", workspaceName: nil, now: now)?.identifier, "m|ntf_1")
        XCTAssertEqual(NotifyPosted(.object(["id": .string("ntf_1"), "message": .string("m"), "collapse": .string("agt_1")]))?.collapse, "agt_1")
    }

    func testQuietNotificationMakesAQuietAlert() throws {
        let now = Date(timeIntervalSince1970: 10_000)
        let posted = try XCTUnwrap(NotifyPosted(.object(["id": .string("ntf_1"), "message": .string("Finished"), "kind": .string("done"), "quiet": .bool(true)])))
        XCTAssertTrue(posted.quiet)
        let item = { (quiet: Bool) in
            InboxItem(notificationID: "ntf_1", computerID: "m", workspace: "w", message: "Finished", kind: .done, quiet: quiet, date: now)
        }
        XCTAssertEqual(LocalAlert(item(true), computerName: "", workspaceName: nil, now: now)?.quiet, true)
        XCTAssertEqual(LocalAlert(item(false), computerName: "", workspaceName: nil, now: now)?.quiet, false)
    }

    func testLocalAlertOnlyForRecentActionableItems() throws {
        let now = Date(timeIntervalSince1970: 10_000)
        let item = { (kind: NotificationKind, age: TimeInterval) in
            InboxItem(notificationID: "ntf_1", computerID: "m", workspace: "w", session: "agt_1", message: "Need a decision", kind: kind, date: now.addingTimeInterval(-age))
        }
        let alert = try XCTUnwrap(LocalAlert(item(.permission, 5), computerName: "Mac Studio", workspaceName: "shop", now: now))
        XCTAssertEqual([alert.id, alert.title, alert.subtitle, alert.body, alert.session], ["m|ntf_1", "Mac Studio · shop", "Permission needed", "Need a decision", "agt_1"])
        XCTAssertNil(LocalAlert(item(.info, 5), computerName: "", workspaceName: nil, now: now))
        XCTAssertNil(LocalAlert(item(.permission, LocalAlert.window), computerName: "", workspaceName: nil, now: now))
    }

    @MainActor
    func testCatchUpResumesFromTheSavedCursor() async throws {
        let defaults = try XCTUnwrap(UserDefaults(suiteName: UUID().uuidString))
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 47621), token: "t")
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": return (FakeTransport.result(workspacesJSON), 200)
            case AgentCommand.sessions:
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
        second.posted = { posted.add($0.notificationID) }
        await second.catchUp(quiet: .milliseconds(50), timeout: .seconds(5))

        XCTAssertEqual(second.connection("mac1")?.state, .online)
        XCTAssertEqual(transport.requests.first { $0.url?.path == "/v1/events" && $0.value(forHTTPHeaderField: "Last-Event-ID") != nil }?.value(forHTTPHeaderField: "Last-Event-ID"), "7")
        XCTAssertEqual(second.inbox.items.map(\.notificationID), ["n8"])
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
    func testPairFallsToTheRelayAndStoresTicketAndKey() async throws {
        let relay = FakeRelay()
        let route = relayRoute(relay.privateKey)
        relay.answer("/v1/pair", #"{"token":"cmt_dev","relayTicket":"cmk_dev","computer":{"id":"mac1","name":"Mac Studio"}}"#)
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        let model = CompanionModel(store: store, transport: relay.transport, device: DeviceIdentity(id: "d", name: "Phone", model: "iPhone"))
        let link = PairingLink(code: "C", hosts: ["100.64.0.1"], port: 47621, computerID: "mac1", computerName: "Mac", expiresAt: .distantFuture, relay: route, pairingTicket: "cmk_pt")

        let computer = try await model.pair(link)
        model.connection("mac1")?.stop()

        XCTAssertEqual(computer.relay, route)
        XCTAssertEqual(store.token(for: "mac1"), "cmt_dev")
        XCTAssertEqual(store.relayTicket(for: "mac1"), "cmk_dev")
        let requests = relay.transport.requests
        XCTAssertEqual(requests.first?.url?.absoluteString, "http://100.64.0.1:47621/v1/pair")
        XCTAssertEqual(requests.first?.timeoutInterval, RemoteClient.probeTimeout)
        XCTAssertEqual(requests.dropFirst().first?.value(forHTTPHeaderField: TunnelTransport.ticketHeader), "cmk_pt")
        XCTAssertEqual(relay.seen.map(\.path), ["/v1/pair"])
        let routes = try XCTUnwrap(model.connection("mac1")?.routes)
        guard routes.count == 2, case .tailnet = routes[0], case .relay(let tunnel, "mac1") = routes[1] else { return XCTFail("routes \(routes)") }
        XCTAssertEqual(tunnel.macKey, route.macKey)
        try store.remove("mac1")
        XCTAssertNil(store.relayTicket(for: "mac1"))
    }

    @MainActor
    func testATailscalePairThatTimesOutGoesToTheRelayAndAUsedCodeSaysItMayHavePaired() async throws {
        let relay = FakeRelay()
        let transport = RoutedTransport(relay: relay, tailnet: FakeTransport(reply: { _ in throw URLError(.timedOut) }))
        let link = PairingLink(code: "C", hosts: ["mac.tail1.ts.net", "100.64.0.1"], port: 47621, computerID: "mac1", computerName: "Mac", expiresAt: .distantFuture, relay: relayRoute(relay.privateKey), pairingTicket: "cmk_pt")
        let device = DeviceIdentity(id: "d", name: "Phone", model: "iPhone")
        relay.answer("/v1/pair", #"{"error":{"code":"forbidden","message":"wrong, expired or already used"}}"#, status: 403)

        do {
            _ = try await RemoteClient.pair(link, device: device, transport: transport)
            XCTFail("paired")
        } catch {
            XCTAssertEqual(error as? RemoteError, .pairingFailed(RemoteClient.mayHavePairedMessage))
        }

        relay.answer("/v1/pair", #"{"token":"cmt_dev","relayTicket":"cmk_dev","computer":{"id":"mac1","name":"Mac Studio"}}"#)
        let paired = try await RemoteClient.pair(link, device: device, transport: transport)
        XCTAssertEqual(paired.relayTicket, "cmk_dev")
        XCTAssertEqual(relay.seen.map(\.path), ["/v1/pair", "/v1/pair"])
    }

    @MainActor
    func testARelayPairWithoutARelayTicketIsRefused() async throws {
        let relay = FakeRelay()
        relay.answer("/v1/pair", #"{"token":"cmt_dev","computer":{"id":"mac1","name":"Mac Studio"}}"#)
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        let model = CompanionModel(store: store, transport: relay.transport, device: DeviceIdentity(id: "d", name: "Phone", model: "iPhone"))
        let link = PairingLink(code: "C", hosts: [], port: 47621, computerID: "mac1", computerName: "Mac", expiresAt: .distantFuture, relay: relayRoute(relay.privateKey), pairingTicket: "cmk_pt")

        do {
            _ = try await model.pair(link)
            XCTFail("paired")
        } catch {
            XCTAssertEqual(error as? RemoteError, .pairingFailed(RemoteClient.noRelayTicketMessage))
        }
        XCTAssertTrue(store.computers.isEmpty)
    }

    @MainActor
    func testSetPathSavesTheChoiceAndReconnectsOverIt() async throws {
        let file = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "computers.json")
        let vault = MemoryTokenVault()
        let store = ComputerStore(file: file, vault: vault)
        let relay = FakeRelay(endStreams: false)
        try store.save(Computer(id: "mac1", name: "Mac Studio", hosts: ["100.64.0.1"], port: 47621, relay: relayRoute(relay.privateKey)), token: "t", relayTicket: "cmk_t")
        let tailnet = FakeTransport(reply: { request in
            (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : helloJSON), 200)
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let model = CompanionModel(store: store, transport: RoutedTransport(relay: relay, tailnet: tailnet), device: DeviceIdentity(id: "d", name: "Phone", model: "iPhone"), sleep: { _ in })
        model.start()
        let connection = try XCTUnwrap(model.connection("mac1"))
        try await waitUntil { connection.activePath == .tailnet && connection.state == .online }

        try model.setPath(.relay, computer: "mac1")

        try await waitUntil { connection.activePath == .relay && connection.state == .online }
        XCTAssertEqual(ComputerStore(file: file, vault: vault).computers.first?.path, .relay)
        connection.stop()
    }

    @MainActor
    func testNewRelayKeyFromHelloIsStored() async throws {
        let file = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "computers.json")
        let vault = MemoryTokenVault()
        let store = ComputerStore(file: file, vault: vault)
        try store.save(Computer(id: "mac1", name: "Mac Studio", hosts: ["100.64.0.1"], port: 47621, relay: relayRoute()), token: "t", relayTicket: "cmk_dev")
        let rotated = relayRoute()
        let hello = #"{"computer":{"id":"mac1","name":"Mac Studio"},"version":"1","commands":[],"topics":[],"tunnel":{"url":"https://relay.example","key":"\#(rotated.key)","kid":"\#(rotated.kid)"}}"#
        let transport = FakeTransport(reply: { request in
            (FakeTransport.result(FakeTransport.rpc(request) == "workspaces" ? workspacesJSON : hello), 200)
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "Phone", model: "iPhone"), sleep: { _ in })
        model.start()
        try await waitUntil { model.computers.first?.relay == rotated }
        model.connection("mac1")?.stop()

        XCTAssertEqual(ComputerStore(file: file, vault: vault).computers.first?.relay, rotated)
        guard case .relay(let tunnel, _)? = model.connection("mac1")?.routes.last else { return XCTFail("no relay route") }
        XCTAssertEqual(tunnel.macKey, rotated.macKey)
    }

    @MainActor
    func testComputerStoreKeepsLastSeenAcrossLaunches() async throws {
        let file = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "computers.json")
        try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(#"[{"id":"m","name":"Mac","hosts":["100.64.0.1"],"port":1,"pairedAt":"2026-10-31T10:00:00Z"}]"#.utf8).write(to: file)
        let vault = MemoryTokenVault()
        let store = ComputerStore(file: file, vault: vault)
        XCTAssertEqual(store.computers.map(\.id), ["m"])
        XCTAssertNil(store.computers[0].lastSeen)
        XCTAssertNil(store.computers[0].relay)
        XCTAssertEqual(store.computers[0].path, .auto)
        XCTAssertEqual(store.computers[0].tag.macColour, .graphite)

        let seen = Date(timeIntervalSince1970: 1_800_000_000)
        try store.seen("m", at: seen)
        try store.seen("m", at: seen.addingTimeInterval(ComputerStore.seenEvery - 1))
        XCTAssertEqual(ComputerStore(file: file, vault: vault).computers[0].lastSeen, seen)

        let connection = ComputerConnection(computer: ComputerStore(file: file, vault: vault).computers[0], token: "t", transport: FakeTransport(reply: { _ in throw URLError(.notConnectedToInternet) }))
        XCTAssertEqual(connection.presence, "offline since " + ComputerConnection.clock(seen))
        XCTAssertEqual(connection.shortPresence, "offline")
        XCTAssertEqual(connection.blocked, "Can't answer — Mac is offline · as of " + ComputerConnection.clock(seen))
        XCTAssertEqual(connection.blocked("add"), "Can't add — Mac is offline · as of " + ComputerConnection.clock(seen))
        do {
            _ = try await connection.command("backlog.add", workspace: "w", args: nil)
            XCTFail("sent offline")
        } catch {
            XCTAssertEqual(connection.failure(error, action: "add"), connection.blocked("add"))
        }
    }

    @MainActor
    func testRequestResolvedElsewhereLeavesNoAlertOrUnreadItem() async throws {
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "m", name: "Mac", hosts: ["100.64.0.1"], port: 1), token: "t")
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case AgentCommand.sessions: (FakeTransport.result("[]"), 200)
            default: (FakeTransport.result(helloJSON), 200)
            }
        }, stream: { _ in
            let wire = [
                #"{"seq":1,"topic":"notify.posted","workspace":"w","payload":{"id":"n1","session":"agt_1","message":"m","kind":"permission"}}"#,
                #"{"seq":2,"topic":"agents.changed","workspace":"w","payload":{"workspace":"w","session":"agt_1","kind":"resolved","request":"permission-1"}}"#,
            ].enumerated().map { "id: \($0.offset + 1)\nevent: bus\ndata: \($0.element)\n\n" }.joined()
            return (200, AsyncThrowingStream { $0.yield(Data((": connected\n\n" + wire).utf8)) })
        })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), sleep: { _ in })
        let posted = Recorder<String>()
        let withdrawn = Recorder<String>()
        model.posted = { posted.add($0.id) }
        model.withdrawn = { $0.forEach(withdrawn.add) }

        model.start()

        try await waitUntil { model.inbox.items.count == 1 }
        XCTAssertEqual(posted.values, withdrawn.values)
        XCTAssertEqual(model.inbox.unread, [])
        try model.remove("m")
    }

    @MainActor
    func testReplayedPushDoesNotAlertAgain() async throws {
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "m", name: "Mac", hosts: ["100.64.0.1"], port: 1), token: "t")
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            default: (FakeTransport.result(helloJSON), 200)
            }
        }, stream: { _ in
            let wire = [
                #"{"seq":1,"topic":"notify.posted","workspace":"w","payload":{"id":"n1","message":"m","kind":"done","pushed":true}}"#,
                #"{"seq":2,"topic":"notify.posted","workspace":"w","payload":{"id":"n2","message":"m","kind":"done"}}"#,
            ].enumerated().map { "id: \($0.offset + 1)\nevent: bus\ndata: \($0.element)\n\n" }.joined()
            return (200, AsyncThrowingStream { $0.yield(Data((": connected\n\n" + wire).utf8)) })
        })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), sleep: { _ in })
        let posted = Recorder<String>()
        model.posted = { posted.add($0.id) }

        model.start()

        try await waitUntil { model.inbox.items.count == 2 }
        XCTAssertEqual(posted.values, ["m|n2"])
        XCTAssertEqual(model.inbox.unread, ["m|n1", "m|n2"])
        try model.remove("m")
    }

    @MainActor
    func testAnsweredQuestionWithdrawsOnlyItsSessionsQuestionAlerts() async throws {
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "m", name: "Mac", hosts: ["100.64.0.1"], port: 1), token: "t")
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case AgentCommand.sessions: (FakeTransport.result("[]"), 200)
            default: (FakeTransport.result(helloJSON), 200)
            }
        }, stream: { _ in
            let wire = [
                #"{"seq":1,"topic":"notify.posted","workspace":"w","payload":{"id":"n1","session":"agt_1","message":"m","kind":"permission"}}"#,
                #"{"seq":2,"topic":"notify.posted","workspace":"w","payload":{"id":"n2","session":"agt_1","message":"q","kind":"needs-input"}}"#,
                #"{"seq":3,"topic":"notify.posted","workspace":"w","payload":{"id":"n3","session":"agt_1","message":"Which store?","kind":"question"}}"#,
                #"{"seq":4,"topic":"agents.changed","workspace":"w","payload":{"workspace":"w","session":"agt_1","kind":"resolved","request":"elicitation-2","requestKind":"elicitation"}}"#,
            ].enumerated().map { "id: \($0.offset + 1)\nevent: bus\ndata: \($0.element)\n\n" }.joined()
            return (200, AsyncThrowingStream { $0.yield(Data((": connected\n\n" + wire).utf8)) })
        })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), sleep: { _ in })
        let posted = Recorder<String>()
        let withdrawn = Recorder<String>()
        model.posted = { posted.add($0.id) }
        model.withdrawn = { $0.forEach(withdrawn.add) }

        model.start()

        try await waitUntil { model.inbox.items.count == 3 }
        XCTAssertEqual(Set(posted.values).subtracting(withdrawn.values), ["m|n1"])
        XCTAssertEqual(model.inbox.unread, ["m|n1"])
        try model.remove("m")
    }

    @MainActor
    func testInboxHoldsBackWhatTheSessionPolicyExcludes() async throws {
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "m", name: "Mac", hosts: ["100.64.0.1"], port: 1), token: "t")
        let sessions = #"[{"id":"agt_1","notifyEffective":"all"},{"id":"agt_2","notifyEffective":"muted"}]"#
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case AgentCommand.sessions: (FakeTransport.result(sessions), 200)
            default: (FakeTransport.result(helloJSON), 200)
            }
        }, stream: { _ in
            let wire = (1...2).map { "id: \($0)\nevent: bus\ndata: {\"seq\":\($0),\"topic\":\"notify.posted\",\"workspace\":\"w\",\"payload\":{\"id\":\"n\($0)\",\"session\":\"agt_\($0)\",\"message\":\"m\",\"kind\":\"done\"}}\n\n" }.joined()
            return (200, AsyncThrowingStream { $0.yield(Data((": connected\n\n" + wire).utf8)) })
        })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), sleep: { _ in })
        let posted = Recorder<String>()
        model.posted = { posted.add($0.notificationID) }

        model.start()

        try await waitUntil { model.inbox.items.count == 2 }
        XCTAssertEqual(posted.values, ["n1"])
        XCTAssertEqual(model.inbox.unread, ["m|n1"])
        try model.remove("m")
    }
}

extension ModelTests {
    @MainActor
    func testGoneCutsStacksOnlyWhereTheStoreFetched() async throws {
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "m", name: "Mac", hosts: ["100.64.0.1"], port: 1), token: "t")
        let transport = FakeTransport(reply: { request in
            switch FakeTransport.rpc(request) {
            case "workspaces": (FakeTransport.result(workspacesJSON), 200)
            case AgentCommand.sessions: (FakeTransport.result(#"[{"id":"agt_1","workspace":"w","title":"Kept","status":"idle"}]"#), 200)
            default: (FakeTransport.result(helloJSON), 200)
            }
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), sleep: { _ in })
        model.start()
        var stacks = TabStacks()
        stacks.select(computer: "m", workspace: "other")
        stacks.push(.session("agt_2"))
        stacks.select(computer: "m", workspace: "w")
        stacks.push(.session("agt_1"))
        stacks.push(.session("agt_2"))
        stacks.push(.session("agt_1"))
        let agents = try XCTUnwrap(model.agents(computer: "m", workspace: "w"))

        await agents.refresh()
        stacks.drop(where: model.gone)

        XCTAssertEqual(stacks.path.map(\.destination), [.session("agt_1")])
        XCTAssertEqual(stacks.path(for: StackKey(computer: "m", workspace: "other", tab: .agents)).map(\.destination), [.session("agt_2")])
        try model.remove("m")
    }
}
