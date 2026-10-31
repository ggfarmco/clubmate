import AppKit
import Foundation
import Observation
import SwiftUI
import WebKit

import ClubmateKit

@MainActor
final class BrowserWindowHost: NSObject, BrowserHost, NSWindowDelegate {
    static let identifier = NSUserInterfaceItemIdentifier("clubmate.agent-browser")
    static let size = NSSize(width: 1280, height: 900)
    private static let offscreen = NSPoint(x: -5000, y: -5000)

    private let window: NSWindow
    private let shelf: NSView
    private let bar = BrowserHandoffBarModel()
    private let slot = BrowserHandoffSlot<WKWebView>()
    private let theme: () -> Theme
    private let title: (String) async -> String?
    private var presented: WKWebView?
    private var observations: [NSKeyValueObservation] = []
    private var lookup: Task<Void, Never>?

    init(theme: @escaping () -> Theme, title: @escaping (String) async -> String?) {
        self.theme = theme
        self.title = title
        window = NSWindow(contentRect: NSRect(origin: Self.offscreen, size: Self.size), styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        shelf = NSView(frame: NSRect(origin: .zero, size: Self.size))
        super.init()
        window.identifier = Self.identifier
        window.title = "Agent Browser"
        window.isReleasedWhenClosed = false
        window.isRestorable = false
        window.tabbingMode = .disallowed
        window.contentView = shelf
        hiddenMode(true)
        window.delegate = self
    }

    func attach(_ webView: WKWebView) {
        webView.translatesAutoresizingMaskIntoConstraints = true
        webView.frame = shelf.bounds
        webView.autoresizingMask = [.width, .height]
        shelf.addSubview(webView)
    }

    func detach(_ webView: WKWebView) {
        if slot.finish(false, ifShowing: webView) { hide() }
        webView.removeFromSuperview()
    }

    func handoff(_ webView: WKWebView, reason: String, owner: String) async -> Bool {
        show(webView, reason: reason, owner: owner)
        return await slot.wait(for: webView)
    }

    func cancelHandoff() {
        if slot.finish(false) != nil { hide() }
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        done()
        return false
    }

    private func done() {
        if slot.finish(true) != nil { hide() }
    }

    private func show(_ webView: WKWebView, reason: String, owner: String) {
        restore()
        presented = webView
        bar.reason = reason
        bar.owner = owner
        observations = [
            webView.observe(\.url, options: [.initial]) { [weak self] webView, _ in
                MainActor.assumeIsolated { self?.observe(webView) }
            },
            webView.observe(\.hasOnlySecureContent) { [weak self] webView, _ in
                MainActor.assumeIsolated { self?.observe(webView) }
            },
        ]
        lookup?.cancel()
        lookup = Task { [weak self, title] in
            guard let name = await title(owner), !name.isEmpty, let self, self.presented === webView else { return }
            self.bar.owner = name
        }
        let theme = theme()
        let header = NSHostingView(rootView: BrowserHandoffBar(model: bar) { [weak self] in self?.done() }.environment(\.theme, theme))
        let content = NSView()
        for view in [header, webView] {
            view.removeFromSuperview()
            view.translatesAutoresizingMaskIntoConstraints = false
            content.addSubview(view)
        }
        NSLayoutConstraint.activate([
            header.topAnchor.constraint(equalTo: content.topAnchor),
            header.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            header.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            webView.topAnchor.constraint(equalTo: header.bottomAnchor),
            webView.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            webView.bottomAnchor.constraint(equalTo: content.bottomAnchor),
        ])
        window.contentView = content
        if let visible = NSScreen.main?.visibleFrame {
            let size = NSSize(width: min(Self.size.width, visible.width), height: min(Self.size.height, visible.height))
            var frame = window.frameRect(forContentRect: NSRect(origin: .zero, size: size))
            frame.size.height = min(frame.height, visible.height)
            frame.origin = NSPoint(x: visible.midX - frame.width / 2, y: visible.midY - frame.height / 2)
            window.setFrame(frame, display: true)
        }
        hiddenMode(false)
        window.makeKeyAndOrderFront(nil)
        window.makeFirstResponder(nil)
        if !TestMode.current { NSApp.activate() }
    }

    private func hide() {
        lookup?.cancel()
        restore()
        window.contentView = shelf
        window.orderOut(nil)
        hiddenMode(true)
        window.setContentSize(Self.size)
        window.setFrameOrigin(Self.offscreen)
    }

    private func hiddenMode(_ hidden: Bool) {
        window.isExcludedFromWindowsMenu = hidden
        window.collectionBehavior = hidden ? [.transient, .ignoresCycle, .stationary] : [.managed, .participatesInCycle]
    }

    private func observe(_ webView: WKWebView) {
        guard presented === webView else { return }
        bar.url = webView.url
        bar.secure = webView.url?.scheme?.lowercased() == "https" && webView.hasOnlySecureContent
    }

    private func restore() {
        observations = []
        guard let webView = presented else { return }
        presented = nil
        webView.removeFromSuperview()
        attach(webView)
    }

    static func dataStoreID(flavor: String) -> UUID {
        UUID(uuidString: flavor == "release" ? "67AF17A3-1824-46B4-B244-FB138FC9C4A7" : "BA45291B-9B60-420B-9DA6-0785A74DAAFA")!
    }

    static func userAgent() -> String {
        let safari = NSDictionary(contentsOfFile: "/Applications/Safari.app/Contents/Info.plist")?["CFBundleShortVersionString"] as? String
        let os = ProcessInfo.processInfo.operatingSystemVersion
        return AgentBrowser.safariUserAgent(version: safari ?? "\(os.majorVersion).\(os.minorVersion)")
    }
}

@MainActor
@Observable
private final class BrowserHandoffBarModel {
    var reason = ""
    var owner = ""
    var url: URL?
    var secure = false

