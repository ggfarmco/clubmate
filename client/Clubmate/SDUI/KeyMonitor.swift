import AppKit
import SwiftUI

import ClubmateKit

extension View {
    func keyMonitor(active: Bool, overTerminals: Bool = false, claims: (@MainActor (NSEvent) -> Bool)? = nil, handle: @escaping @MainActor (NSEvent) -> Bool) -> some View {
        background(KeyMonitor(active: active, overTerminals: overTerminals, claims: claims, handle: handle))
    }
}

private struct KeyMonitor: NSViewRepresentable {
    let active: Bool
    let overTerminals: Bool
    let claims: (@MainActor (NSEvent) -> Bool)?
    let handle: @MainActor (NSEvent) -> Bool

    func makeNSView(context: Context) -> KeyMonitorView {
        KeyMonitorView()
    }

    func updateNSView(_ view: KeyMonitorView, context: Context) {
        view.handle = handle
        view.overTerminals = overTerminals
        view.claims = claims
        view.active = active
    }
}

final class KeyMonitorView: NSView {
    var handle: (@MainActor (NSEvent) -> Bool)?
    var overTerminals = false
    var claims: (@MainActor (NSEvent) -> Bool)?
    var active = false {
        didSet { update() }
    }

    private var monitor: Any?

    private static let claimers = NSHashTable<KeyMonitorView>.weakObjects()

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
        if claims != nil { Self.claimers.add(self) }
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            let consumed = MainActor.assumeIsolated { self?.deliver(event) == true }
            return consumed ? nil : event
        }
        if TestMode.current { TestHarness.shared.register(self) }
    }

    func deliver(_ event: NSEvent) -> Bool {
        guard accepts(event) else { return false }
        if claims == nil, Self.claimers.allObjects.contains(where: { $0.claims?(event) == true && $0.accepts(event) }) { return false }
        return handle?(event) == true
    }

    private func accepts(_ event: NSEvent) -> Bool {
        guard monitor != nil, let window, event.window === window, !CodeWebView.owns(window.firstResponder) else { return false }
        return overTerminals || !(window.firstResponder is TerminalSurfaceView)
    }

    private func remove() {
        Self.claimers.remove(self)
        guard let monitor else { return }
        NSEvent.removeMonitor(monitor)
        self.monitor = nil
    }
}
