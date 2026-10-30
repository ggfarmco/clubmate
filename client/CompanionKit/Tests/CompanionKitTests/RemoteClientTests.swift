import Foundation
import XCTest

import CompanionKit

final class RemoteClientTests: XCTestCase {
    func testFallsBackToNextHostAndRemembersIt() async throws {
        let transport = FakeTransport(reply: { request in
            guard request.url?.host() == "100.64.0.2" else { throw URLError(.cannotFindHost) }
            return (FakeTransport.result(helloJSON), 200)
        })
        let client = RemoteClient(hosts: ["mac.tail.ts.net", "100.64.0.2"], port: 47621, token: "cmt_x", transport: transport)
        _ = try await client.hello()
        _ = try await client.hello()
        XCTAssertEqual(transport.requests.map { $0.url?.host() }, ["mac.tail.ts.net", "100.64.0.2", "100.64.0.2"])
        XCTAssertEqual(transport.requests.last?.value(forHTTPHeaderField: "Authorization"), "Bearer cmt_x")
    }

    func testHelloProbesEachHostBrieflyExceptTheLast() async throws {
        let transport = FakeTransport(reply: { request in
            guard request.url?.host() == "100.64.0.3" else { throw URLError(.timedOut) }
            return (FakeTransport.result(helloJSON), 200)
        })
        let client = RemoteClient(hosts: ["mac.tail.ts.net", "100.64.0.2", "100.64.0.3"], port: 47621, token: "t", transport: transport)
        _ = try await client.hello()
        _ = try await client.command("agents.sessions", workspace: "w")
        XCTAssertEqual(transport.requests.map(\.timeoutInterval), [RemoteClient.probeTimeout, RemoteClient.probeTimeout, 20, 20])
    }

    func testMapsStatusesToErrors() async throws {
        let transport = FakeTransport(reply: { request in
            FakeTransport.rpc(request) == "hello"
                ? (Data(#"{"error":{"code":"unauthorized","message":"no"}}"#.utf8), 401)
                : (Data(#"{"error":{"code":"not_allowed","message":"agents.delete is not remote"}}"#.utf8), 200)
        })
        let client = RemoteClient(hosts: ["100.64.0.1"], port: 47621, token: "t", transport: transport)
        await XCTAssertThrowsAsync(try await client.hello(), RemoteError.unauthorized)
        await XCTAssertThrowsAsync(try await client.command("agents.delete", workspace: "w"), RemoteError.server(code: "not_allowed", message: "agents.delete is not remote"))
    }

    func testPairSendsCodeAndDevice() async throws {
        let transport = FakeTransport(reply: { _ in (Data(#"{"token":"cmt_abc","computer":{"id":"mac1","name":"Mac Studio"}}"#.utf8), 200) })
        let link = PairingLink(code: "CODE", hosts: ["fd7a:115c:a1e0::1"], port: 47621, computerID: "mac1", computerName: "Mac", expiresAt: .distantFuture)
        let device = DeviceIdentity(id: "dev1", name: "Phone", model: "iPhone")
        let result = try await RemoteClient.pair(link, device: device, transport: transport, now: .distantPast)
        XCTAssertEqual(result.token, "cmt_abc")
        XCTAssertEqual(result.computer, Computer(id: "mac1", name: "Mac Studio", hosts: ["fd7a:115c:a1e0::1"], port: 47621, pairedAt: .distantPast))
        let request = try XCTUnwrap(transport.requests.first)
        XCTAssertEqual(request.url?.absoluteString, "http://[fd7a:115c:a1e0::1]:47621/v1/pair")
        XCTAssertEqual(try JSONDecoder().decode(JSONValue.self, from: request.httpBody ?? Data()), .object(["code": .string("CODE"), "device": .object(["id": .string("dev1"), "name": .string("Phone"), "model": .string("iPhone")])]))
    }

    func testNeverContactsNonTailnetHosts() async throws {
        let transport = FakeTransport(reply: { _ in (FakeTransport.result(helloJSON), 200) })
        let client = RemoteClient(hosts: ["192.168.1.5", "mac.example.com", "127.0.0.1", "evil.com@x.ts.net", "evil.com#.ts.net"], port: 47621, token: "cmt_secret", transport: transport)
        await XCTAssertThrowsAsync(try await client.hello(), RemoteError.hostNotAllowed)
        XCTAssertEqual(transport.requests.count, 0)
    }

    func testPairRejectsAnotherComputer() async throws {
        let transport = FakeTransport(reply: { _ in (Data(#"{"token":"cmt_abc","computer":{"id":"other","name":"Imposter"}}"#.utf8), 200) })
        let link = PairingLink(code: "CODE", hosts: ["100.64.0.1"], port: 47621, computerID: "mac1", computerName: "Mac", expiresAt: .distantFuture)
        await XCTAssertThrowsAsync(try await RemoteClient.pair(link, device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), transport: transport), RemoteError.pairingFailed(RemoteClient.mismatchMessage))
    }
}

func XCTAssertThrowsAsync<T>(_ expression: @autoclosure () async throws -> T, _ expected: RemoteError, file: StaticString = #filePath, line: UInt = #line) async {
    do {
        _ = try await expression()
        XCTFail("no error", file: file, line: line)
    } catch {
        XCTAssertEqual(error as? RemoteError, expected, file: file, line: line)
    }
}
