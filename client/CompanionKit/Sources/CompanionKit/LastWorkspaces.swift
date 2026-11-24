import Foundation

public struct LastWorkspaces: Equatable, Sendable {
    public static let defaultsKey = "lastWorkspaces"

    private var ids: [String: String]

    public init(_ ids: [String: String] = [:]) {
        self.ids = ids
    }

    public subscript(computer: String) -> String? {
        get { ids[computer] }
        set { ids[computer] = newValue }
    }

    public func workspace(for computer: String, in list: [Workspace]) -> Workspace? {
        list.first { $0.id == ids[computer] } ?? Workspace.homeFirst(list).first
    }

    public mutating func forget(computer: String) {
        ids[computer] = nil
    }

    public func save(to defaults: UserDefaults, key: String = defaultsKey) {
        defaults.set(ids, forKey: key)
    }

    public static func restore(from defaults: UserDefaults, key: String = defaultsKey) -> LastWorkspaces {
        LastWorkspaces(defaults.dictionary(forKey: key) as? [String: String] ?? [:])
    }
}
