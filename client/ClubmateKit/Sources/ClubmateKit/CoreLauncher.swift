import Foundation

public struct CoreLauncher: Sendable {
    public let cliPath: URL

    public init(cliPath: URL) {
        self.cliPath = cliPath
    }

    public func ensureRunning(paths: ClubmatePaths, wait: Duration = .seconds(5)) async throws {
        do {
            try await probe(paths)
            return
        } catch ClubmateError.notRunning {
            try await start(paths: paths, wait: wait)
        }
        try await probe(paths)
    }

    private func probe(_ paths: ClubmatePaths) async throws {
        let connection = try await RPCConnection.connect(paths: paths)
        await connection.close()
    }

    private func start(paths: ClubmatePaths, wait: Duration) async throws {
        let process = Process()
        process.executableURL = cliPath
        process.arguments = ["core", "start", "--wait", "\(wait.components.seconds * 1000 + wait.components.attoseconds / 1_000_000_000_000_000)ms"]
        process.environment = ProcessInfo.processInfo.environment.merging([
            "CLUBMATE_STATE_DIR": paths.state.fileSystemPath,
            "CLUBMATE_RUNTIME_DIR": paths.runtime.fileSystemPath,
        ]) { $1 }
        let stderr = Pipe()
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = FileHandle.nullDevice
        process.standardError = stderr
        let status: Int32 = try await withCheckedThrowingContinuation { continuation in
            process.terminationHandler = { continuation.resume(returning: $0.terminationStatus) }
            do {
                try process.run()
            } catch {
                process.terminationHandler = nil
                continuation.resume(throwing: ClubmateError.coreStartFailed("\(error)"))
            }
        }
        guard status == 0 else {
            let message = String(decoding: stderr.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
            throw ClubmateError.coreStartFailed(message.trimmingCharacters(in: .whitespacesAndNewlines))
        }
    }
}
