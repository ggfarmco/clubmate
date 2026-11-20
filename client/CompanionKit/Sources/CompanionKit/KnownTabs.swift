import Foundation

public struct KnownTabs: Equatable, Sendable {
    public static let defaultsKey = "knownTabs"

    private var sets: [WorkspaceKey: [CompanionTab]] = [:]
    private var last: [CompanionTab]?

    public init() {}

    public func tabs(for key: WorkspaceKey?, live: [CompanionTab]?) -> [CompanionTab]? {
        live ?? key.flatMap { sets[$0] } ?? last
    }

    public mutating func remember(_ tabs: [CompanionTab], for key: WorkspaceKey) {
        sets[key] = tabs
        last = tabs
    }

    public func save(to defaults: UserDefaults, key: String = defaultsKey) {
        guard let data = try? JSONEncoder().encode(Saved(sets: sets.map { Saved.Entry(workspace: $0.key, tabs: $0.value) }, last: last)) else { return }
        defaults.set(data, forKey: key)
    }

    public static func restore(from defaults: UserDefaults, key: String = defaultsKey) -> KnownTabs {
        var known = KnownTabs()
        guard let data = defaults.data(forKey: key), let saved = try? JSONDecoder().decode(Saved.self, from: data) else { return known }
        known.sets = Dictionary(saved.sets.map { ($0.workspace, $0.tabs) }, uniquingKeysWith: { $1 })
        known.last = saved.last
        return known
    }

    private struct Saved: Codable {
        struct Entry: Codable {
            let workspace: WorkspaceKey
            let tabs: [CompanionTab]
        }

        let sets: [Entry]
        let last: [CompanionTab]?
    }
}
