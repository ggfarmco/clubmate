import Foundation
import XCTest

import CompanionKit

@MainActor
final class PushTests: XCTestCase {
    func testEnvironmentComesFromTheProfilePlistInsideTheSignedBlob() {
        let plist = #"<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>Entitlements</key><dict><key>aps-environment</key><string>development</string></dict></dict></plist>"#
        let profile = Data([0x30, 0x82, 0x01]) + Data(plist.utf8) + Data([0xA0, 0x82, 0x3C, 0x3F])
        XCTAssertEqual(PushToken.environment(profile: profile), "development")
        XCTAssertEqual(PushToken.environment(profile: nil), "production")
        XCTAssertEqual(PushToken.environment(profile: Data("<?xml version=\"1.0\"?><plist><dict/></plist>".utf8)), "production")
    }

    func testDeviceTokenIsLowercaseHex() {
        XCTAssertEqual(PushToken(device: Data([0x0A, 0xFF, 0x00]), environment: "production").token, "0aff00")
    }

    func testDeliveredNoticesMatchByIdentifierOrPayloadIDAndResolvedByComputerSessionAndKind() {
        let delivered = [
            DeliveredNotice(identifier: "mac1|n1", userInfo: ["computer": "mac1", "session": "s1", "kind": "permission"]),
            DeliveredNotice(identifier: "SYS-2", userInfo: ["id": "mac1|n2", "computer": "mac1", "session": "s1", "kind": "question"]),
            DeliveredNotice(identifier: "SYS-3", userInfo: ["id": "mac2|n3", "computer": "mac2", "session": "s1", "kind": "needs-input"]),
        ]
        XCTAssertEqual(DeliveredNotice.withdrawn(["mac1|n1", "mac1|n2"], from: delivered), ["mac1|n1", "SYS-2"])
        XCTAssertEqual(DeliveredNotice.resolved(["resolved": ["computer": "mac1", "session": "s1", "requestKind": "permission"]], from: delivered), ["mac1|n1"])
        XCTAssertEqual(DeliveredNotice.resolved(["resolved": ["computer": "mac1", "session": "s1", "requestKind": "elicitation"]], from: delivered), ["SYS-2"])
        XCTAssertNil(DeliveredNotice.resolved(["aps": ["content-available": 1]], from: delivered))
    }

    func testTokenGoesToEveryMacAfterHelloAndWhenItChangesAndAnOlderMacStaysOnlineAndIsClearedOnRemove() async throws {
        let file = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "computers.json")
        let store = ComputerStore(file: file, vault: MemoryTokenVault())
        try store.save(Computer(id: "mac1", name: "Mac", hosts: ["100.64.0.1"], port: 1), token: "t")
        try store.save(Computer(id: "mac2", name: "Old", hosts: ["100.64.0.2"], port: 1), token: "t")
        let transport = FakeTransport(reply: { request in
            let old = request.url?.host() == "100.64.0.2"
            switch FakeTransport.rpc(request) {
            case "workspaces": return (FakeTransport.result("[]"), 200)
            case "hello": return (FakeTransport.result(#"{"computer":{"id":"\#(old ? "mac2" : "mac1")","name":"M"},"version":"0.0.16","commands":[],"topics":[]}"#), 200)
            case "pushToken" where old: return (Data(#"{"error":{"code":"invalid_params","message":"unknown method"}}"#.utf8), 200)
            case "pushToken": return (FakeTransport.result(#"{"push":true}"#), 200)
            default: return (FakeTransport.result("null"), 200)
            }
        }, stream: { _ in (200, AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) }) })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), sleep: { _ in })
        func sent(_ host: String) -> [String] {
            transport.requests.filter { $0.url?.host() == host && FakeTransport.rpc($0) == "pushToken" }.compactMap { request in
                (try? JSONDecoder().decode(JSONValue.self, from: request.httpBody ?? Data()))?["params"].map { "\($0["token"]?.stringValue ?? "")/\($0["environment"]?.stringValue ?? "")" }
            }
        }

        model.registered(PushToken(token: "aa", environment: "development"))
        model.start()
        try await waitUntil { sent("100.64.0.1") == ["aa/development"] && sent("100.64.0.2") == ["aa/development"] }
        model.registered(PushToken(token: "bb", environment: "development"))
        try await waitUntil { sent("100.64.0.1").last == "bb/development" && sent("100.64.0.2").last == "bb/development" }
        XCTAssertEqual(model.connection("mac2")?.state, .online)

        model.disconnectAll()
        XCTAssertEqual(model.connection("mac1")?.state, .offline(nil))
        model.reconnectAll()
        try await waitUntil { sent("100.64.0.1") == ["aa/development", "bb/development", "bb/development"] && model.connection("mac1")?.state == .online }
        try model.remove("mac1")
        try model.remove("mac2")
        try await waitUntil { sent("100.64.0.1").last == "/" && sent("100.64.0.2").last == "/" }
        let cleared = transport.requests.last { $0.url?.host() == "100.64.0.1" && FakeTransport.rpc($0) == "pushToken" }
        XCTAssertEqual(cleared?.value(forHTTPHeaderField: "Authorization"), "Bearer t")
    }
}
