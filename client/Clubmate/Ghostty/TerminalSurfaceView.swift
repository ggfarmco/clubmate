import AppKit

import GhosttyKit

final class TerminalSurfaceView: NSView {
    let sessionID: String
    var onClose: (() -> Void)?

    nonisolated(unsafe) private(set) var surface: ghostty_surface_t?
    nonisolated(unsafe) private var cStrings: [UnsafeMutablePointer<CChar>] = []
    nonisolated(unsafe) private var observers: [NSObjectProtocol] = []
    private var destroyed = false
    private var visible = true
    private var trackingArea: NSTrackingArea?
    private var markedText = ""
    private var keyTextAccumulator: [String]?

    init(sessionID: String) {
        self.sessionID = sessionID
        super.init(frame: .zero)
        wantsLayer = true
        observe()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        nil
    }

    deinit {
        observers.forEach {
            NotificationCenter.default.removeObserver($0)
            NSWorkspace.shared.notificationCenter.removeObserver($0)
        }
        if let surface { ghostty_surface_free(surface) }
        cStrings.forEach { Darwin.free($0) }
    }

    func destroy() {
        guard !destroyed else { return }
        destroyed = true
        onClose = nil
        observers.forEach {
            NotificationCenter.default.removeObserver($0)
            NSWorkspace.shared.notificationCenter.removeObserver($0)
        }
        observers = []
        if let surface { ghostty_surface_free(surface) }
        surface = nil
        releaseCStrings()
    }

    func childClosed() {
        guard surface != nil else { return }
        onClose?()
    }

    func setVisible(_ visible: Bool) {
        self.visible = visible
        isHidden = !visible
        guard let surface else { return }
        ghostty_surface_set_occlusion(surface, visible)
        if visible { ghostty_surface_refresh(surface) }
    }

    func focus() {
        guard let window, window.firstResponder !== self else { return }
        window.makeFirstResponder(self)
    }

    override func setFrameSize(_ newSize: NSSize) {
        super.setFrameSize(newSize)
        surface == nil ? createSurface() : updateSize()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard surface != nil else { return createSurface() }
        updateSize()
        updateDisplayID()
        updateFocus()
    }

    override func viewDidChangeBackingProperties() {
        super.viewDidChangeBackingProperties()
        updateSize()
    }

    private func observe() {
        let center = NotificationCenter.default
        for name in [NSWindow.didBecomeKeyNotification, NSWindow.didResignKeyNotification] {
            observers.append(center.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.updateFocus() }
            })
        }
        observers.append(center.addObserver(forName: NSWindow.didChangeScreenNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.updateDisplayID() }
        })
        observers.append(NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.screensDidWakeNotification, object: nil, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.createSurface() }
        })
    }

    private func createSurface() {
        guard !destroyed, surface == nil, let app = GhosttyApp.shared.app, let window else { return }
        let size = convertToBacking(bounds).size
        guard size.width > 0, size.height > 0, let command = Self.command(sessionID) else { return }
        releaseCStrings()
        var config = ghostty_surface_config_new()
        config.platform_tag = GHOSTTY_PLATFORM_MACOS
        config.platform = ghostty_platform_u(macos: ghostty_platform_macos_s(nsview: Unmanaged.passUnretained(self).toOpaque()))
        config.userdata = Unmanaged.passUnretained(self).toOpaque()
        config.scale_factor = window.backingScaleFactor
        config.working_directory = retain(NSHomeDirectory())
        config.command = retain(command)
        config.wait_after_command = false
        var env = Self.environment.map { ghostty_env_var_s(key: retain($0.key), value: retain($0.value)) }
        surface = env.withUnsafeMutableBufferPointer { buffer in
            config.env_vars = buffer.baseAddress
            config.env_var_count = buffer.count
            return ghostty_surface_new(app, &config)
        }
        guard let surface else { return releaseCStrings() }
        ghostty_surface_set_color_scheme(surface, GhosttyApp.shared.colorScheme)
        updateDisplayID()
        updateSize()
        ghostty_surface_set_occlusion(surface, visible)
        updateFocus()
    }

    private func retain(_ string: String) -> UnsafePointer<CChar>? {
        guard let copy = strdup(string) else { return nil }
        cStrings.append(copy)
        return UnsafePointer(copy)
    }

    private func releaseCStrings() {
        cStrings.forEach { Darwin.free($0) }
        cStrings = []
    }

    private func updateSize() {
        guard let surface, let window else { return }
        let size = convertToBacking(bounds).size
        guard size.width > 0, size.height > 0 else { return }
        let scale = window.backingScaleFactor
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        layer?.contentsScale = scale
        CATransaction.commit()
        ghostty_surface_set_content_scale(surface, scale, scale)
        ghostty_surface_set_size(surface, UInt32(size.width), UInt32(size.height))
    }

    private func updateDisplayID() {
        guard let surface,
              let screen = window?.screen,
              let id = screen.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? UInt32
        else { return }
        ghostty_surface_set_display_id(surface, id)
    }

    private func updateFocus() {
        guard let surface else { return }
        ghostty_surface_set_focus(surface, window?.isKeyWindow == true && window?.firstResponder === self)
    }

    private static func command(_ sessionID: String) -> String? {
        guard let cli = Bundle.main.resourceURL?.appending(path: "bin/clubmate") else { return nil }
        return "\(quote(cli.path(percentEncoded: false))) attach \(quote(sessionID))"
    }

    private static func quote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    private static var environment: [String: String] {
        let env = ProcessInfo.processInfo.environment
        return ["CLUBMATE_STATE_DIR", "CLUBMATE_RUNTIME_DIR"].reduce(into: [:]) { result, key in
            if let value = env[key], !value.isEmpty { result[key] = value }
        }
    }
}

