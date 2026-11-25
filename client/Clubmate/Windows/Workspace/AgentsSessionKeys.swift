import AppKit
import SwiftUI

import ClubmateKit

struct RowNumbers: Equatable {
    var shown = false
    var lands = 0
}

extension EnvironmentValues {
    @Entry var rowNumbers = RowNumbers()
}

extension View {
    func agentsSessionKeys(active: Bool, last: @escaping @MainActor () -> Void) -> some View {
        modifier(AgentsSessionKeys(active: active, last: last))
    }
}

private struct AgentsSessionKeys: ViewModifier {
    let active: Bool
    let last: @MainActor () -> Void

    @State private var numbers = RowNumbers()

    func body(content: Content) -> some View {
        content
            .environment(\.rowNumbers, RowNumbers(shown: active && numbers.shown, lands: numbers.lands))
            .keyMonitor(active: active) { event in
                let flags = event.modifierFlags
                guard SessionKeys.isLast(keyCode: event.keyCode, control: flags.contains(.control), command: flags.contains(.command), option: flags.contains(.option), shift: flags.contains(.shift)) else { return false }
                last()
                return true
            }
            .background(ControlHold(active: active) { numbers.shown = $0 } land: { numbers.lands += 1 })
    }
}

private struct ControlHold: NSViewRepresentable {
    let active: Bool
    let changed: @MainActor (Bool) -> Void
    let land: @MainActor () -> Void

    func makeNSView(context: Context) -> ControlHoldView {
        ControlHoldView()
    }

    func updateNSView(_ view: ControlHoldView, context: Context) {
        view.changed = changed
        view.land = land
        view.active = active
    }
}

final class ControlHoldView: NSView {
    var changed: (@MainActor (Bool) -> Void)?
    var land: (@MainActor () -> Void)?
    var active = false {
        didSet { update() }
    }

    private var monitor: Any?
    private var timer: Timer?
    private var shown = false
    private var keys = ControlHoldKeys()

    static let delay: TimeInterval = 0.5

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
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.flagsChanged, .keyDown]) { [weak self] event in
            MainActor.assumeIsolated { self?.observe(event) }
            return event
        }
    }

    private func observe(_ event: NSEvent) {
        guard event.window === window, window?.isKeyWindow == true else {
            keys = ControlHoldKeys()
            return cancel()
        }
        guard event.type == .flagsChanged else { return cancel() }
        let flags = event.modifierFlags
        for effect in keys.flags(control: flags.contains(.control), command: flags.contains(.command), option: flags.contains(.option), shift: flags.contains(.shift)) {
            switch effect {
            case .land: land?()
            case .start: start()
            case .cancel: cancel()
            }
        }
    }

    private func start() {
        guard timer == nil, !shown else { return }
        timer = Timer.scheduledTimer(withTimeInterval: Self.delay, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated { self?.show() }
        }
    }

    private func show() {
        timer = nil
        shown = true
        changed?(true)
    }

    private func cancel() {
        timer?.invalidate()
        timer = nil
        guard shown else { return }
        shown = false
        changed?(false)
    }

    private func remove() {
        cancel()
        keys = ControlHoldKeys()
        guard let monitor else { return }
        NSEvent.removeMonitor(monitor)
        self.monitor = nil
    }
}
