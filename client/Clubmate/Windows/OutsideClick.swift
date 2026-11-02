import AppKit
import SwiftUI

extension View {
    func onClickOutside(_ action: @escaping @MainActor () -> Void) -> some View {
        background(OutsideClickMonitor(action: action))
    }
}

private struct OutsideClickMonitor: NSViewRepresentable {
    let action: @MainActor () -> Void

    func makeNSView(context: Context) -> OutsideClickView {
        OutsideClickView()
    }

    func updateNSView(_ view: OutsideClickView, context: Context) {
        view.action = action
    }
}

final class OutsideClickView: NSView {
    var action: (@MainActor () -> Void)?

    private var monitor: Any?

    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard window != nil else { return remove() }
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]) { [weak self] event in
            MainActor.assumeIsolated { self?.check(event) }
            return event
        }
    }

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        if newWindow == nil { remove() }
    }

    private func check(_ event: NSEvent) {
        guard let window, let action else { return }
        if event.window === window, bounds.contains(convert(event.locationInWindow, from: nil)) { return }
        Task { @MainActor in action() }
    }

    private func remove() {
        guard let monitor else { return }
        NSEvent.removeMonitor(monitor)
        self.monitor = nil
    }
}
