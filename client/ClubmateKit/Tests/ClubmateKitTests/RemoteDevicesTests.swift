import CoreImage
import Foundation
import XCTest

import ClubmateKit

@MainActor
final class RemoteDevicesTests: XCTestCase {
    private var core: TestCore?

    override func tearDown() async throws {
        await core?.stop()
        core = nil
    }

    func testLoadDecodesAndSortsDevices() async throws {
        let fake = FakeRemote()
        fake.devices = """
        [{"id":"old","name":"iPad","model":"iPad","pairedAt":"2026-10-01T10:00:00Z","lastSeen":"0001-01-01T00:00:00Z","connected":false},
         {"id":"new","name":"Phone","model":"iPhone","pairedAt":"2026-10-29T10:00:00.123456789+01:00","lastSeen":"2026-10-30T09:00:00.5Z","connected":true}]
        """
        let model = fake.model()

        await model.load()

        XCTAssertEqual(model.phase, .ready)
        XCTAssertEqual(model.status?.summary, "Listening on mac.tailnet.ts.net:47621")
        XCTAssertEqual(model.devices.map(\.id), ["new", "old"])
        XCTAssertNil(model.devices.last?.lastSeen)
        XCTAssertNotNil(model.devices.first?.lastSeen)
    }

    func testStatusSummary() {
        XCTAssertEqual(RemoteStatus(enabled: false, port: 47621, listening: [], hosts: [], error: "").summary, "Off")
        XCTAssertEqual(RemoteStatus(enabled: true, port: 47621, listening: [], hosts: [], error: "no Tailscale address").state, .failed)
        XCTAssertEqual(RemoteStatus(enabled: true, port: 47621, listening: ["[fd7a::1]:47621"], hosts: ["fd7a::1"], error: "").summary, "Listening on [fd7a::1]:47621")
    }

