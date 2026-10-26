import AppKit
import SwiftUI

import ClubmateKit

@MainActor
final class SurfaceDeck {
    let container = SurfaceDeckView()

    private var views: [String: TerminalSurfaceView] = [:]
    private var evictions: [String: Task<Void, Never>] = [:]
    private var selected: String?
    private let grace: Duration

    init(grace: Duration = .seconds(30)) {
        self.grace = grace
        container.onWindowChange = { [weak self] in self?.apply(focus: true) }
    }

    func view(for sessionID: String) -> TerminalSurfaceView {
        if let view = views[sessionID] { return view }
        let view = TerminalSurfaceView(sessionID: sessionID)
        view.onClose = { [weak self] in self?.surfaceClosed(sessionID) }
        view.frame = container.bounds
        view.autoresizingMask = [.width, .height]
        view.setVisible(false)
        container.addSubview(view)
        views[sessionID] = view
        return view
    }

    func select(_ sessionID: String) {
        guard selected != sessionID || views[sessionID] == nil else { return }
        selected = sessionID
        _ = view(for: sessionID)
        apply(focus: true)
    }

    func sync(referenced: Set<String>) {
        for id in referenced {
            evictions.removeValue(forKey: id)?.cancel()
        }
        for id in views.keys where !referenced.contains(id) && evictions[id] == nil {
            evictions[id] = Task { [weak self, grace] in
                try? await Task.sleep(for: grace)
                guard !Task.isCancelled else { return }
                self?.remove(id)
            }
        }
    }

    func surfaceClosed(_ sessionID: String) {
        remove(sessionID)
    }

    func closeAll() {
        views.keys.forEach(remove)
    }

    static func sessionIDs(in slots: [String: SlotView]) -> Set<String> {
        var ids = Set<String>()
        var stack = slots.values.map(\.tree)
        while let node = stack.popLast() {
            switch node.type {
            case "surface": ids.formUnion([node.string("sessionId")].compactMap { $0 })
            case "list": ids.formUnion(node.items().map(\.id))
            case "tree": ids.formUnion(flatten(node.treeItems()))
            default: break
            }
            stack.append(contentsOf: node.children)
        }
        return ids
    }

    private static func flatten(_ items: [SDUITreeItem]) -> [String] {
        items.flatMap { [$0.id] + flatten($0.children) }
    }

    private func apply(focus: Bool) {
        let onScreen = container.window != nil
        for (id, view) in views {
            view.setVisible(onScreen && id == selected)
        }
        if focus, onScreen, let selected { views[selected]?.focus() }
    }

    private func remove(_ sessionID: String) {
        evictions.removeValue(forKey: sessionID)?.cancel()
        guard let view = views.removeValue(forKey: sessionID) else { return }
        view.destroy()
        view.removeFromSuperview()
        if selected == sessionID { selected = nil }
    }
}

final class SurfaceDeckView: NSView {
    var onWindowChange: (() -> Void)?

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        onWindowChange?()
    }
}

private struct SurfaceDeckKey: EnvironmentKey {
    static let defaultValue: SurfaceDeck? = nil
}

extension EnvironmentValues {
    var surfaceDeck: SurfaceDeck? {
        get { self[SurfaceDeckKey.self] }
        set { self[SurfaceDeckKey.self] = newValue }
    }
}
