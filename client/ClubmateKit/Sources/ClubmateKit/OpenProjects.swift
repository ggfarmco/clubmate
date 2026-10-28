import Foundation

@MainActor
public final class OpenProjects {
    private let defaults: UserDefaults
    private let key: String
    private var terminated = false
    public private(set) var ids: [String]

    public init(defaults: UserDefaults = .standard, key: String = "openProjects") {
        self.defaults = defaults
        self.key = key
        ids = defaults.stringArray(forKey: key) ?? []
    }

    public func opened(_ id: String) {
        guard !ids.contains(id) else { return }
        ids.append(id)
        save()
    }

    public func closed(_ id: String) {
        guard !terminated else { return }
        forget(id)
    }

    public func forget(_ id: String) {
        guard ids.contains(id) else { return }
        ids.removeAll { $0 == id }
        save()
    }

    public func terminating() {
        terminated = true
    }

    private func save() {
        defaults.set(ids, forKey: key)
    }
}
