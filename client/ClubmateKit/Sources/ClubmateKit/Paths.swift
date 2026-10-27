import Darwin
import Foundation

public struct ClubmatePaths: Sendable, Equatable {
    public let state: URL
    public let runtime: URL

    public init(state: URL, runtime: URL) {
        self.state = state
        self.runtime = runtime
    }

    public static func resolve(env: [String: String] = ProcessInfo.processInfo.environment) -> ClubmatePaths {
        let state = env["CLUBMATE_STATE_DIR"].flatMap(nonEmpty).map { URL(filePath: $0) }
            ?? URL(filePath: env["HOME"].flatMap(nonEmpty) ?? NSHomeDirectory()).appending(path: "Library/Application Support/clubmate")
        let runtime = URL(filePath: env["CLUBMATE_RUNTIME_DIR"].flatMap(nonEmpty) ?? "/tmp/clubmate-\(getuid())")
        return ClubmatePaths(state: state, runtime: runtime)
    }

    public var coreSocket: URL {
        runtime.appending(path: "core.sock")
    }

    public var sessionsSocketDir: URL {
        runtime.appending(path: "s")
    }

    public func sessionSocket(_ id: String) -> URL {
        sessionsSocketDir.appending(path: id + ".sock")
    }

    private static func nonEmpty(_ s: String) -> String? {
        s.isEmpty ? nil : s
    }
}
