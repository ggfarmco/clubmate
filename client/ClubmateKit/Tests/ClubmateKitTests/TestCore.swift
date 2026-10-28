import Darwin
import Foundation

import ClubmateKit

struct TestFailure: Error, CustomStringConvertible {
    let description: String
}

private let goBinaries: Result<URL, any Error> = Result { try buildGoBinaries() }

@MainActor
final class TestCore {
    let base: URL
    let paths: ClubmatePaths
    let bin: URL
    let env: [String: String]
    private var process: Process?

    private init(base: URL, bin: URL) {
        self.base = base
        self.bin = bin
        paths = ClubmatePaths(state: base.appending(path: "state"), runtime: base.appending(path: "run"))
        env = ProcessInfo.processInfo.environment.merging([
            "CLUBMATE_STATE_DIR": paths.state.path(percentEncoded: false),
            "CLUBMATE_RUNTIME_DIR": paths.runtime.path(percentEncoded: false),
            "CLUBMATE_PLUGINS_PATH": bin.appending(path: "plugins").path(percentEncoded: false),
        ]) { $1 }
    }

    nonisolated static let commit = "0123456789abcdef0123456789abcdef01234567"

    static func prepare(bin: URL? = nil) throws -> TestCore {
        TestCore(base: try makeTempDir(), bin: try bin ?? goBinaries.get())
    }

    static func start(bin: URL? = nil) async throws -> TestCore {
        let core = try prepare(bin: bin)
        try await core.launch()
        return core
    }

    static func makeTempDir() throws -> URL {
        var template = Array("/tmp/cmkXXXXXX".utf8CString)
        let path = template.withUnsafeMutableBufferPointer { buf in
            mkdtemp(buf.baseAddress).map { String(cString: $0) }
        }
        guard let path else { throw TestFailure(description: "mkdtemp failed") }
        return URL(fileURLWithPath: path, isDirectory: true)
    }

    @discardableResult
    nonisolated static func run(_ exe: URL, _ args: [String], env: [String: String]? = nil, cwd: URL? = nil) throws -> String {
        let process = Process()
        process.executableURL = exe
        process.arguments = args
        process.currentDirectoryURL = cwd
        if let env { process.environment = env }
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe
        process.standardInput = FileHandle.nullDevice
        try process.run()
        let output = String(decoding: pipe.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
        process.waitUntilExit()
        guard process.terminationStatus == 0 else {
            throw TestFailure(description: "\(exe.lastPathComponent) \(args.joined(separator: " ")): \(output)")
        }
        return output
    }

    func cli(_ args: String...) throws {
        try Self.run(bin.appending(path: "clubmate"), args, env: env)
    }

    func stop() async {
        await killSessions()
        if let pid = await runningPID() {
            if (try? cli("core", "stop")) == nil {
                kill(pid_t(pid), SIGTERM)
                await waitGone(pid_t(pid))
            }
        }
        if let process {
            if process.isRunning { process.terminate() }
            process.waitUntilExit()
        }
        try? FileManager.default.removeItem(at: base)
    }

    func crash() throws {
        guard let process, process.isRunning else { throw TestFailure(description: "core not running") }
        kill(process.processIdentifier, SIGKILL)
        process.waitUntilExit()
        self.process = nil
    }

    func launch() async throws {
        try FileManager.default.createDirectory(at: paths.state, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let log = paths.state.appending(path: "core.log")
        FileManager.default.createFile(atPath: log.path(percentEncoded: false), contents: nil)
        let handle = try FileHandle(forWritingTo: log)
        defer { try? handle.close() }
        let process = Process()
        process.executableURL = bin.appending(path: "clubmate-core")
        process.environment = env
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = handle
        process.standardError = handle
        try process.run()
        self.process = process
        let deadline = ContinuousClock.now + .seconds(10)
        while ContinuousClock.now < deadline {
            if await runningPID() != nil { return }
            try await Task.sleep(for: .milliseconds(50))
        }
        let output = (try? String(contentsOf: log, encoding: .utf8)) ?? ""
        throw TestFailure(description: "core did not start: \(output)")
    }

    private func killSessions() async {
        guard let client = try? await CoreClient.connect(paths: paths) else { return }
        if case .array(let sessions)? = try? await client.call(method: "session.list") {
            for case .object(let session) in sessions where session["state"] == .string("running") {
                if let id = session["id"] {
                    _ = try? await client.call(method: "session.kill", params: .object(["id": id]))
                }
            }
        }
        await client.close()
    }

    private func runningPID() async -> Int? {
        guard let client = try? await CoreClient.connect(paths: paths) else { return nil }
        let ping = try? await client.ping()
        await client.close()
        return ping?.pid
    }

    private func waitGone(_ pid: pid_t) async {
        let deadline = ContinuousClock.now + .seconds(20)
        while ContinuousClock.now < deadline, kill(pid, 0) == 0 || errno != ESRCH {
            try? await Task.sleep(for: .milliseconds(50))
        }
    }
}

extension TestCore {
    nonisolated static func binaries(version: String) throws -> URL {
        let (package, root) = try goRoot()
        let bin = package.appending(path: ".build/clubmate-go/\(version)/bin", directoryHint: .isDirectory)
        let ldflags = "-X clubmate/internal/buildinfo.Version=\(version) -X clubmate/internal/buildinfo.Commit=\(commit)"
        try run(goEnv, ["go", "build", "-ldflags", ldflags, "-o", bin.path(percentEncoded: false), "./cmd/..."], cwd: root)
        return bin
    }
}

private let goEnv = URL(fileURLWithPath: "/usr/bin/env")

private func goRoot() throws -> (package: URL, root: URL) {
    let package = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    var root = package
    while !FileManager.default.fileExists(atPath: root.appending(path: "go.mod").path(percentEncoded: false)) {
        guard root.pathComponents.count > 1 else { throw TestFailure(description: "go.mod not found") }
        root = root.deletingLastPathComponent()
    }
    return (package, root)
}

private func buildGoBinaries() throws -> URL {
    let fm = FileManager.default
    let (package, root) = try goRoot()
    let bin = package.appending(path: ".build/clubmate-go/bin", directoryHint: .isDirectory)
    try TestCore.run(goEnv, ["go", "build", "-o", bin.path(percentEncoded: false), "./cmd/..."], cwd: root)
    for (id, source) in [("terminals", "plugins/terminals"), ("projects", "plugins/projects"), ("echo", "internal/testutil/plugins/echo")] {
        let dir = bin.appending(path: "plugins/\(id)", directoryHint: .isDirectory)
        try TestCore.run(goEnv, ["go", "build", "-o", dir.appending(path: id).path(percentEncoded: false), "./\(source)"], cwd: root)
        let manifest = dir.appending(path: "plugin.json")
        try? fm.removeItem(at: manifest)
        try fm.copyItem(at: root.appending(path: "\(source)/plugin.json"), to: manifest)
    }
    return bin
}
