import Observation
import XCTest

import CompanionKit

@testable import Companion

private struct OneMac: HTTPTransport {
    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let body = (try? JSONSerialization.jsonObject(with: request.httpBody ?? Data())) as? [String: Any]
        let result = body?["method"] as? String == "workspaces"
            ? #"[{"id":"w1","name":"shop"},{"id":"w0","name":"Home","home":true}]"#
            : #"{"computer":{"id":"mac","name":"Mac Studio"},"version":"0.0.13","commands":[],"topics":["agents.changed"]}"#
        return (Data("{\"result\":\(result)}".utf8), try XCTUnwrap(HTTPURLResponse(url: try XCTUnwrap(request.url), statusCode: 200, httpVersion: nil, headerFields: nil)))
    }

    func stream(_ request: URLRequest) async throws -> (HTTPURLResponse, AsyncThrowingStream<Data, any Error>) {
        (try XCTUnwrap(HTTPURLResponse(url: try XCTUnwrap(request.url), statusCode: 200, httpVersion: nil, headerFields: nil)), AsyncThrowingStream { $0.yield(Data(": connected\n\n".utf8)) })
    }
}

@MainActor
private func waitUntil(timeout: Duration = .seconds(5), _ condition: @MainActor () -> Bool) async throws {
    let deadline = ContinuousClock.now + timeout
    while !condition() {
        guard ContinuousClock.now < deadline else { throw CancellationError() }
        try await Task.sleep(for: .milliseconds(5))
    }
}

@MainActor
final class NavigationTrackingTests: XCTestCase {
    private final class Hits: @unchecked Sendable {
        var tabs: Set<CompanionTab> = []
    }

    func testAPushInvalidatesOnlyReadersOfItsOwnTab() {
        let navigation = AppNavigation(restore: false)
        navigation.switchWorkspace(computer: "mac", workspace: "shop")
        let hits = Hits()
        for tab in [CompanionTab.agents, .backlog] {
            withObservationTracking { _ = navigation.path(tab) } onChange: { hits.tabs.insert(tab) }
        }
        navigation.push(.session("agt_1"))
        XCTAssertEqual(hits.tabs, [.agents])
        XCTAssertEqual(navigation.path(.agents).map(\.destination), [.session("agt_1")])
    }

    func testEachWorkspaceKeepsItsOwnPaths() {
        let navigation = AppNavigation(restore: false)
        navigation.switchWorkspace(computer: "mac", workspace: "shop")
        navigation.push(.session("agt_1"))
        navigation.switchWorkspace(computer: "mac", workspace: "blog")
        XCTAssertEqual(navigation.path(.agents), [])
        navigation.switchWorkspace(computer: "mac", workspace: "shop")
        XCTAssertEqual(navigation.path(.agents).map(\.destination), [.session("agt_1")])
    }

    func testFirstConnectOpensHome() async throws {
        let store = ComputerStore(file: nil, vault: MemoryTokenVault())
        try store.save(Computer(id: "mac", name: "Mac Studio", hosts: ["100.64.0.1"], port: 47621), token: "t")
        let model = CompanionModel(store: store, transport: OneMac(), device: DeviceIdentity(id: "d", name: "P", model: "iPhone"), sleep: { _ in })
        model.start()
        try await waitUntil { model.connection("mac")?.workspaces.count == 2 }
        let navigation = AppNavigation(restore: true, defaults: try XCTUnwrap(UserDefaults(suiteName: UUID().uuidString)))

        navigation.follow(model)

        XCTAssertEqual(navigation.workspaceKey, WorkspaceKey(computer: "mac", workspace: "w0"))
        model.connection("mac")?.stop()
    }

    func testLastUsedWorkspacePerMacSurvivesARestartUntilTheMacIsForgotten() throws {
        let defaults = try XCTUnwrap(UserDefaults(suiteName: UUID().uuidString))
        let navigation = AppNavigation(restore: true, defaults: defaults)
        navigation.switchWorkspace(computer: "pro", workspace: "infra")
        navigation.switchWorkspace(computer: "studio", workspace: "shop")

        let restarted = AppNavigation(restore: true, defaults: defaults)
        XCTAssertEqual(restarted.workspace("pro"), "infra")
        XCTAssertEqual(restarted.workspace("studio"), "shop")
        restarted.forget("pro")

        XCTAssertNil(AppNavigation(restore: true, defaults: defaults).workspace("pro"))
    }
}