    var address: String {
        guard let url else { return "No page" }
        return url.host() ?? url.absoluteString
    }
}

private struct BrowserHandoffBar: View {
    let model: BrowserHandoffBarModel
    let done: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            HStack(spacing: theme.spacing.l) {
                Image(systemName: model.secure ? "lock.fill" : "exclamationmark.triangle.fill")
                    .foregroundStyle(model.secure ? theme.muted : theme.warning)
                Text(model.address)
                    .font(theme.ui)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .truncationMode(.head)
                    .help(model.url?.absoluteString ?? model.address)
                    .layoutPriority(1)
                if !model.secure {
                    Text("Not secure")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.warning)
                        .fixedSize()
                }
                Spacer(minLength: theme.spacing.l)
                CardButton(label: "Done", tone: .primary, action: done)
                    .fixedSize()
                    .help("Done (↩)")
            }
            HStack(spacing: theme.spacing.l) {
                Image(systemName: "hand.raised")
                    .foregroundStyle(theme.accent)
                Text("\(Text("Agent:").foregroundStyle(theme.muted)) \(reason)")
                    .font(theme.ui)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help("Agent: \(reason)")
                    .layoutPriority(1)
                Text(model.owner)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(model.owner)
                    .frame(maxWidth: theme.labelMaxWidth, alignment: .leading)
                    .fixedSize(horizontal: true, vertical: false)
                Spacer(minLength: 0)
            }
        }
        .padding(.horizontal, theme.gutter)
        .padding(.vertical, theme.spacing.l)
        .frame(minWidth: 0, maxWidth: .infinity)
        .background(theme.background)
        .overlay(alignment: .bottom) {
            Rectangle().fill(theme.border).frame(height: 1)
        }
        .keyMonitor(active: true) { event in
            guard [36, 76].contains(event.keyCode),
                  event.modifierFlags.intersection([.command, .option, .control, .shift]).isEmpty,
                  !Self.inPage(event.window?.firstResponder)
            else { return false }
            done()
            return true
        }
    }

    private var reason: String {
        model.reason.isEmpty ? "The agent needs you on this page" : model.reason
    }

    private static func inPage(_ responder: NSResponder?) -> Bool {
        var view = responder as? NSView
        while let current = view {
            if current is WKWebView { return true }
            view = current.superview
        }
        return false
    }
}
