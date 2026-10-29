import Foundation
import XCTest

import ClubmateKit

final class ClaudeConnectorTests: XCTestCase {
    func testConnectReplacesExistingServer() async {
        let runner = FakeRunner(results: [
            "/bin/zsh -l -i -c command -v claude": ProcessOutput(status: 0, output: "welcome\n/opt/bin/claude\n"),
            "/opt/bin/claude mcp get clubmate": ProcessOutput(status: 0, output: "clubmate: …\n"),
        ])
        let connector = ClaudeConnector(runner: runner, shell: "/bin/zsh")
        let claude = await connector.locate()
        XCTAssertEqual(claude, "/opt/bin/claude")
        let result = await connector.connect(claude: "/opt/bin/claude", cli: "/App/bin/clubmate")
        XCTAssertEqual(result.status, 0)
        let calls = await runner.calls
        XCTAssertEqual(calls, [
            "/bin/zsh -l -i -c command -v claude",
            "/opt/bin/claude mcp get clubmate",
            "/opt/bin/claude mcp remove --scope user clubmate",
            "/opt/bin/claude mcp add --scope user clubmate -- /App/bin/clubmate mcp",
        ])
    }

    func testRunnerKillsAProcessIgnoringTerm() async {
        let started = ContinuousClock.now
        let result = await SystemProcessRunner(timeout: .milliseconds(300)).run("/bin/sh", ["-c", "trap '' TERM; echo started; sleep 3"])
        XCTAssertLessThan(ContinuousClock.now - started, .seconds(2))
        XCTAssertNotEqual(result.status, 0)
        XCTAssertEqual(result.output, "started\n")
    }
}

private actor FakeRunner: ProcessRunning {
    private let results: [String: ProcessOutput]
    private(set) var calls: [String] = []

    init(results: [String: ProcessOutput]) {
        self.results = results
    }

    nonisolated func run(_ executable: String, _ arguments: [String]) async -> ProcessOutput {
        await record(([executable] + arguments).joined(separator: " "))
    }

    private func record(_ call: String) -> ProcessOutput {
        calls.append(call)
        return results[call] ?? ProcessOutput(status: 0, output: "")
    }
}