extension TerminalSurfaceView {
    override var acceptsFirstResponder: Bool { true }

    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    override func becomeFirstResponder() -> Bool {
        let result = super.becomeFirstResponder()
        if result, let surface { ghostty_surface_set_focus(surface, window?.isKeyWindow == true) }
        return result
    }

    override func resignFirstResponder() -> Bool {
        let result = super.resignFirstResponder()
        if result, let surface { ghostty_surface_set_focus(surface, false) }
        return result
    }

    override func resetCursorRects() {
        addCursorRect(bounds, cursor: .iBeam)
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let trackingArea { removeTrackingArea(trackingArea) }
        let area = NSTrackingArea(
            rect: bounds,
            options: [.mouseMoved, .mouseEnteredAndExited, .activeInKeyWindow, .inVisibleRect],
            owner: self
        )
        addTrackingArea(area)
        trackingArea = area
    }

    override func keyDown(with event: NSEvent) {
        guard let surface else { return super.keyDown(with: event) }
        let action = event.isARepeat ? GHOSTTY_ACTION_REPEAT : GHOSTTY_ACTION_PRESS
        let flags = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        if flags.contains(.command) {
            send(keyEvent(event, action), text: nil)
            return
        }
        if flags.contains(.control), !flags.contains(.option), !hasMarkedText() {
            send(keyEvent(event, action), text: event.charactersIgnoringModifiers)
            return
        }
        let hadMarkedText = hasMarkedText()
        let translated = translatedEvent(event, surface)
        keyTextAccumulator = []
        interpretKeyEvents([translated])
        let texts = keyTextAccumulator ?? []
        keyTextAccumulator = nil
        var key = keyEvent(event, action)
        key.consumed_mods = consumedMods(translated.modifierFlags)
        key.composing = hasMarkedText() || hadMarkedText
        if !texts.isEmpty {
            key.composing = false
            texts.forEach { send(key, text: $0) }
        } else if !hasMarkedText() {
            let text = Self.printable(event.characters ?? "")
            if !text.isEmpty, !key.composing {
                send(key, text: text)
            } else {
                key.consumed_mods = GHOSTTY_MODS_NONE
                send(key, text: nil)
            }
        }
    }

    override func keyUp(with event: NSEvent) {
        guard surface != nil else { return super.keyUp(with: event) }
        send(keyEvent(event, GHOSTTY_ACTION_RELEASE), text: nil)
    }

    override func flagsChanged(with event: NSEvent) {
        guard surface != nil else { return super.flagsChanged(with: event) }
        send(keyEvent(event, Self.isFlagPress(event) ? GHOSTTY_ACTION_PRESS : GHOSTTY_ACTION_RELEASE), text: nil)
    }

