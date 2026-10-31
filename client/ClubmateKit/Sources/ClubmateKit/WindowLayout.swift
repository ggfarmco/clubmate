import Foundation
import Observation

public struct LayoutWindow: Codable, Equatable, Sendable {
    public var id: String
    public var workspaces: [String]
    public var active: String?
    public var chatShown: Bool?
    public var chatWidth: Double?

    public init(id: String = UUID().uuidString, workspaces: [String] = [], active: String? = nil) {
        self.id = id
        self.workspaces = workspaces
        self.active = active
    }
}

@MainActor
@Observable
public final class WindowLayout {
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let key: String
    @ObservationIgnored private var isTerminating = false
    public private(set) var windows: [LayoutWindow]

    public init(defaults: UserDefaults = .standard, key: String = "windowLayout") {
        self.defaults = defaults
        self.key = key
        if let data = defaults.data(forKey: key), let saved = try? JSONDecoder().decode([LayoutWindow].self, from: data) {
            windows = saved
        } else if let legacy = defaults.stringArray(forKey: "openProjects") {
            windows = legacy.isEmpty ? [] : [LayoutWindow(workspaces: legacy, active: legacy.last)]
            defaults.removeObject(forKey: "openProjects")
            save()
        } else {
            windows = []
        }
    }

    public func window(_ id: String) -> LayoutWindow? {
        windows.first { $0.id == id }
    }

    public var workspaces: Set<String> {
        Set(windows.flatMap(\.workspaces))
    }

    public func activeWorkspace(preferring window: String?) -> String? {
        window.flatMap { self.window($0)?.active } ?? windows.lazy.compactMap(\.active).first
    }

    public func claim(excluding shown: Set<String>) -> String {
        windows.first { !shown.contains($0.id) }?.id ?? newWindow()
    }

    @discardableResult
    public func newWindow() -> String {
        let window = LayoutWindow()
        windows.append(window)
        save()
        return window.id
    }

    @discardableResult
    public func open(_ ws: String, preferring window: String?) -> String {
        if let index = windows.firstIndex(where: { $0.workspaces.contains(ws) }) {
            windows[index].active = ws
            save()
            return windows[index].id
        }
        let index = window.flatMap { id in windows.firstIndex { $0.id == id } } ?? appendWindow()
        windows[index].workspaces.append(ws)
        windows[index].active = ws
        save()
        return windows[index].id
    }

    public func activate(_ ws: String, in window: String) {
        guard let index = windows.firstIndex(where: { $0.id == window }),
              windows[index].workspaces.contains(ws) else { return }
        windows[index].active = ws
        save()
    }

    public func next(in window: String) {
        step(in: window, by: 1)
    }

    public func previous(in window: String) {
        step(in: window, by: -1)
    }

    public func close(_ ws: String, in window: String) {
        guard let index = windows.firstIndex(where: { $0.id == window }) else { return }
        detach(ws, from: index)
        save()
    }

    public func remove(_ ws: String) {
        for index in windows.indices {
            detach(ws, from: index)
        }
        save()
    }

    @discardableResult
    public func move(_ ws: String, toNewWindow: Bool) -> String {
        for index in windows.indices {
            detach(ws, from: index)
        }
        let window = LayoutWindow(workspaces: [ws], active: ws)
        windows.append(window)
        save()
        return window.id
    }

    public func toggleChat(in window: String) {
        guard let index = windows.firstIndex(where: { $0.id == window }) else { return }
        windows[index].chatShown = windows[index].chatShown == true ? nil : true
        save()
    }

    public func showChat(in window: String) {
        guard let index = windows.firstIndex(where: { $0.id == window }), windows[index].chatShown != true else { return }
        windows[index].chatShown = true
        save()
    }

    public func setChatWidth(_ width: Double, in window: String) {
        guard let index = windows.firstIndex(where: { $0.id == window }) else { return }
        windows[index].chatWidth = width
        save()
    }

    public func closeWindow(_ id: String) {
        windows.removeAll { $0.id == id }
        save()
    }

    public func forget(missing: Set<String>) {
        guard !missing.isEmpty else { return }
        for index in windows.indices {
            for ws in windows[index].workspaces where missing.contains(ws) {
                detach(ws, from: index)
            }
        }
        save()
    }

    public func terminating() {
        isTerminating = true
    }

    private func appendWindow() -> Int {
        windows.append(LayoutWindow())
        return windows.count - 1
    }

    private func step(in window: String, by delta: Int) {
        guard let index = windows.firstIndex(where: { $0.id == window }) else { return }
        let list = windows[index].workspaces
        guard !list.isEmpty else { return }
        let current = windows[index].active.flatMap { list.firstIndex(of: $0) } ?? (delta > 0 ? -1 : 0)
        windows[index].active = list[((current + delta) % list.count + list.count) % list.count]
        save()
    }

    private func detach(_ ws: String, from index: Int) {
        guard let position = windows[index].workspaces.firstIndex(of: ws) else { return }
        windows[index].workspaces.remove(at: position)
        guard windows[index].active == ws else { return }
        let rest = windows[index].workspaces
        windows[index].active = position < rest.count ? rest[position] : rest.last
    }

    private func save() {
        guard !isTerminating, let data = try? JSONEncoder().encode(windows) else { return }
        defaults.set(data, forKey: key)
    }
}

@MainActor
public enum SurfaceDeckPolicy {
    public static func shouldCloseDeck(workspace: String, layout: WindowLayout) -> Bool {
        !layout.windows.contains { $0.workspaces.contains(workspace) }
    }
}
