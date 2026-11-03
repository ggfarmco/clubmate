import AppKit
import SwiftUI

struct FullScreenTitlebar: NSViewRepresentable {
    let color: NSColor

    func makeNSView(context: Context) -> FullScreenTitlebarView {
        FullScreenTitlebarView()
    }

    func updateNSView(_ view: FullScreenTitlebarView, context: Context) {
        view.color = color
    }
}

final class FullScreenTitlebarView: NSView {
    private static let events = [NSWindow.willEnterFullScreenNotification, NSWindow.didEnterFullScreenNotification]

    var color: NSColor? {
        didSet { if color != oldValue { paint() } }
    }

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        for name in Self.events {
            if let window { NotificationCenter.default.removeObserver(self, name: name, object: window) }
            if let newWindow { NotificationCenter.default.addObserver(self, selector: #selector(fullScreenChanged), name: name, object: newWindow) }
        }
    }

    @objc private func fullScreenChanged(_ notification: Notification) {
        paint()
        DispatchQueue.main.async { [weak self] in self?.paint() }
    }

    private func paint() {
        guard let window, let color else { return }
        let toolbar = NSApp.windows.first { $0.className == "NSToolbarFullScreenWindow" && $0.parent == window }
        guard let root = toolbar?.contentView?.superview ?? toolbar?.contentView,
              let container = root.firstDescendant(named: "NSTitlebarContainerView") else { return }
        if let titlebar = container.firstDescendant(named: "NSTitlebarView") {
            titlebar.wantsLayer = true
            titlebar.layer?.backgroundColor = color.cgColor
        }
        container.firstDescendant(named: "NSTitlebarBackgroundView")?.isHidden = true
    }
}

private extension NSView {
    func firstDescendant(named name: String) -> NSView? {
        for view in subviews {
            if view.className == name { return view }
            if let found = view.firstDescendant(named: name) { return found }
        }
        return nil
    }
}