    override func doCommand(by selector: Selector) {}

    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        mouseButton(event, GHOSTTY_MOUSE_PRESS, GHOSTTY_MOUSE_LEFT)
    }

    override func mouseUp(with event: NSEvent) {
        mouseButton(event, GHOSTTY_MOUSE_RELEASE, GHOSTTY_MOUSE_LEFT)
    }

    override func rightMouseDown(with event: NSEvent) {
        mouseButton(event, GHOSTTY_MOUSE_PRESS, GHOSTTY_MOUSE_RIGHT)
    }

    override func rightMouseUp(with event: NSEvent) {
        mouseButton(event, GHOSTTY_MOUSE_RELEASE, GHOSTTY_MOUSE_RIGHT)
    }

    override func otherMouseDown(with event: NSEvent) {
        guard event.buttonNumber == 2 else { return super.otherMouseDown(with: event) }
        mouseButton(event, GHOSTTY_MOUSE_PRESS, GHOSTTY_MOUSE_MIDDLE)
    }

    override func otherMouseUp(with event: NSEvent) {
        guard event.buttonNumber == 2 else { return super.otherMouseUp(with: event) }
        mouseButton(event, GHOSTTY_MOUSE_RELEASE, GHOSTTY_MOUSE_MIDDLE)
    }

    override func mouseMoved(with event: NSEvent) { mousePosition(event) }
    override func mouseDragged(with event: NSEvent) { mousePosition(event) }
    override func rightMouseDragged(with event: NSEvent) { mousePosition(event) }
    override func otherMouseDragged(with event: NSEvent) { mousePosition(event) }
    override func mouseEntered(with event: NSEvent) { mousePosition(event) }

    override func mouseExited(with event: NSEvent) {
        guard let surface, NSEvent.pressedMouseButtons == 0 else { return }
        ghostty_surface_mouse_pos(surface, -1, -1, Self.mods(event.modifierFlags))
    }

    override func scrollWheel(with event: NSEvent) {
        guard let surface else { return }
        mousePosition(event)
        let mods: ghostty_input_scroll_mods_t = event.hasPreciseScrollingDeltas ? 1 : 0
        ghostty_surface_mouse_scroll(surface, event.scrollingDeltaX, event.scrollingDeltaY, mods)
    }

    @objc func copy(_ sender: Any?) {
        performBinding("copy_to_clipboard")
    }

    @objc func paste(_ sender: Any?) {
        performBinding("paste_from_clipboard")
    }

    override func selectAll(_ sender: Any?) {
        performBinding("select_all")
    }

    private func performBinding(_ action: String) {
        guard let surface else { return }
        _ = ghostty_surface_binding_action(surface, action, UInt(action.utf8.count))
    }

    private func mouseButton(_ event: NSEvent, _ state: ghostty_input_mouse_state_e, _ button: ghostty_input_mouse_button_e) {
        guard let surface else { return }
        mousePosition(event)
        _ = ghostty_surface_mouse_button(surface, state, button, Self.mods(event.modifierFlags))
    }

    private func mousePosition(_ event: NSEvent) {
        guard let surface else { return }
        let point = convert(event.locationInWindow, from: nil)
        ghostty_surface_mouse_pos(surface, point.x, bounds.height - point.y, Self.mods(event.modifierFlags))
    }

    private func send(_ key: ghostty_input_key_s, text: String?) {
        guard let surface else { return }
        var key = key
        guard let text, !text.isEmpty else {
            key.text = nil
            _ = ghostty_surface_key(surface, key)
            return
        }
        text.withCString { pointer in
            key.text = pointer
            _ = ghostty_surface_key(surface, key)
        }
    }

    private func keyEvent(_ event: NSEvent, _ action: ghostty_input_action_e) -> ghostty_input_key_s {
        var key = ghostty_input_key_s()
        key.action = action
        key.keycode = UInt32(event.keyCode)
        key.mods = Self.mods(event.modifierFlags)
        key.consumed_mods = GHOSTTY_MODS_NONE
        key.composing = false
        key.text = nil
        if event.type == .keyDown || event.type == .keyUp,
           let scalar = event.characters(byApplyingModifiers: [])?.unicodeScalars.first {
            key.unshifted_codepoint = scalar.value
        }
        return key
    }

    private func translatedEvent(_ event: NSEvent, _ surface: ghostty_surface_t) -> NSEvent {
        let translation = ghostty_surface_key_translation_mods(surface, Self.mods(event.modifierFlags)).rawValue
        var flags = event.modifierFlags
        let pairs: [(UInt32, NSEvent.ModifierFlags)] = [
            (GHOSTTY_MODS_SHIFT.rawValue, .shift),
            (GHOSTTY_MODS_CTRL.rawValue, .control),
            (GHOSTTY_MODS_ALT.rawValue, .option),
            (GHOSTTY_MODS_SUPER.rawValue, .command),
        ]
        for (bit, flag) in pairs {
            if translation & bit != 0 { flags.insert(flag) } else { flags.remove(flag) }
        }
        guard flags != event.modifierFlags else { return event }
        return NSEvent.keyEvent(
            with: event.type,
            location: event.locationInWindow,
            modifierFlags: flags,
            timestamp: event.timestamp,
            windowNumber: event.windowNumber,
            context: nil,
            characters: event.characters(byApplyingModifiers: flags) ?? "",
            charactersIgnoringModifiers: event.charactersIgnoringModifiers ?? "",
            isARepeat: event.isARepeat,
            keyCode: event.keyCode
        ) ?? event
    }

    private func consumedMods(_ flags: NSEvent.ModifierFlags) -> ghostty_input_mods_e {
        var mods = GHOSTTY_MODS_NONE.rawValue
        if flags.contains(.shift) { mods |= GHOSTTY_MODS_SHIFT.rawValue }
        if flags.contains(.option) { mods |= GHOSTTY_MODS_ALT.rawValue }
        return ghostty_input_mods_e(rawValue: mods)
    }

    private static func mods(_ flags: NSEvent.ModifierFlags) -> ghostty_input_mods_e {
        var mods = GHOSTTY_MODS_NONE.rawValue
        if flags.contains(.shift) { mods |= GHOSTTY_MODS_SHIFT.rawValue }
        if flags.contains(.control) { mods |= GHOSTTY_MODS_CTRL.rawValue }
        if flags.contains(.option) { mods |= GHOSTTY_MODS_ALT.rawValue }
        if flags.contains(.command) { mods |= GHOSTTY_MODS_SUPER.rawValue }
        if flags.contains(.capsLock) { mods |= GHOSTTY_MODS_CAPS.rawValue }
        return ghostty_input_mods_e(rawValue: mods)
    }

    private static func isFlagPress(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        switch event.keyCode {
        case 56, 60: return flags.contains(.shift)
        case 58, 61: return flags.contains(.option)
        case 59, 62: return flags.contains(.control)
        case 54, 55: return flags.contains(.command)
        case 57: return flags.contains(.capsLock)
        default: return false
        }
    }

    private static func printable(_ text: String) -> String {
        guard let scalar = text.unicodeScalars.first else { return "" }
        return scalar.value < 0x20 || (0xF700...0xF8FF).contains(scalar.value) ? "" : text
    }
}

