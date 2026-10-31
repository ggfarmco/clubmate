import Foundation

public enum TestMode {
    nonisolated(unsafe) public static let defaults: UserDefaults = {
        guard current, let suite = UserDefaults(suiteName: suiteName(paths: ClubmatePaths.resolve())) else { return .standard }
        suite.removePersistentDomain(forName: suiteName(paths: ClubmatePaths.resolve()))
        return suite
    }()

    public static func suiteName(paths: ClubmatePaths) -> String {
        "com.clubmate.app.dev.test." + paths.state.deletingLastPathComponent().lastPathComponent
    }

    public static let current: Bool = {
        let env = ProcessInfo.processInfo.environment
        let defaults = ClubmatePaths.resolve(env: env.filter { $0.key != "CLUBMATE_STATE_DIR" && $0.key != "CLUBMATE_RUNTIME_DIR" })
        return enabled(env: env, paths: ClubmatePaths.resolve(env: env), defaults: defaults)
    }()

    public static func enabled(env: [String: String], paths: ClubmatePaths, defaults: ClubmatePaths) -> Bool {
        env["CLUBMATE_TEST_MODE"] == "1" && paths.flavor != "release" && paths.state != defaults.state && paths.runtime != defaults.runtime
    }
}
