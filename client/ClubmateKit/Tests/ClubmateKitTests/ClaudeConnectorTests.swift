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

    func testRunnerReturnsStatusAndOutput() async {
        let result = await SystemProcessRunner().run("/bin/sh", ["-c", "echo out; echo err >&2; exit 3"])
        XCTAssertEqual(result, ProcessOutput(status: 3, output: "out\nerr\n"))
    }

    func testRunnerKillsAProcessIgnoringTerm() async throws {
        let dir = try await TestCore.makeTempDir()
        defer { try? FileManager.default.removeItem(at: dir) }
        let pidFile = dir.appending(path: "pid").path(percentEncoded: false)
        let started = ContinuousClock.now
        let result = await SystemProcessRunner(timeout: .milliseconds(300)).run("/bin/sh", ["-c", "trap '' TERM; sleep 30 & echo $! > '\(pidFile)'; echo started; wait"])
        XCTAssertLessThan(ContinuousClock.now - started, .seconds(2))
        XCTAssertNotEqual(result.status, 0)
        XCTAssertEqual(result.output, "started\n")
        let pid = try XCTUnwrap(Int32(String(contentsOfFile: pidFile, encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)))
        let gone = ContinuousClock.now + .seconds(2)
        while kill(pid, 0) == 0, ContinuousClock.now < gone {
            try await Task.sleep(for: .milliseconds(20))
        }
        XCTAssertNotEqual(kill(pid, 0), 0)
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