extension TerminalSurfaceView: NSMenuItemValidation {
    func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
        switch menuItem.action {
        case #selector(copy(_:)): surface.map { ghostty_surface_has_selection($0) } ?? false
        case #selector(paste(_:)), #selector(selectAll(_:)): surface != nil
        default: true
        }
    }
}

extension TerminalSurfaceView: @preconcurrency NSTextInputClient {
    func insertText(_ string: Any, replacementRange: NSRange) {
        let text = (string as? String) ?? (string as? NSAttributedString)?.string ?? ""
        guard !text.isEmpty else { return }
        unmarkText()
        if keyTextAccumulator != nil {
            keyTextAccumulator?.append(text)
        } else if let surface {
            text.withCString { ghostty_surface_text(surface, $0, UInt(text.utf8.count)) }
        }
    }

    func setMarkedText(_ string: Any, selectedRange: NSRange, replacementRange: NSRange) {
        markedText = (string as? String) ?? (string as? NSAttributedString)?.string ?? ""
        guard let surface else { return }
        markedText.withCString { ghostty_surface_preedit(surface, $0, UInt(markedText.utf8.count)) }
    }

    func unmarkText() {
        guard !markedText.isEmpty else { return }
        markedText = ""
        if let surface { ghostty_surface_preedit(surface, nil, 0) }
    }

    func hasMarkedText() -> Bool {
        !markedText.isEmpty
    }

    func markedRange() -> NSRange {
        markedText.isEmpty ? NSRange(location: NSNotFound, length: 0) : NSRange(location: 0, length: markedText.utf16.count)
    }

    func selectedRange() -> NSRange {
        NSRange(location: NSNotFound, length: 0)
    }

    func validAttributesForMarkedText() -> [NSAttributedString.Key] {
        []
    }

    func attributedSubstring(forProposedRange range: NSRange, actualRange: NSRangePointer?) -> NSAttributedString? {
        nil
    }

    func characterIndex(for point: NSPoint) -> Int {
        NSNotFound
    }

    func firstRect(forCharacterRange range: NSRange, actualRange: NSRangePointer?) -> NSRect {
        guard let surface, let window else { return .zero }
        var x = 0.0, y = 0.0, width = 0.0, height = 0.0
        ghostty_surface_ime_point(surface, &x, &y, &width, &height)
        let point = window.convertPoint(toScreen: convert(NSPoint(x: x, y: bounds.height - y), to: nil))
        return NSRect(x: point.x, y: point.y - height, width: width, height: height)
    }
}
