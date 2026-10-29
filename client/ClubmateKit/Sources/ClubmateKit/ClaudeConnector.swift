import Foundation

public struct ProcessOutput: Sendable, Equatable {
    public let status: Int32
    public let output: String

    public init(status: Int32, output: String) {
        self.status = status
        self.output = output
    }
}

public protocol ProcessRunning: Sendable {
    func run(_ executable: String, _ arguments: [String]) async -> ProcessOutput
}

public struct ClaudeConnector: Sendable {
    public static let serverName = "clubmate"

    private let runner: any ProcessRunning
    private let shell: String

    public init(runner: any ProcessRunning = SystemProcessRunner(), shell: String = ClaudeConnector.loginShell()) {
        self.runner = runner
        self.shell = shell
    }

    public func locate() async -> String? {
        let result = await runner.run(shell, ["-l", "-i", "-c", "command -v claude"])
        guard result.status == 0 else { return nil }
        return result.output.split(whereSeparator: \.isNewline).map(String.init).last { $0.hasPrefix("/") }
    }

    public func isConnected(claude: String) async -> Bool {
        await runner.run(claude, ["mcp", "get", Self.serverName]).status == 0
    }

    public func connect(claude: String, cli: String) async -> ProcessOutput {
        var log = ""
        if await isConnected(claude: claude) {
            let removed = await runner.run(claude, ["mcp", "remove", "--scope", "user", Self.serverName])
            log += removed.output
            guard removed.status == 0 else { return ProcessOutput(status: removed.status, output: log) }
        }
        let added = await runner.run(claude, ["mcp", "add", "--scope", "user", Self.serverName, "--", cli, "mcp"])
        return ProcessOutput(status: added.status, output: log + added.output)
    }

    public static func loginShell() -> String {
        guard let entry = getpwuid(getuid()), let shell = entry.pointee.pw_shell else { return "/bin/zsh" }
        let path = String(cString: shell)
        return path.isEmpty ? "/bin/zsh" : path
    }
}

public struct SystemProcessRunner: ProcessRunning {
    private let timeout: Duration

    public init(timeout: Duration = .seconds(15)) {
        self.timeout = timeout
    }

    public func run(_ executable: String, _ arguments: [String]) async -> ProcessOutput {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: executable)
        process.arguments = arguments
        process.standardInput = FileHandle.nullDevice
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe
        let done = AsyncStream.makeStream(of: Void.self)
        process.terminationHandler = { _ in done.continuation.finish() }
        do {
            try process.run()
        } catch {
            return ProcessOutput(status: -1, output: "\(error.localizedDescription)\n")
        }
        let reader = Task.detached { pipe.fileHandleForReading.readDataToEndOfFile() }
        let deadline = Task {
            guard (try? await Task.sleep(for: timeout)) != nil else { return }
            process.terminate()
        }
        for await _ in done.stream {}
        deadline.cancel()
        let data = await reader.value
        return ProcessOutput(status: process.terminationStatus, output: String(decoding: data, as: UTF8.self))
    }
}
