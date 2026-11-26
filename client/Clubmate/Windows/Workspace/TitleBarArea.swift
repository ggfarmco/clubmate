import AppKit
import SwiftUI

struct TitleBarChrome: Equatable {
    static let fallback = TitleBarChrome(height: 32, lights: 69)

    var height: CGFloat
    var lights: CGFloat
}

struct TitleBarArea: NSViewRepresentable {
    let changed: (TitleBarChrome) -> Void

    func makeNSView(context: Context) -> TitleBarAreaView {
        TitleBarAreaView()
    }

    func updateNSView(_ view: TitleBarAreaView, context: Context) {
        view.changed = changed
    }
}

final class TitleBarAreaView: NSView {
    private static let events = [NSWindow.didEnterFullScreenNotification, NSWindow.didExitFullScreenNotification, NSWindow.willExitFullScreenNotification]

    var changed: ((TitleBarChrome) -> Void)? {
        didSet { measure() }
    }

    override var mouseDownCanMoveWindow: Bool { true }

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        for name in Self.events {
            if let window { NotificationCenter.default.removeObserver(self, name: name, object: window) }
            if let newWindow { NotificationCenter.default.addObserver(self, selector: #selector(fullScreenChanged), name: name, object: newWindow) }
        }
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        measure()
    }

    @objc private func fullScreenChanged(_ notification: Notification) {
        measure()
    }

    override func mouseDown(with event: NSEvent) {
        guard let window else { return }
        if event.clickCount == 2 {
            switch UserDefaults.standard.string(forKey: "AppleActionOnDoubleClick") {
            case "Minimize": window.performMiniaturize(nil)
            case "None": break
            default: window.performZoom(nil)
            }
            return
        }
        window.performDrag(with: event)
    }

    private func measure() {
        guard let window, let changed else { return }
        var chrome = TitleBarChrome.fallback
        if window.styleMask.contains(.fullScreen) {
            chrome.lights = 0
        } else if let close = window.standardWindowButton(.closeButton), let zoom = window.standardWindowButton(.zoomButton), close.window == window {
            let center = window.frame.height - close.convert(close.bounds, to: nil).midY
            if center > 0, center <= TitleBarChrome.fallback.height { chrome.height = (center * 2).rounded() }
            chrome.lights = zoom.convert(zoom.bounds, to: nil).maxX
        }
        DispatchQueue.main.async { changed(chrome) }
    }
}
