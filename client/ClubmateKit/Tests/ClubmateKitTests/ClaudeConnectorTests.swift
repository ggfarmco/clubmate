import Foundation
import XCTest

import ClubmateKit

final class ClaudeConnectorTests: XCTestCase {
    func testLocateTakesClaudeAndLoginPath() async {
        let runner = FakeRunner(results: [
            #"/bin/zsh -l -i -c command -v claude; printf '%s\n' "$PATH""#: ProcessOutput(status: 0, output: "welcome\n/tmp/motd\n\n/opt/bin/claude\n/opt/bin:/usr/bin\n"),
            "/opt/bin/claude mcp get clubmate": ProcessOutput(status: 0, output: "clubmate: …\n"),
        ])
        let connector = ClaudeConnector(runner: runner, shell: "/bin/zsh", serverName: "clubmate")
        let location = await connector.locate()
        XCTAssertEqual(location, ClaudeLocation(claude: "/opt/bin/claude", path: "/opt/bin:/usr/bin"))
        guard let location else { return }
        let result = await connector.connect(location, cli: "/App/bin/clubmate")
        XCTAssertEqual(result.status, 0)
        let calls = await runner.calls
        XCTAssertEqual(calls.map(\.command), [
            #"/bin/zsh -l -i -c command -v claude; printf '%s\n' "$PATH""#,
            "/opt/bin/claude mcp get clubmate",
            "/opt/bin/claude mcp remove --scope user clubmate",
            "/opt/bin/claude mcp add --scope user clubmate -- /App/bin/clubmate mcp",
        ])
        XCTAssertNil(calls[0].environment)
        XCTAssertTrue(calls.dropFirst().allSatisfy { $0.environment?.contains("PATH=/opt/bin:/usr/bin") == true })
    }

    func testRegistrationMatchesThisBundle() async {
        XCTAssertEqual(ClaudeConnector.serverName(flavor: "release"), "clubmate")
        XCTAssertEqual(ClaudeConnector.serverName(flavor: "dev"), "clubmate-dev")
        let location = ClaudeLocation(claude: "/opt/bin/claude", path: "/usr/bin")
        let entry = "clubmate-dev:\n  Scope: User config\n  Command: /App/bin/clubmate\n  Args: mcp\n"
        let runner = FakeRunner(results: ["/opt/bin/claude mcp get clubmate-dev": ProcessOutput(status: 0, output: entry)])
        let connector = ClaudeConnector(runner: runner, shell: "/bin/zsh", serverName: "clubmate-dev")
        let current = await connector.registration(location, cli: "/App/bin/clubmate")
        let other = await connector.registration(location, cli: "/Other/bin/clubmate")
        XCTAssertEqual(current, .current)
        XCTAssertEqual(other, .other("/App/bin/clubmate"))
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

private struct Call: Sendable {
    let command: String
    let environment: [String]?
}

private actor FakeRunner: ProcessRunning {
    private let results: [String: ProcessOutput]
    private(set) var calls: [Call] = []

    init(results: [String: ProcessOutput]) {
        self.results = results
    }

    nonisolated func run(_ executable: String, _ arguments: [String], environment: [String]?) async -> ProcessOutput {
        await record(Call(command: ([executable] + arguments).joined(separator: " "), environment: environment))
    }

    private func record(_ call: Call) -> ProcessOutput {
        calls.append(call)
        return results[call.command] ?? ProcessOutput(status: 0, output: "")
    }
}
