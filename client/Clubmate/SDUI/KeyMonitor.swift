import AppKit
import SwiftUI

import ClubmateKit

extension View {
    func keyMonitor(active: Bool, handle: @escaping @MainActor (NSEvent) -> Bool) -> some View {
        background(KeyMonitor(active: active, handle: handle))
    }
}

private struct KeyMonitor: NSViewRepresentable {
    let active: Bool
    let handle: @MainActor (NSEvent) -> Bool

    func makeNSView(context: Context) -> KeyMonitorView {
        KeyMonitorView()
    }

    func updateNSView(_ view: KeyMonitorView, context: Context) {
        view.handle = handle
        view.active = active
    }
}

final class KeyMonitorView: NSView {
    var handle: (@MainActor (NSEvent) -> Bool)?
    var active = false {
        didSet { update() }
    }

    private var monitor: Any?

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        if newWindow == nil { remove() }
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        update()
    }

    private func update() {
        guard active, window != nil else { return remove() }
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            let consumed = MainActor.assumeIsolated { self?.deliver(event) == true }
            return consumed ? nil : event
        }
        if TestMode.current { TestHarness.shared.register(self) }
    }

    func deliver(_ event: NSEvent) -> Bool {
        guard monitor != nil, let window, event.window === window, !(window.firstResponder is TerminalSurfaceView) else { return false }
        return handle?(event) == true
    }

    private func remove() {
        guard let monitor else { return }
        NSEvent.removeMonitor(monitor)
        self.monitor = nil
    }
}
