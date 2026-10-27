import AppKit
import Observation
import SwiftUI

@MainActor
@Observable
final class SurfaceDeck {
    private(set) var owner: UUID?

    @ObservationIgnored let container = SurfaceDeckView()
    @ObservationIgnored private var views: [String: TerminalSurfaceView] = [:]
    @ObservationIgnored private var selected: String?
    @ObservationIgnored private var closed = false

    init() {
        container.onWindowChange = { [weak self] in self?.apply() }
        container.onWindowClose = { [weak self] in self?.closeAll() }
    }

    func claim(_ host: UUID) {
        guard owner == nil else { return }
        owner = host
    }

    func release(_ host: UUID) {
        guard owner == host else { return }
        owner = nil
    }

    func select(_ sessionID: String) {
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
    var onWindowClose: (() -> Void)?

    private var closeObserver: NSObjectProtocol?
    private weak var observedWindow: NSWindow?

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        defer { onWindowChange?() }
        guard let window, window !== observedWindow else { return }
        closeObserver.map(NotificationCenter.default.removeObserver)
        observedWindow = window
        closeObserver = NotificationCenter.default.addObserver(
            forName: NSWindow.willCloseNotification, object: window, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.onWindowClose?() }
        }
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
