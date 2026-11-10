import Foundation
import XCTest

import ClubmateProtocol

@testable import ClubmateKit

@MainActor
final class BrowserProtocolTests: XCTestCase {
    private var core: TestCore?

    override func tearDown() async throws {
        await core?.stop()
        core = nil
    }

    func testDecodesCallAndKey() throws {
        let json = #"{"workspace":"ws_1","owner":"agt_1","tab":"main","action":"open","args":{"url":"https://x"}}"#
        let call = try JSONDecoder().decode(BrowserCall.self, from: Data(json.utf8))
        XCTAssertEqual(call.key, "agt_1/main@ws_1")
        XCTAssertEqual(call.action, .open)
        XCTAssertEqual(call.args.objectValue?["url"]?.stringValue, "https://x")
        XCTAssertNil(call.timeout)
    }

    func testDetectsBlockPages() {
        XCTAssertTrue(BlockDetector.blocked(status: 403, title: "Zugriff verweigert / Access denied", html: ""))
        XCTAssertTrue(BlockDetector.blocked(status: 403, title: "Just a moment...", html: "<div id=\"challenge-form\"></div>"))
        XCTAssertTrue(BlockDetector.blocked(status: 200, title: "x", html: "<iframe src=\"https://geo.captcha-delivery.com/…\">"))
        XCTAssertFalse(BlockDetector.blocked(status: 200, title: "Access denied", html: ""))
        XCTAssertFalse(BlockDetector.blocked(status: 200, title: "PKW-Suche bei mobile.de", html: "<main>Skoda Superb</main>"))
        XCTAssertFalse(BlockDetector.blocked(status: 404, title: "Not found", html: ""))
    }

    func testBrowserCallReachesHandlerAndErrorsMap() async throws {
        let core = try await TestCore.start()
        self.core = core
        let client = try await CoreClient.connect(paths: core.paths)
        let workspace = try await client.createWorkspace(name: "demo")
        try await client.register(handles: ["browser.call"])
        client.browserHandler = { call in
            if call.action == .back { throw BrowserError(message: "tab closed") }
            return .object(["ok": .bool(true), "key": .string(call.key)])
        }
        let run: (String) async throws -> JSONValue = { id in
            try await client.call(method: "command.run", params: .object([
                "id": .string(id), "workspaceId": .string(workspace.id), "agent": .bool(true), "args": .object([:]),
            ]))
        }
        let reply = try await run("browser.snapshot")
        XCTAssertEqual(reply.objectValue?["ok"], .bool(true))
        XCTAssertEqual(reply.objectValue?["key"]?.stringValue, "default/main@" + workspace.id)
        do {
            _ = try await run("browser.back")
            XCTFail("expected error")
        } catch {
            XCTAssertTrue("\(error)".contains("tab closed"), "\(error)")
        }
        await client.close()
    }
}