    func testPresence() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let locale = Locale(identifier: "en_US")
        func device(_ lastSeen: Date?, connected: Bool = false) -> RemoteDevice {
            RemoteDevice(id: "d", name: "Phone", model: "iPhone", pairedAt: now, lastSeen: lastSeen, connected: connected)
        }
        XCTAssertEqual(device(now, connected: true).presence(now: now, locale: locale), "Connected")
        XCTAssertEqual(device(nil).presence(now: now, locale: locale), "Never connected")
        XCTAssertEqual(device(now.addingTimeInterval(-20)).presence(now: now, locale: locale), "Last seen just now")
        XCTAssertEqual(device(now.addingTimeInterval(-7200)).presence(now: now, locale: locale), "Last seen 2 hours ago")
    }

    func testCountdownAndComputerName() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let pairing = RemotePairing(link: "clubmate://pair?v=1&c=ABC&h=100.1.2.3&p=47621&id=c1&n=Mac%20Studio&e=1", code: "ABC", expiresAt: now.addingTimeInterval(61))
        XCTAssertEqual(pairing.computerName, "Mac Studio")
        XCTAssertEqual(pairing.countdown(now: now), "1:01")
        XCTAssertNil(pairing.countdown(now: now.addingTimeInterval(61)))
    }

    func testRepairingSameDeviceCountsAsNew() {
        let now = Date()
        let old = RemoteDevice(id: "d", name: "Phone", model: "iPhone", pairedAt: now, lastSeen: nil, connected: false)
        let again = RemoteDevice(id: "d", name: "Phone", model: "iPhone", pairedAt: now.addingTimeInterval(5), lastSeen: nil, connected: false)
        XCTAssertNil(RemoteDevice.newlyPaired(from: [old], to: [old]))
        XCTAssertEqual(RemoteDevice.newlyPaired(from: [old], to: [again]), again)
    }

    func testPairingClosesWhenDeviceAppears() async throws {
        let fake = FakeRemote()
        let model = fake.model()
        await model.load()

        await model.pair()
        XCTAssertEqual(model.pairing?.code, "ABC")
        XCTAssertNotNil(model.qr)
        await model.load()
        XCTAssertNotNil(model.pairing)

        fake.devices = #"[{"id":"p","name":"Vadim's iPhone","model":"iPhone","pairedAt":"2026-10-30T10:00:00Z","connected":true}]"#
        await model.load()

        XCTAssertNil(model.pairing)
        XCTAssertEqual(model.notice, "Paired Vadim's iPhone")
    }

    func testPairingClosesWhenAccessIsTurnedOff() async throws {
        let fake = FakeRemote()
        let model = fake.model()
        await model.pair()
        XCTAssertNotNil(model.pairing)

        fake.status = #"{"enabled":false,"port":47621,"listening":[],"hosts":[],"error":""}"#
        await model.load()

        XCTAssertNil(model.pairing)
        XCTAssertNil(model.qr)
    }

    func testDoneCancelsCode() async throws {
        let fake = FakeRemote()
        let model = fake.model()
        await model.pair()

        await model.done()

        XCTAssertNil(model.pairing)
        XCTAssertEqual(fake.calls.last, CommandInvocation(id: "remote.cancel-pair", args: .object([:])))
    }

    func testEnableUpdatesStatus() async throws {
        let fake = FakeRemote()
        let model = fake.model()

        await model.setEnabled(false)

        XCTAssertEqual(fake.calls, [CommandInvocation(id: "remote.enable", args: .object(["enabled": .bool(false)]))])
        XCTAssertEqual(model.status?.state, .off)
    }

    func testSetColourDecodesIdentity() async throws {
        let fake = FakeRemote()
        let model = fake.model()

        await model.setColour("magenta")

        XCTAssertEqual(fake.calls, [CommandInvocation(id: "remote.set-colour", args: .object(["colour": .string("magenta")]))])
        XCTAssertEqual(model.status?.computer, RemoteComputer(id: "c1", name: "Mac Studio", colour: "magenta"))
        XCTAssertEqual(model.status?.otherMacs, [RemoteOtherMac(name: "MacBook Pro", colour: "blue", devices: ["iPhone"])])
        XCTAssertEqual(model.status?.titleBarColour, false)
        XCTAssertEqual(model.status?.titleDot, true)
    }

    func testDecodesRelayFieldsAndOldPlugin() throws {
        let new = try decoded(#"{"enabled":true,"relay":{"mode":"manual","url":"https://relay.example.com","state":"ready","detail":"Ready"},"tunnel":{"on":true,"url":"https://relay.example.com","connected":true,"error":"","channels":2,"state":"connected","detail":"Connected"},"tailnet":{"state":"off","severity":"note","detail":"Tailscale is stopped on this Mac. Phones connect through the relay."},"pairReady":false,"pairHint":"Start Tailscale"}"#, as: RemoteStatus.self)
        XCTAssertEqual(new.relay, RemoteRelay(mode: "manual", url: "https://relay.example.com", state: "ready", detail: "Ready"))
        XCTAssertEqual(new.relay?.host, "relay.example.com")
        XCTAssertEqual(new.tunnel, RemoteTunnel(on: true, url: "https://relay.example.com", connected: true, channels: 2, state: "connected", detail: "Connected"))
        XCTAssertEqual(new.tailnet, RemoteTailnet(state: "off", severity: .note, detail: "Tailscale is stopped on this Mac. Phones connect through the relay."))
        XCTAssertEqual(new.pairReady, false)
        XCTAssertEqual(new.pairHint, "Start Tailscale")

        let old = try decoded(#"{"enabled":true,"tunnel":{"on":false}}"#, as: RemoteStatus.self)
        XCTAssertNil(old.relay)
        XCTAssertNil(old.tailnet)
        XCTAssertEqual(old.tunnel, RemoteTunnel(on: false))
        XCTAssertNil(old.pairReady)

        let pairing = try decoded(#"{"link":"clubmate://pair?v=2","code":"A","expiresAt":"2026-10-30T10:05:00Z","relay":true}"#, as: RemotePairing.self)
        XCTAssertTrue(pairing.relay)
    }

    func testPairReadinessComesFromPlugin() async throws {
        let fake = FakeRemote()
        let model = fake.model()
        fake.status = #"{"enabled":true,"port":47621,"listening":[],"hosts":[],"error":"no Tailscale address","pairReady":true}"#
        await model.load()
        XCTAssertTrue(model.pairReady)
        XCTAssertEqual(model.pairHint, "")

        fake.status = #"{"enabled":true,"port":47621,"listening":["100.1.2.3:47621"],"hosts":["100.1.2.3"],"error":"","pairReady":false,"pairHint":"Turn on the relay"}"#
        await model.load()
        XCTAssertFalse(model.pairReady)
        XCTAssertEqual(model.pairHint, "Turn on the relay")

        fake.status = #"{"enabled":true,"port":47621,"listening":[],"hosts":[],"error":"no Tailscale address"}"#
        await model.load()
        XCTAssertEqual(model.pairHint, RemoteDevicesModel.tailscaleHint)
    }

    func testRelayActionsSendCommands() async throws {
        let fake = FakeRemote()
        let model = fake.model()

        await model.setTunnel(true)
        XCTAssertEqual(fake.calls.last, CommandInvocation(id: "remote.set-tunnel", args: .object(["on": .bool(true)])))
        XCTAssertEqual(model.status?.tunnel?.on, true)

        fake.confirmed = false
        await model.rotateRelayKey()
        XCTAssertFalse(fake.calls.contains { $0.id == "remote.tunnel-rotate" })

        fake.confirmed = true
        await model.rotateRelayKey()
        XCTAssertEqual(fake.asked, [RemoteDevicesModel.rotate, RemoteDevicesModel.rotate])
        XCTAssertEqual(fake.calls.last, CommandInvocation(id: "remote.tunnel-rotate", args: .object([:])))
        XCTAssertNil(model.error)
    }

    func testRevokeNeedsConfirmation() async throws {
        let fake = FakeRemote()
        let model = fake.model()
        let device = RemoteDevice(id: "d1", name: "Phone", model: "iPhone", pairedAt: Date(), lastSeen: nil, connected: false)

        fake.confirmed = false
        await model.revoke(device)
        XCTAssertFalse(fake.calls.contains { $0.id == "remote.revoke" })

        fake.confirmed = true
        await model.revoke(device)
        XCTAssertEqual(fake.asked, [RemoteDevicesModel.revoke, RemoteDevicesModel.revoke])
        XCTAssertTrue(fake.calls.contains(CommandInvocation(id: "remote.revoke", args: .object(["device": .string("d1")]))))
        XCTAssertNil(model.error)
    }

    func testMissingPluginIsUnavailable() async throws {
        let fake = FakeRemote()
        fake.failure = .rpc(code: -32000, message: #"unknown command: "remote.status""#)
        let model = fake.model()

        await model.load()

        XCTAssertEqual(model.phase, .unavailable(RemoteDevicesModel.missingPlugin))
    }

    func testQRCodeIsCrisp() throws {
        let image = try XCTUnwrap(QRCode.image("clubmate://pair?v=1&c=ABC", minSide: 500))
        XCTAssertGreaterThanOrEqual(image.width, 500)
        XCTAssertEqual(image.width, image.height)
        let modules = try XCTUnwrap(CIFilter(name: "CIQRCodeGenerator", parameters: ["inputMessage": Data("clubmate://pair?v=1&c=ABC".utf8), "inputCorrectionLevel": "M"])?.outputImage).extent.width
        XCTAssertEqual(image.width % Int(modules), 0)
    }

    func testFollowReloadsOnRemoteChanged() async throws {
        let core = try await TestCore.start()
        self.core = core
        let client = try await CoreClient.connect(paths: core.paths)
        let fake = FakeRemote()
        let model = fake.model()
        let follower = Task { await model.follow(client) }
        try await waitUntil { fake.calls.count >= 2 }
        let before = fake.calls.count

        try core.cli("bus", "publish", RemoteDevicesModel.topic, "{}")

        try await waitUntil { fake.calls.count > before }
        follower.cancel()
        await client.close()
    }
}

private func decoded<T: Decodable>(_ text: String, as type: T.Type) throws -> T {
    try JSONDecoder().decode(JSONValue.self, from: Data(text.utf8)).decode(type)
}

@MainActor
private final class FakeRemote {
    var status = #"{"enabled":true,"port":47621,"listening":["100.101.1.2:47621"],"hosts":["mac.tailnet.ts.net","100.101.1.2"],"error":""}"#
    var devices = "null"
    var failure: ClubmateError?
    var confirmed = true
    private(set) var calls: [CommandInvocation] = []
    private(set) var asked: [RemoteDevicesModel.Ask] = []

    func model() -> RemoteDevicesModel {
        RemoteDevicesModel(
            run: { [unowned self] invocation in self.answer(invocation) },
            confirm: { [unowned self] ask in
                self.asked.append(ask)
                return self.confirmed
            }
        )
    }

    private func answer(_ invocation: CommandInvocation) -> Result<JSONValue, ClubmateError> {
        calls.append(invocation)
        if let failure { return .failure(failure) }
        switch invocation.id {
        case "remote.status": return .success(json(status))
        case "remote.devices": return .success(json(devices))
        case "remote.enable": return .success(json(#"{"enabled":false,"port":47621,"listening":[],"hosts":[],"error":""}"#))
        case "remote.set-colour": return .success(json(#"{"enabled":true,"computer":{"id":"c1","name":"Mac Studio","colour":"magenta"},"titleBarColour":false,"otherMacs":[{"name":"MacBook Pro","colour":"blue","devices":["iPhone"]}],"titleDot":true}"#))
        case "remote.set-tunnel", "remote.tunnel-rotate": return .success(json(#"{"enabled":true,"tunnel":{"on":true,"state":"connecting","detail":"Connecting…"}}"#))
        case "remote.pair": return .success(json(#"{"link":"clubmate://pair?v=1&c=ABC&n=Mac","code":"ABC","expiresAt":"2026-10-30T10:05:00Z"}"#))
        default: return .success(.null)
        }
    }

    private func json(_ text: String) -> JSONValue {
        (try? JSONDecoder().decode(JSONValue.self, from: Data(text.utf8))) ?? .null
    }
}
