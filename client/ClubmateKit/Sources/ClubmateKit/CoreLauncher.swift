import Foundation

public struct CoreLauncher: Sendable {
    public let cliPath: URL
    public let bundledVersion: String

    public init(cliPath: URL, bundledVersion: String = "") {
        self.cliPath = cliPath
        self.bundledVersion = bundledVersion
    }

    public func ensureRunning(paths: ClubmatePaths, wait: Duration = .seconds(15)) async throws {
        do {
            let running = try await ping(paths)
            if let flavor = running.flavor, !flavor.isEmpty, !paths.flavor.isEmpty, flavor != paths.flavor {
                throw ClubmateError.coreStartFailed("a \(flavor) Clubmate core is running on \(paths.runtime.fileSystemPath); refusing to replace it")
            }
            if bundledVersion.isEmpty || running.version == bundledVersion { return }
            try await cli(["core", "stop"], paths: paths)
        } catch ClubmateError.notRunning {}
        try await cli(["core", "start", "--wait", "\(wait.components.seconds * 1000 + wait.components.attoseconds / 1_000_000_000_000_000)ms"], paths: paths)
        _ = try await ping(paths)
    }

    private func ping(_ paths: ClubmatePaths) async throws -> PingResult {
        let client = try await CoreClient.connect(paths: paths)
        do {
            let ping = try await client.ping()
            await client.close()
            return ping
        } catch {
            await client.close()
            throw error
        }
    }

    private func cli(_ arguments: [String], paths: ClubmatePaths) async throws {
        let process = Process()
        process.executableURL = cliPath
        process.arguments = arguments
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
