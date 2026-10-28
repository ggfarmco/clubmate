public enum BuildInfo {
    public static func display(version: String, commit: String) -> String {
        commit.isEmpty ? version : "\(version) (\(commit.prefix(7)))"
    }
}
