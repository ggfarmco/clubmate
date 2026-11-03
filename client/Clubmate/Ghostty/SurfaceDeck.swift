import AppKit
import Observation
import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class SurfaceDeck {
    private(set) var owner: UUID?

    @ObservationIgnored weak var panes: PaneFocus?
    @ObservationIgnored let container = SurfaceDeckView()
    @ObservationIgnored private var views: [String: TerminalSurfaceView] = [:]
    @ObservationIgnored private var selected: String?
    @ObservationIgnored private var slots: [String: String] = [:]
    @ObservationIgnored private var closed = false

    init() {
        container.onWindowChange = { [weak self] in self?.apply() }
    }

    func claim(_ host: UUID) {
        guard owner == nil else { return }
        owner = host
    }

    func release(_ host: UUID) {
        guard owner == host else { return }
        owner = nil
    }

    func select(_ sessionID: String, slot: String) {
        slots[sessionID] = slot
        guard !closed, selected != sessionID || views[sessionID] == nil else { return }
        selected = sessionID
        if views[sessionID] == nil { mount(sessionID) }
        apply()
    }

    func focus(_ sessionID: String) {
        guard selected == sessionID, container.window != nil else { return }
        views[sessionID]?.focus()
    }

    func surfaceClosed(_ sessionID: String) {
        slots[sessionID] = nil
        guard let view = views.removeValue(forKey: sessionID) else { return }
        view.destroy()
        view.removeFromSuperview()
        if selected == sessionID { selected = nil }
    }

    func closeAll() {
        closed = true
        guard !views.isEmpty else { return }
        ghosttyLog.notice("freeing \(self.views.count, privacy: .public) surfaces")
        views.keys.forEach(surfaceClosed)
    }

    private func mount(_ sessionID: String) {
        let view = TerminalSurfaceView(sessionID: sessionID)
        view.onClose = { [weak self] in self?.surfaceClosed(sessionID) }
        view.onFocus = { [weak self] focused in self?.panes?.report(.terminal, from: "terminal:" + sessionID, slot: self?.slots[sessionID], focused: focused) }
        view.frame = container.bounds
        view.autoresizingMask = [.width, .height]
        view.setVisible(false)
        container.addSubview(view)
        views[sessionID] = view
    }

    private func apply() {
        let onScreen = container.window != nil
        for (id, view) in views {
            view.setVisible(onScreen && id == selected)
        }
        if onScreen, let selected { views[selected]?.focus() }
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
