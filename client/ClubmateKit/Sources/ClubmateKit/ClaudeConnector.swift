import Darwin
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
    private let drain: Duration

    public init(timeout: Duration = .seconds(15), drain: Duration = .milliseconds(100)) {
        self.timeout = timeout
        self.drain = drain
    }

    public func run(_ executable: String, _ arguments: [String]) async -> ProcessOutput {
        var fds: [Int32] = [-1, -1]
        guard pipe(&fds) == 0 else { return Self.failure("pipe") }
        let child: SpawnedChild
        do {
            child = try SpawnedChild(executable, arguments, output: fds[1])
        } catch {
            close(fds[0])
            close(fds[1])
            return ProcessOutput(status: -1, output: "\(error)\n")
        }
        close(fds[1])
        let reader = PipeReader(fd: fds[0])
        let reading = Task.detached { await Self.onThread { reader.loop() } }
        let waiting = Task.detached { await Self.onThread { child.wait() } }
        let deadline = Task {
            guard (try? await Task.sleep(for: timeout)) != nil else { return }
            child.killGroup()
        }
        let status = await waiting.value
        deadline.cancel()
        try? await Task.sleep(for: drain)
        reader.stop()
        await reading.value
        close(fds[0])
        return ProcessOutput(status: status, output: reader.text)
    }

    private static func onThread<T: Sendable>(_ work: @escaping @Sendable () -> T) async -> T {
        await withCheckedContinuation { continuation in
            Thread.detachNewThread { continuation.resume(returning: work()) }
        }
    }

    private static func failure(_ what: String) -> ProcessOutput {
        ProcessOutput(status: -1, output: "\(what): \(String(cString: strerror(errno)))\n")
    }
}

struct SpawnError: Error, CustomStringConvertible {
    let code: Int32

    var description: String { "spawn: \(String(cString: strerror(code)))" }
}

private final class SpawnedChild: @unchecked Sendable {
    let pid: pid_t
    private let lock = NSLock()
    private var reaped = false

    init(_ executable: String, _ arguments: [String], output: Int32) throws {
        var actions: posix_spawn_file_actions_t?
        posix_spawn_file_actions_init(&actions)
        defer { posix_spawn_file_actions_destroy(&actions) }
        posix_spawn_file_actions_addopen(&actions, 0, "/dev/null", O_RDONLY, 0)
        posix_spawn_file_actions_adddup2(&actions, output, 1)
        posix_spawn_file_actions_adddup2(&actions, output, 2)
        var attr: posix_spawnattr_t?
        posix_spawnattr_init(&attr)
        defer { posix_spawnattr_destroy(&attr) }
        posix_spawnattr_setflags(&attr, Int16(POSIX_SPAWN_SETPGROUP | POSIX_SPAWN_CLOEXEC_DEFAULT))
        posix_spawnattr_setpgroup(&attr, 0)
        let argv = ([executable] + arguments).map { strdup($0) } + [nil]
        defer { argv.forEach { free($0) } }
        var pid: pid_t = 0
        let rc = posix_spawn(&pid, executable, &actions, &attr, argv, environ)
        guard rc == 0 else { throw SpawnError(code: rc) }
        self.pid = pid
    }

    func wait() -> Int32 {
        var info = siginfo_t()
        while waitid(P_PID, id_t(pid), &info, WEXITED | WNOWAIT) == -1 && errno == EINTR {}
        return lock.withLock {
            reaped = true
            var status: Int32 = 0
            while waitpid(pid, &status, 0) == -1 && errno == EINTR {}
            let signal = status & 0x7f
            return signal == 0 ? (status >> 8) & 0xff : 128 + signal
        }
    }

    func killGroup() {
        lock.withLock {
            if !reaped { kill(-pid, SIGKILL) }
        }
    }
}

private final class PipeReader: @unchecked Sendable {
    private let fd: Int32
    private let lock = NSLock()
    private var data = Data()
    private var stopped = false

    init(fd: Int32) {
        self.fd = fd
    }

    func loop() {
        var buffer = [UInt8](repeating: 0, count: 64 * 1024)
        while !lock.withLock({ stopped }) {
            var pfd = pollfd(fd: fd, events: Int16(POLLIN), revents: 0)
            let ready = poll(&pfd, 1, 50)
            if ready < 0, errno == EINTR { continue }
            if ready < 0 { return }
            if ready == 0 { continue }
            let n = read(fd, &buffer, buffer.count)
            if n < 0, errno == EINTR || errno == EAGAIN { continue }
            if n <= 0 { return }
            lock.withLock { data.append(contentsOf: buffer[0..<n]) }
        }
    }

    func stop() {
        lock.withLock { stopped = true }
    }

    var text: String {
        lock.withLock { String(decoding: data, as: UTF8.self) }
    }
}
