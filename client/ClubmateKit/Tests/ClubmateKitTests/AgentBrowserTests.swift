import Foundation
import WebKit
import XCTest

import ClubmateProtocol

@testable import ClubmateKit

@MainActor
final class AgentBrowserTests: XCTestCase {
    private var server: FixtureServer?
    private var hosts: [FakeHost] = []

    override func tearDown() async throws {
        server?.stop()
        server = nil
        hosts.forEach { $0.releaseAll() }
        hosts = []
    }

    func testCookiesAreSharedOnlyWithinAWorkspace() async throws {
        let server = try await serve { path, headers in
            path == "/set"
                ? FixtureResponse(headers: ["Set-Cookie": "k=v; Path=/"], body: "<title>set</title>")
                : FixtureResponse(body: "<title>\(headers["cookie"] ?? "none")</title>")
        }
        let host = FakeHost()
        let browser = makeBrowser(host)
        _ = try await browser.handle(open("agt_1", server.url("/set")))
        let same = try await browser.handle(open("agt_2", server.url("/echo")))
        let other = try await browser.handle(open("agt_2", server.url("/echo"), workspace: "w2"))
        XCTAssertTrue(same["title"]?.stringValue?.contains("k=v") == true, "\(same)")
        XCTAssertEqual(other["title"]?.stringValue, "none")
        XCTAssertEqual(host.attached.count, 3)
        do {
            _ = try await browser.handle(BrowserCall(owner: "agt_1", tab: "main", action: .snapshot))
            XCTFail("call without a workspace ran")
        } catch let error as BrowserError {
            XCTAssertTrue(error.message.contains("no workspace"), error.message)
        }
    }

    func testDataStoreIDIsStablePerWorkspace() {
        let base = UUID(uuidString: "67AF17A3-1824-46B4-B244-FB138FC9C4A7")!
        let a = AgentBrowser.dataStoreID(base: base, workspace: "ws_a")
        XCTAssertEqual(a, AgentBrowser.dataStoreID(base: base, workspace: "ws_a"))
        XCTAssertNotEqual(a, AgentBrowser.dataStoreID(base: base, workspace: "ws_b"))
        XCTAssertNotEqual(a, base)
        XCTAssertEqual(Array(a.uuidString)[14], "5")
    }

    func testCallsOnOneTabRunInOrder() async throws {
        let server = try await serve { _, _ in FixtureResponse(body: "<title>slow</title>", delay: .milliseconds(500)) }
        let browser = makeBrowser(FakeHost())
        let opening = Task.immediate { try await browser.handle(self.open("agt_1", server.url("/slow"))) }
        let snapshot = try await browser.handle(BrowserCall(workspace: "w1", owner: "agt_1", tab: "main", action: .snapshot))
        _ = try await opening.value
        XCTAssertTrue(snapshot["url"]?.stringValue?.hasSuffix("/slow") == true, "\(snapshot)")
    }

    func testCloseOwnerFailsInFlightAndDetaches() async throws {
        let server = try await serve { path, _ in path == "/hang" ? .never : FixtureResponse(body: "<title>p</title>") }
        let host = FakeHost()
        let browser = makeBrowser(host)
        _ = try await browser.handle(open("agt_1", server.url("/p"), tab: "b"))
        let started = ContinuousClock.now
        let hanging = Task.immediate { try await browser.handle(self.open("agt_1", server.url("/hang"))) }
        let queued = Task.immediate { try await browser.handle(BrowserCall(workspace: "w1", owner: "agt_1", tab: "main", action: .snapshot)) }
        try await Task.sleep(for: .milliseconds(300))
        browser.close(owner: "agt_1")
        for call in [hanging, queued] {
            do {
                _ = try await call.value
                XCTFail("call on a closed tab returned")
            } catch let error as BrowserError {
                XCTAssertEqual(error.message, "tab closed")
            }
        }
        XCTAssertLessThan(ContinuousClock.now - started, .seconds(3))
        XCTAssertEqual(Set(host.detached.map(ObjectIdentifier.init)), Set(host.attached.map(ObjectIdentifier.init)))
        _ = try await browser.handle(open("agt_1", server.url("/p"), tab: "b"))
        XCTAssertEqual(host.attached.count, 3)
    }

    func testHandoffDoneAndTimeout() async throws {
        let host = FakeHost()
        let browser = makeBrowser(host)
        let started = ContinuousClock.now
        let expired = try await browser.handle(handoff("agt_1", timeout: 1))
        XCTAssertEqual(expired, .object(["result": .string("no-human")]))
        XCTAssertLessThan(ContinuousClock.now - started, .milliseconds(1800))
        XCTAssertEqual(host.cancels, 1)
        let waiting = Task.immediate { try await browser.handle(self.handoff("agt_1", timeout: 30)) }
        try await host.waitForHandoff(2)
        host.resolve(true)
        let done = try await waiting.value
        XCTAssertEqual(done, .object(["result": .string("done")]))
    }

