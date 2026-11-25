import AppKit
import SwiftUI

import ClubmateKit

enum KeyClaim: Int {
    case idle
    case docked
    case typed
}

extension View {
    func keyMonitor(active: Bool, overTerminals: Bool = false, fallback: Bool = false, claims: (@MainActor (NSEvent) -> KeyClaim?)? = nil, handle: @escaping @MainActor (NSEvent) -> Bool) -> some View {
        background(KeyMonitor(active: active, overTerminals: overTerminals, fallback: fallback, claims: claims, handle: handle))
    }
}

private struct KeyMonitor: NSViewRepresentable {
    let active: Bool
    let overTerminals: Bool
    let fallback: Bool
    let claims: (@MainActor (NSEvent) -> KeyClaim?)?
    let handle: @MainActor (NSEvent) -> Bool

    func makeNSView(context: Context) -> KeyMonitorView {
        KeyMonitorView()
    }

    func updateNSView(_ view: KeyMonitorView, context: Context) {
        view.handle = handle
        view.overTerminals = overTerminals
        view.fallback = fallback
        view.claims = claims
        view.active = active
    }
}

final class KeyMonitorView: NSView {
    var handle: (@MainActor (NSEvent) -> Bool)?
    var overTerminals = false
    var fallback = false
    var claims: (@MainActor (NSEvent) -> KeyClaim?)?
    var active = false {
        didSet { update() }
    }

    private var installed = false
    private var serial = 0

    private static let claimers = NSHashTable<KeyMonitorView>.weakObjects()
    private static let views = NSHashTable<KeyMonitorView>.weakObjects()
    private static var monitor: Any?
    private static var installs = 0

    static func ordered(_ views: [KeyMonitorView]) -> [KeyMonitorView] {
        views.sorted { ($0.fallback ? 1 : 0, $0.serial) < ($1.fallback ? 1 : 0, $1.serial) }
    }

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
        guard !installed else { return }
        installed = true
        Self.installs += 1
        serial = Self.installs
        Self.views.add(self)
        if Self.monitor == nil {
            Self.monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
                let consumed = MainActor.assumeIsolated { Self.ordered(Self.views.allObjects).contains { $0.deliver(event) } }
                return consumed ? nil : event
            }
        }
        if TestMode.current { TestHarness.shared.register(self) }
    }

    func deliver(_ event: NSEvent) -> Bool {
        guard accepts(event) else { return false }
        let own = rank(of: event)
        if Self.claimers.allObjects.contains(where: { $0.rank(of: event) > own && $0.accepts(event) }) { return false }
        return handle?(event) == true
    }

    private func rank(of event: NSEvent) -> Int {
        claims?(event)?.rawValue ?? -1
    }

    private func accepts(_ event: NSEvent) -> Bool {
        guard installed, let window, event.window === window, !CodeWebView.owns(window.firstResponder) else { return false }
        return overTerminals || !(window.firstResponder is TerminalSurfaceView)
    }

    private func remove() {
        Self.claimers.remove(self)
        Self.views.remove(self)
        installed = false
    }
}
