import AppKit
import SwiftUI

struct WindowObserver: NSViewRepresentable {
    let onKey: @MainActor () -> Void
    let onAttach: @MainActor (NSWindow) -> Void
    let onClose: @MainActor () -> Void

    func makeNSView(context: Context) -> WindowObserverView {
        let view = WindowObserverView()
        updateNSView(view, context: context)
        return view
    }

    func updateNSView(_ view: WindowObserverView, context: Context) {
        view.onKey = onKey
        view.onAttach = onAttach
        view.onClose = onClose
        if let window = view.window { onAttach(window) }
        if view.window?.isKeyWindow == true { onKey() }
    }
}

final class WindowObserverView: NSView {
    var onKey: (@MainActor () -> Void)?
    var onAttach: (@MainActor (NSWindow) -> Void)?
    var onClose: (@MainActor () -> Void)?

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        if let window {
            NotificationCenter.default.removeObserver(self, name: NSWindow.didBecomeKeyNotification, object: window)
            NotificationCenter.default.removeObserver(self, name: NSWindow.willCloseNotification, object: window)
        }
        if let newWindow {
            NotificationCenter.default.addObserver(self, selector: #selector(windowDidBecomeKey), name: NSWindow.didBecomeKeyNotification, object: newWindow)
            NotificationCenter.default.addObserver(self, selector: #selector(windowWillClose), name: NSWindow.willCloseNotification, object: newWindow)
        }
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if let window { onAttach?(window) }
        if window?.isKeyWindow == true { onKey?() }
    }

    @objc private func windowDidBecomeKey(_ notification: Notification) {
        onKey?()
    }

    @objc private func windowWillClose(_ notification: Notification) {
        onClose?()
    }
}
