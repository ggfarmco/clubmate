import AppKit
import os
import SwiftUI

import ClubmateKit

private let titleBarLog = Logger(subsystem: Diagnostics.logSubsystem, category: "titlebar")

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
    private var updates: NSObjectProtocol?

    private static let events = [NSWindow.willEnterFullScreenNotification, NSWindow.didEnterFullScreenNotification, NSWindow.didExitFullScreenNotification, NSWindow.willExitFullScreenNotification, NSWindow.didBecomeKeyNotification, NSWindow.didEndLiveResizeNotification, NSWindow.didChangeScreenNotification]

    var changed: ((TitleBarChrome) -> Void)? {
        didSet { if oldValue == nil { measure() } }
    }

    isolated deinit {
        if let updates { NotificationCenter.default.removeObserver(updates) }
    }

    override func viewDidChangeEffectiveAppearance() {
        super.viewDidChangeEffectiveAppearance()
        measure()
    }

    override var mouseDownCanMoveWindow: Bool { true }

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        if newWindow == nil { follow(fullScreen: false) }
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

    private func follow(fullScreen: Bool) {
        if fullScreen, updates == nil {
            updates = NotificationCenter.default.addObserver(forName: NSApplication.didUpdateNotification, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated {
                    if let window = self?.window { TitleBarBackground.clear(window) }
                }
            }
        } else if !fullScreen, let updates {
            NotificationCenter.default.removeObserver(updates)
            self.updates = nil
        }
    }

    private func measure() {
        guard let window, let changed else { return }
        TitleBarBackground.clear(window)
        var chrome = TitleBarChrome.fallback
        follow(fullScreen: window.styleMask.contains(.fullScreen))
        if !window.styleMask.contains(.fullScreen), let close = window.standardWindowButton(.closeButton), let zoom = window.standardWindowButton(.zoomButton), close.window == window {
            let center = window.frame.height - close.convert(close.bounds, to: nil).midY
            if center > 0, center <= TitleBarChrome.fallback.height { chrome.height = (center * 2).rounded() }
            chrome.lights = zoom.convert(zoom.bounds, to: nil).maxX
        }
        DispatchQueue.main.async { changed(chrome) }
    }
}

enum TitleBarBackground {
    static func clear(_ window: NSWindow) {
        if !window.titlebarAppearsTransparent { window.titlebarAppearsTransparent = true }
        guard let close = window.standardWindowButton(.closeButton), let host = close.window else { return }
        if host !== window {
            if host.isOpaque { host.isOpaque = false }
            if host.backgroundColor != .clear { host.backgroundColor = .clear }
        }
        for view in views(holding: close) where !view.isHidden {
            view.isHidden = true
            titleBarLog.info("hid title bar view \(String(describing: type(of: view)), privacy: .public)")
        }
        var view = close.superview
        while let current = view, current !== host.contentView?.superview {
            if current.layer?.backgroundColor != nil { current.layer?.backgroundColor = nil }
            view = current.superview
        }
    }

    static func views(in window: NSWindow) -> [NSView] {
        guard let close = window.standardWindowButton(.closeButton), close.window === window else { return [] }
        return views(holding: close)
    }

    private static func views(holding close: NSButton) -> [NSView] {
        guard let frame = close.window?.contentView?.superview, var child = close.superview else { return [] }
        var out: [NSView] = []
        while let parent = child.superview, parent !== frame {
            out += parent.subviews.filter { $0 !== child }
            child = parent
        }
        return out
    }
}