    func testSecondHandoffWaitsForFirst() async throws {
        let host = FakeHost()
        let browser = makeBrowser(host)
        let started = ContinuousClock.now
        let first = Task.immediate { try await browser.handle(self.handoff("agt_1", timeout: 1)) }
        let second = Task.immediate { try await browser.handle(self.handoff("agt_2", timeout: 2)) }
        let results = try await [first.value, second.value]
        XCTAssertEqual(results, Array(repeating: .object(["result": .string("no-human")]), count: 2))
        XCTAssertLessThan(ContinuousClock.now - started, .milliseconds(2800))
        XCTAssertEqual(host.handoffs, 2)
        XCTAssertEqual(host.maxOpen, 1)
    }

    func testCloseOwnerEndsActiveHandoff() async throws {
        let host = FakeHost()
        let browser = makeBrowser(host)
        let first = Task.immediate { try await browser.handle(self.handoff("agt_1", timeout: 30)) }
        try await host.waitForHandoff(1)
        let second = Task.immediate { try await browser.handle(self.handoff("agt_2", timeout: 30)) }
        try await Task.sleep(for: .milliseconds(100))
        browser.close(owner: "agt_1")
        do {
            _ = try await first.value
            XCTFail("handoff on a closed tab returned")
        } catch let error as BrowserError {
            XCTAssertEqual(error.message, "tab closed")
        }
        try await host.waitForHandoff(2)
        XCTAssertEqual(host.cancels, 1)
        host.resolve(true)
        let done = try await second.value
        XCTAssertEqual(done, .object(["result": .string("done")]))
    }

    func testOwnerClosedByBusEvents() {
        func changed(_ kind: String, status: String = "running") -> BusEvent {
            BusEvent(seq: 1, topic: "agents.changed", payload: .object(["workspace": .string("w"), "session": .string("agt_1"), "kind": .string(kind), "status": .string(status)]))
        }
        XCTAssertEqual(AgentBrowser.owner(closedBy: changed("deleted")), "agt_1")
        XCTAssertEqual(AgentBrowser.owner(closedBy: changed("status", status: "closed")), "agt_1")
        XCTAssertNil(AgentBrowser.owner(closedBy: changed("status")))
        XCTAssertEqual(AgentBrowser.owner(closedBy: BusEvent(seq: 2, topic: "session.removed", payload: .object(["id": .string("ses_1")]))), "ses_1")
    }

    func testSafariUserAgent() {
        XCTAssertEqual(
            AgentBrowser.safariUserAgent(version: "27.2"),
            "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/27.2 Safari/605.1.15"
        )
    }

    private func serve(_ handler: @escaping FixtureServer.Handler) async throws -> FixtureServer {
        let server = try await FixtureServer(handler: handler)
        self.server = server
        return server
    }

    private func makeBrowser(_ host: FakeHost) -> AgentBrowser {
        hosts.append(host)
        return AgentBrowser(
            host: host,
            dataStore: { _ in .nonPersistent() },
            userAgent: "ClubmateTests",
            screenshots: FileManager.default.temporaryDirectory.appending(path: "AgentBrowserTests"),
            navigationTimeout: .seconds(10),
            allowsLocal: true
        )
    }

    private func open(_ owner: String, _ url: URL, tab: String = "main", workspace: String = "w1") -> BrowserCall {
        BrowserCall(workspace: workspace, owner: owner, tab: tab, action: .open, args: .object(["url": .string(url.absoluteString)]))
    }

    private func handoff(_ owner: String, timeout: Int) -> BrowserCall {
        BrowserCall(workspace: "w1", owner: owner, tab: "main", action: .handoff, args: .object(["reason": .string("captcha")]), timeout: timeout)
    }
}

@MainActor
private final class FakeHost: BrowserHost {
    var attached: [WKWebView] = []
    var detached: [WKWebView] = []
    var cancels = 0
    var handoffs = 0
    var maxOpen = 0
    private var open = 0
    private var waiting: [CheckedContinuation<Bool, Never>] = []

    func attach(_ webView: WKWebView) {
        attached.append(webView)
    }

    func detach(_ webView: WKWebView) {
        detached.append(webView)
    }

    func handoff(_ webView: WKWebView, reason: String, owner: String) async -> Bool {
        handoffs += 1
        open += 1
        maxOpen = max(maxOpen, open)
        return await withCheckedContinuation { waiting.append($0) }
    }

    func cancelHandoff() {
        cancels += 1
        open -= 1
    }

    func resolve(_ done: Bool) {
        open -= 1
        waiting.removeLast().resume(returning: done)
    }

    func releaseAll() {
        waiting.forEach { $0.resume(returning: false) }
        waiting = []
    }

    func waitForHandoff(_ count: Int) async throws {
        while handoffs < count { try await Task.sleep(for: .milliseconds(10)) }
    }
}
