import AppKit
import ObjectiveC

import ClubmateKit

@MainActor
enum TestKeyWindow {
    private(set) static var current: NSWindow?
    private(set) static var main: NSWindow?
    private static weak var requested: NSWindow?
    private static var announced = Set<Int>()

    static func install() {
        swap(NSWindow.self, #selector(getter: NSWindow.isKeyWindow), #selector(NSWindow.test_isKeyWindow))
        swap(NSWindow.self, #selector(getter: NSWindow.isMainWindow), #selector(NSWindow.test_isMainWindow))
        swap(NSWindow.self, #selector(NSWindow.makeKey), #selector(NSWindow.test_makeKey))
        swap(NSWindow.self, #selector(NSWindow.makeKeyAndOrderFront(_:)), #selector(NSWindow.test_makeKeyAndOrderFront(_:)))
        swap(NSApplication.self, #selector(getter: NSApplication.keyWindow), #selector(NSApplication.test_keyWindow))
        swap(NSApplication.self, #selector(getter: NSApplication.mainWindow), #selector(NSApplication.test_mainWindow))
        swap(NSApplication.self, #selector(NSApplication.activate(ignoringOtherApps:)), #selector(NSApplication.test_activate(ignoringOtherApps:)))
        swap(NSApplication.self, #selector(NSApplication.activate as (NSApplication) -> () -> Void), #selector(NSApplication.test_activateQuietly))
        swap(NSApplication.self, #selector(getter: NSApplication.isActive), #selector(NSApplication.test_isActive))
        swap(NSMenu.self, #selector(NSMenu.popUp(positioning:at:in:)), #selector(NSMenu.test_popUp(positioning:at:in:)))
        swap(object_getClass(NSMenu.self)!, #selector(NSMenu.popUpContextMenu(_:with:for:)), #selector(NSMenu.test_popUpContextMenu(_:with:for:)))
        NotificationCenter.default.addObserver(forName: NSMenu.didBeginTrackingNotification, object: nil, queue: nil) { note in
            nonisolated(unsafe) let menu = note.object as? NSMenu
            MainActor.assumeIsolated { menu.map(TestMenus.tracking) }
        }
    }

    private static func swap(_ cls: AnyClass, _ original: Selector, _ replacement: Selector) {
        guard let a = class_getInstanceMethod(cls, original), let b = class_getInstanceMethod(cls, replacement) else {
            fatalError("test input: cannot swizzle \(original)")
        }
        if class_addMethod(cls, original, method_getImplementation(b), method_getTypeEncoding(b)) {
            class_replaceMethod(cls, replacement, method_getImplementation(a), method_getTypeEncoding(a))
        } else {
            method_exchangeImplementations(a, b)
        }
    }

    static func request(_ window: NSWindow) {
        guard window.canBecomeKey else { return }
        requested = window
        sync()
    }

    static func sync() {
        let base = (requested.flatMap { $0.isVisible ? $0 : nil }) ?? workspaceWindow()
        var key = base
        while let sheet = key?.attachedSheet, sheet.isVisible { key = sheet }
        let main = base.flatMap { $0.canBecomeMain && $0.sheetParent == nil && !($0 is NSPanel) && !$0.isPopover ? $0 : nil } ?? workspaceWindow()
        if main !== self.main {
            let old = self.main
            changing(["mainWindow"], [old, main].compactMap { $0 }, ["isMainWindow", "mainWindow"]) { self.main = main }
            old?.resignMain()
            main?.becomeMain()
        }
        if key !== current {
            let old = current
            changing(["keyWindow"], [old, key].compactMap { $0 }, ["isKeyWindow", "keyWindow"]) { current = key }
            if key?.parent == nil || key?.parent !== old { old?.resignKey() }
            key?.becomeKey()
            if old == nil, key != nil { NotificationCenter.default.post(name: NSApplication.didBecomeActiveNotification, object: NSApp) }
        }
    }

    static func announce() {
        sync()
        guard let current, announced.insert(current.windowNumber).inserted else { return }
        current.resignKey()
        current.becomeKey()
    }

    private static func changing(_ appKeys: [String], _ windows: [NSWindow], _ windowKeys: [String], _ change: () -> Void) {
        appKeys.forEach { NSApp.willChangeValue(forKey: $0) }
        windows.forEach { window in windowKeys.forEach { window.willChangeValue(forKey: $0) } }
        change()
        windows.forEach { window in windowKeys.forEach { window.didChangeValue(forKey: $0) } }
        appKeys.forEach { NSApp.didChangeValue(forKey: $0) }
    }

    private static func workspaceWindow() -> NSWindow? {
        NSApp.windows.first { $0.isVisible && $0.styleMask.contains(.titled) && !($0 is NSPanel) && $0.sheetParent == nil && $0.identifier != BrowserWindowHost.identifier }
    }
}

extension NSWindow {
    var isPopover: Bool { String(describing: type(of: self)).contains("Popover") }

    @objc func test_isKeyWindow() -> Bool {
        MainActor.assumeIsolated { TestKeyWindow.current === self }
    }

    @objc func test_isMainWindow() -> Bool {
        MainActor.assumeIsolated { TestKeyWindow.main === self }
    }

    @objc func test_makeKey() {
        test_makeKey()
        MainActor.assumeIsolated { TestKeyWindow.request(self) }
    }

    @objc func test_makeKeyAndOrderFront(_ sender: Any?) {
        test_makeKeyAndOrderFront(sender)
        MainActor.assumeIsolated { TestKeyWindow.request(self) }
    }
}

extension NSApplication {
    @objc func test_keyWindow() -> NSWindow? {
        MainActor.assumeIsolated { TestKeyWindow.current }
    }

    @objc func test_mainWindow() -> NSWindow? {
        MainActor.assumeIsolated { TestKeyWindow.main }
    }

    @objc func test_isActive() -> Bool { true }

    @objc func test_activate(ignoringOtherApps: Bool) {}

    @objc func test_activateQuietly() {}
}

extension NSMenu {
    @MainActor @objc func test_popUp(positioning item: NSMenuItem?, at location: NSPoint, in view: NSView?) -> Bool {
        TestMenus.shown(self)
    }

    @MainActor @objc class func test_popUpContextMenu(_ menu: NSMenu, with event: NSEvent, for view: NSView) {
        _ = TestMenus.shown(menu)
    }
}

@MainActor
enum TestMenus {
    static var choice: [String]?
    static var opened: [LayoutMenuItem]?
    static var picked = false

    static func shown(_ menu: NSMenu) -> Bool {
        prepare(menu)
        opened = items(menu)
        guard let choice else { return false }
        self.choice = nil
        guard let (owner, index) = find(choice, in: menu) else { return false }
        owner.performActionForItem(at: index)
        picked = true
        return true
    }

    static func tracking(_ menu: NSMenu) {
        guard menu.supermenu == nil, menu !== NSApp.mainMenu else { return }
        TestHarness.shared.conceal()
        opened = items(menu)
        let found = choice.flatMap { find($0, in: menu) }
        choice = nil
        picked = found != nil
        DispatchQueue.main.async {
            menu.cancelTrackingWithoutAnimation()
            guard let (owner, index) = found else { return }
            DispatchQueue.main.async { owner.performActionForItem(at: index) }
        }
    }

    static func prepare(_ menu: NSMenu) {
        menu.delegate?.menuNeedsUpdate?(menu)
        menu.update()
    }

    static func items(_ menu: NSMenu) -> [LayoutMenuItem] {
        menu.items.filter { !$0.isSeparatorItem && !$0.isHidden }.map { item in
            let children = item.submenu.map { submenu -> [LayoutMenuItem] in
                prepare(submenu)
                return items(submenu)
            }
            return LayoutMenuItem(title: item.title, enabled: item.isEnabled, checked: item.state == .on, children: children)
        }
    }

    private static func find(_ path: [String], in menu: NSMenu) -> (NSMenu, Int)? {
        guard let first = path.first, let index = menu.items.firstIndex(where: { !$0.isSeparatorItem && !$0.isHidden && $0.title == first }) else { return nil }
        guard path.count > 1 else { return menu.items[index].isEnabled ? (menu, index) : nil }
        guard let submenu = menu.items[index].submenu else { return nil }
        prepare(submenu)
        return find(Array(path.dropFirst()), in: submenu)
    }
}

final class TestDraggingInfo: NSObject, NSDraggingInfo {
    let draggingDestinationWindow: NSWindow?
    let draggingLocation: NSPoint
    let draggingPasteboard: NSPasteboard
    let draggingSequenceNumber = Int.random(in: 1...Int(Int32.max))
    var draggingFormation: NSDraggingFormation = .default
    var animatesToDestination = false
    var numberOfValidItemsForDrop: Int
    var springLoadingHighlight: NSSpringLoadingHighlight { .none }
    var draggingSourceOperationMask: NSDragOperation { [.copy, .link, .generic] }
    var draggedImageLocation: NSPoint { draggingLocation }
    var draggedImage: NSImage? { nil }
    var draggingSource: Any? { nil }

    @MainActor
    init(window: NSWindow, location: NSPoint, urls: [URL]) {
        let pasteboard = NSPasteboard(name: NSPasteboard.Name("clubmate.test.drop.\(UUID().uuidString)"))
        pasteboard.clearContents()
        pasteboard.writeObjects(urls as [NSURL])
        draggingDestinationWindow = window
        draggingLocation = location
        draggingPasteboard = pasteboard
        numberOfValidItemsForDrop = urls.count
    }

    func slideDraggedImage(to screenPoint: NSPoint) {}

    func resetSpringLoading() {}

    func enumerateDraggingItems(options enumOpts: NSDraggingItemEnumerationOptions = [], for view: NSView?, classes classArray: [AnyClass], searchOptions: [NSPasteboard.ReadingOptionKey: Any] = [:], using block: (NSDraggingItem, Int, UnsafeMutablePointer<ObjCBool>) -> Void) {
        let objects = draggingPasteboard.readObjects(forClasses: classArray, options: searchOptions) ?? []
        var stop: ObjCBool = false
        for (index, object) in objects.enumerated() {
            guard let writer = object as? NSPasteboardWriting else { continue }
            block(NSDraggingItem(pasteboardWriter: writer), index, &stop)
            if stop.boolValue { break }
        }
    }

    func release() {
        draggingPasteboard.releaseGlobally()
    }
}

