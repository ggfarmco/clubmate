public enum UpdateGuard {
    public static func needsConfirmation(itemProtocol: Int?, coreProtocol: Int, liveSessions: Int) -> Bool {
        guard let itemProtocol else { return false }
        return itemProtocol != coreProtocol && liveSessions > 0
    }
}
