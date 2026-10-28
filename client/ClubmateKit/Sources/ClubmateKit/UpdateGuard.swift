public enum UpdateGuard {
    public static func needsConfirmation(itemProtocol: Int?, coreProtocol: Int?, liveSessions: Int?) -> Bool {
        guard let itemProtocol, liveSessions != 0 else { return false }
        return itemProtocol != coreProtocol
    }
}
