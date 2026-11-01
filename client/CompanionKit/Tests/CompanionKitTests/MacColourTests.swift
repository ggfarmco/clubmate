import Foundation
import XCTest

import CompanionKit

@MainActor
final class MacColourTests: XCTestCase {
    private struct Macs {
        let model: CompanionModel
        let transport: FakeTransport
        let streams: Recorder<AsyncThrowingStream<Data, any Error>.Continuation>
        let file: URL
    }

    private nonisolated static let studioHost = "100.64.0.1"
    private nonisolated static let oldHost = "100.64.0.2"

    private func pairedMacs() throws -> Macs {
        let file = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString).appending(path: "computers.json")
        let store = ComputerStore(file: file, vault: MemoryTokenVault())
        try store.save(Computer(id: "mac1", name: "Mac", hosts: [Self.studioHost], port: 1), token: "t")
        try store.save(Computer(id: "mac2", name: "MacBook Air", hosts: [Self.oldHost], port: 1), token: "t")
        let streams = Recorder<AsyncThrowingStream<Data, any Error>.Continuation>()
        let transport = FakeTransport(reply: { request in
            let old = request.url?.host() == Self.oldHost
            switch FakeTransport.rpc(request) {
            case "workspaces": return (FakeTransport.result("[]"), 200)
            case "hello" where old: return (FakeTransport.result(#"{"computer":{"id":"mac2","name":"MacBook Air"},"version":"0.0.13","commands":[],"topics":[]}"#), 200)
            case "hello": return (FakeTransport.result(#"{"computer":{"id":"mac1","name":"Mac Studio","colour":"blue"},"version":"0.0.16","commands":[],"topics":[]}"#), 200)
            case "macs" where old: return (Data(#"{"error":{"code":"invalid_params","message":"unknown method"}}"#.utf8), 200)
            case "macs": return (FakeTransport.result("null"), 200)
            case "setIdentityColour": return (FakeTransport.result(#"{"computer":{"id":"mac1","name":"Mac Studio","colour":"magenta"}}"#), 200)
            default: return (FakeTransport.result("null"), 200)
            }
        }, stream: { request in
            let old = request.url?.host() == Self.oldHost
            return (200, AsyncThrowingStream {
                $0.yield(Data(": connected\n\n".utf8))
                if !old { streams.add($0) }
            })
        })
        let model = CompanionModel(store: store, transport: transport, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), sleep: { _ in })
        return Macs(model: model, transport: transport, streams: streams, file: file)
    }

    private func sentMacs(_ transport: FakeTransport, to host: String) -> [[MacTag]] {
        transport.requests.filter { $0.url?.host() == host && FakeTransport.rpc($0) == "macs" }.compactMap { request in
            try? JSONDecoder().decode(JSONValue.self, from: request.httpBody ?? Data())["params"]?["macs"]?.decode([MacTag].self)
        }
    }

    func testUnknownTokenIsGraphite() {
        XCTAssertEqual(MacColour(token: "teal"), .graphite)
        XCTAssertEqual(MacColour(token: "magenta"), .magenta)
    }

    func testEachMacHearsAboutTheOthersAfterHelloAndAnOlderMacStaysOnline() async throws {
        let macs = try pairedMacs()
        macs.model.start()
        try await waitUntil {
            self.sentMacs(macs.transport, to: Self.oldHost).last == [MacTag(name: "Mac Studio", colour: "blue")]
                && self.sentMacs(macs.transport, to: Self.studioHost).last == [MacTag(name: "MacBook Air", colour: nil)]
        }
        XCTAssertEqual(macs.model.connection("mac2")?.state, .online)
        XCTAssertEqual(macs.model.tag("mac1"), MacTag(name: "Mac Studio", colour: "blue"))
        XCTAssertEqual(macs.model.tag("mac2")?.macColour, .graphite)
        try macs.model.remove("mac2")
        XCTAssertNil(macs.model.tag("mac1"))
        try macs.model.remove("mac1")
    }

    func testAColourChangedOnEitherSideIsSavedAndSentToTheOtherMacs() async throws {
        let macs = try pairedMacs()
        macs.model.start()
        try await waitUntil { macs.streams.values.count == 1 && macs.model.connection("mac2")?.state == .online }

        let event = #"{"seq":1,"topic":"remote.identity","payload":{"computer":{"id":"mac1","name":"Mac Studio","colour":"sky"}}}"#
        macs.streams.values[0].yield(Data("id: 1\nevent: bus\ndata: \(event)\n\n".utf8))
        try await waitUntil { self.sentMacs(macs.transport, to: Self.oldHost).last == [MacTag(name: "Mac Studio", colour: "sky")] }
        XCTAssertEqual(ComputerStore(file: macs.file, vault: MemoryTokenVault()).computers.first?.colour, "sky")

        try await macs.model.setColour(.magenta, computer: "mac1")
        try await waitUntil { self.sentMacs(macs.transport, to: Self.oldHost).last == [MacTag(name: "Mac Studio", colour: "magenta")] }
        let request = try XCTUnwrap(macs.transport.requests.first { FakeTransport.rpc($0) == "setIdentityColour" })
        XCTAssertEqual(try JSONDecoder().decode(JSONValue.self, from: request.httpBody ?? Data())["params"], .object(["colour": .string("magenta")]))
        XCTAssertEqual(macs.model.computers.first?.colour, "magenta")
        try macs.model.remove("mac1")
        try macs.model.remove("mac2")
    }
}
