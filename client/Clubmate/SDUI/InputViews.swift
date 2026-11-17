import AppKit
import SwiftUI

import ClubmateKit

struct ButtonNodeView: View {
    let node: SDUINode

    @State private var hovering = false
    @State private var keyboardFocus = false
    @State private var popoverShown = false
    @State private var popoverLink = SDUIPopover()
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme
    @Environment(\.buttonKeyHint) private var keyHint
    @Environment(\.buttonIsDefault) private var isDefault
    @Environment(\.sduiPopover) private var popover
    @Environment(\.formDocked) private var docked

    private var variant: String? { node.string("variant") }
    private var disabled: Bool { press.disabled }
    private var loading: String? { press.loading ?? press.saving }
    private var combo: KeyCombo? { press.combo }

    var body: some View {
        Button(action: press.click) { chrome }
            .buttonStyle(.plain)
            .disabled(disabled && loading == nil)
            .focusable(!disabled, interactions: .activate)
            .focusEffectDisabled()
            .focused($focused)
            .onChange(of: focused) { _, now in
                keyboardFocus = now && !Self.mouseEvents.contains(NSApp.currentEvent?.type ?? .keyDown)
            }
            .onKeyPress(keys: [.space, .return]) { _ in
                press.click()
                return .handled
            }
            .onHover { hovering = $0 }
            .keyMonitor(active: combo != nil && (!disabled || press.unanswered) && !popoverShown, overTerminals: combo?.passesTerminals == true && !docked, claims: docked ? claim : nil, handle: press.handle)
            .keyMonitor(active: popoverShown, handle: popoverLink.handle)
            .help(iconOnly ? tooltip : "")
            .accessibilityLabel(node.string("label") ?? "")
            .popover(isPresented: $popoverShown, arrowEdge: .bottom) {
                if let popoverForm {
                    SDUIPopoverView(form: popoverForm, popover: popoverLink)
                }
            }
            .onAppear { popoverLink.dismiss = { popoverShown = false } }
            .modifier(PopoverPresenter(target: popoverForm == nil ? nil : node.id.map { "popover:" + $0 }, shown: $popoverShown))
    }

    private func claim(_ event: NSEvent) -> KeyClaim? {
        combo?.fires(on: event) == true ? .docked : nil
    }

    private var popoverForm: SDUINode? { node.children.first { $0.type == "form" } }

    private var iconOnly: Bool { node.bool("iconOnly") == true && node.string("icon") != nil }

    private var height: CGFloat { variant == nil ? theme.rowHeight : theme.control }

    private var tooltip: String {
        [node.string("label") ?? "", keyLabel ?? ""].filter { !$0.isEmpty }.joined(separator: " ")
    }

    private var chrome: some View {
        ZStack {
            label
                .opacity(loading == nil ? 1 : 0)
            if let loading {
                Text(loading)
                    .lineLimit(1)
            }
        }
        .font(theme.ui)
        .foregroundStyle(foreground)
        .padding(.horizontal, iconOnly ? 0 : theme.gutter)
        .frame(width: iconOnly ? height : nil, height: height)
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(fill))
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(hovering && !disabled ? theme.hover : .clear))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(stroke, lineWidth: 1))
        .overlay {
            if isDefault || keyboardFocus && focused {
                RoundedRectangle(cornerRadius: theme.radius.m + Self.ringGap)
                    .stroke(theme.accent, lineWidth: theme.focusRing)
                    .padding(-Self.ringGap)
            }
        }
        .contentShape(Rectangle())
    }

    static let ringGap: CGFloat = 2
    static let mouseEvents: Set<NSEvent.EventType> = [.leftMouseDown, .leftMouseUp, .rightMouseDown, .rightMouseUp, .otherMouseDown, .otherMouseUp]

    private var label: some View {
        HStack(spacing: theme.spacing.m) {
            if let icon = node.string("icon") {
                Image(systemName: SymbolName.resolve(icon))
            }
            if !iconOnly {
                Text(node.string("label") ?? "")
                    .lineLimit(1)
            }
            if let keyLabel, !iconOnly {
                Text(keyLabel)
                    .font(theme.uiSmall)
                    .foregroundStyle(disabled ? theme.faint : variant == "primary" ? theme.onAccent : theme.muted)
            }
        }
    }

    private var press: ButtonPress {
        ButtonPress(node: node, dispatch: dispatch, form: form, open: popoverForm == nil ? nil : { popoverShown = true }, dismiss: popover?.dismiss)
    }

    private var keyLabel: String? { combo?.label ?? node.string("hint").flatMap(KeyCombo.init)?.label ?? (popover == nil ? nil : SDUIPopover.keyLabel(node)) ?? keyHint }

    private var foreground: Color {
        if loading != nil { return variant == "primary" ? theme.onAccent : theme.text2 }
        if disabled { return theme.faint }
        switch variant {
        case "primary": return theme.onAccent
        case "danger": return theme.danger
        case "ghost": return hovering ? theme.foreground : theme.text2
        default: return theme.foreground
        }
    }

    private var fill: Color {
        switch variant {
        case "primary": disabled && loading == nil ? theme.selectedIdle : theme.accent
        case "secondary": theme.surface2
        default: .clear
        }
    }

    private var stroke: Color {
        switch variant {
        case "secondary": theme.borderStrong
        case "danger": theme.toneBorder(theme.danger)
        default: .clear
        }
    }
}

extension KeyCombo {
    @MainActor
    func fires(on event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        guard matches(keyCode: event.keyCode, command: flags.contains(.command), control: flags.contains(.control), option: flags.contains(.option), shift: flags.contains(.shift)) else { return false }
        if bare || movesCaret, event.window?.firstResponder is NSTextView { return false }
        return NSApp.mainMenu.map { MenuKeys.claims($0, event) } != true
    }
}

@MainActor
struct ButtonPress {
    let node: SDUINode
    let dispatch: SDUIDispatch
    let form: FormState?
    var open: (() -> Void)?
    var dismiss: (() -> Void)?

    var combo: KeyCombo? { node.string("shortcut").flatMap(KeyCombo.init) }
    var loading: String? { node.string("loading").flatMap { $0.isEmpty ? nil : $0 } }
    var unanswered: Bool { form?.unanswered == true && (node.string("action") == nil || node.bool("withForm") == true) }
    var disabled: Bool { node.bool("disabled") == true || loading != nil || unanswered }
    var saving: String? { form?.saving == true && node.string("action") == nil ? UIFormSave.verb : nil }

    func handle(_ event: NSEvent) -> Bool {
        guard combo?.fires(on: event) == true else { return false }
        click()
        return true
    }

    func click() {
        guard !disabled else { return }
        guard let message = node.confirmation() else { return perform() }
        Task {
            if await Confirmation.ask(message, action: node.confirmActionTitle()) { perform() }
        }
    }

    private func perform() {
        if let text = node.string("copy") { return Clipboard.copy(text) }
        if let open { return open() }
        if let dismiss, node.string("action") == SDUIPopover.dismissAction { return dismiss() }
        guard let form else { return dispatch(node, .click) }
        if node.string("action") == nil { return form.submit() }
        dispatch(node, node.bool("withForm") == true ? .submit(.object(form.values)) : .click)
    }
}

@MainActor
enum Clipboard {
    static let testName = NSPasteboard.Name("com.clubmate.test")

    static var board: NSPasteboard { TestMode.current ? NSPasteboard(name: testName) : .general }

    static func copy(_ text: String) {
        board.clearContents()
        board.setString(text, forType: .string)
    }
}

@MainActor
enum Confirmation {
    private static let bodySize = NSSize(width: 560, height: 300)

    static func ask(_ request: ConfirmRequest, font: NSFont, window: NSWindow?, expiry: ConfirmExpiry?) async -> Bool {
        await ask(request.message, detail: request.detail, facts: request.facts, body: request.displayBody, font: font, action: "Allow", cancel: "Deny", cancelFirst: true, window: window, expiry: expiry)
    }

    static func ask(_ message: String, detail: String = "", facts: String = "", body: String? = nil, font: NSFont = .monospacedSystemFont(ofSize: NSFont.smallSystemFontSize, weight: .regular), action: String, cancel: String = "Cancel", destructive: Bool = false, cancelFirst: Bool = false, window: NSWindow? = nil, expiry: ConfirmExpiry? = nil) async -> Bool {
        let alert = NSAlert()
        alert.messageText = message
        alert.informativeText = detail
        alert.accessoryView = accessory(facts: facts, body: body, font: font)
        if TestMode.current, let view = alert.window.contentView {
            TestHarness.shared.probe("alert.detail", role: "text", label: detail, filling: view)
        }
        let buttons = (cancelFirst ? [cancel, action] : [action, cancel]).map { alert.addButton(withTitle: $0) }
        let (confirm, dismiss) = cancelFirst ? (buttons[1], buttons[0]) : (buttons[0], buttons[1])
        let confirmed: NSApplication.ModalResponse = cancelFirst ? .alertSecondButtonReturn : .alertFirstButtonReturn
        let denied: NSApplication.ModalResponse = cancelFirst ? .alertFirstButtonReturn : .alertSecondButtonReturn
        let safe = destructive || cancelFirst
        if safe {
            confirm.hasDestructiveAction = destructive
            confirm.keyEquivalent = ""
            confirm.refusesFirstResponder = true
            dismiss.keyEquivalent = "\r"
            alert.window.initialFirstResponder = dismiss
        }
        let escape = safe ? NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            guard event.window === alert.window, event.charactersIgnoringModifiers == "\u{1b}" else { return event }
            dismiss.performClick(nil)
            return nil
        } : nil
        defer { escape.map(NSEvent.removeMonitor) }
        guard let window = window ?? NSApp.promptWindow else {
            if TestMode.current { return false }
            expiry?.onExpire { NSApp.abortModal() }
            return alert.runModal() == confirmed
        }
        expiry?.onExpire { [weak window] in window?.endSheet(alert.window, returnCode: denied) }
        return await alert.beginSheetModal(for: window) == confirmed
    }

    private static func accessory(facts: String, body: String?, font: NSFont) -> NSView? {
        let small = NSFont(descriptor: font.fontDescriptor, size: max(font.pointSize - 1, NSFont.smallSystemFontSize)) ?? font
        let label: NSTextField? = facts.isEmpty ? nil : {
            let label = NSTextField(wrappingLabelWithString: facts)
            label.font = small
            label.textColor = .secondaryLabelColor
            label.isSelectable = false
            label.preferredMaxLayoutWidth = bodySize.width
            label.frame.size = NSSize(width: bodySize.width, height: neededHeight(label, width: bodySize.width))
            return label
        }()
        let scroll = body.map { bodyView($0, font: font) }
        guard label != nil || scroll != nil else { return nil }
        let gap: CGFloat = label != nil && scroll != nil ? factsGap : 0
        let height = (label?.frame.height ?? 0) + gap + (scroll?.frame.height ?? 0)
        let container = NSView(frame: NSRect(x: 0, y: 0, width: bodySize.width, height: height))
        if let scroll {
            scroll.frame.origin = .zero
            container.addSubview(scroll)
        }
        if let label {
            label.frame.origin = NSPoint(x: 0, y: height - label.frame.height)
            container.addSubview(label)
            if TestMode.current {
                TestHarness.shared.probe("alert.facts", role: neededHeight(label, width: label.frame.width) > label.frame.height ? "text clipped" : "text", label: facts, filling: label)
                TestHarness.shared.probe("alert.accessory", role: "stack", label: "", filling: container)
            }
        }
        return container
    }

    private static func neededHeight(_ label: NSTextField, width: CGFloat) -> CGFloat {
        (label.cell?.cellSize(forBounds: NSRect(x: 0, y: 0, width: width, height: .greatestFiniteMagnitude)).height ?? label.fittingSize.height).rounded(.up)
    }

    private static let factsGap: CGFloat = 10
    private static let headerGap: CGFloat = 8

    private static func bodyView(_ text: String, font: NSFont) -> NSView {
        let scroll = NSScrollView(frame: NSRect(origin: .zero, size: bodySize))
        scroll.borderType = .bezelBorder
        scroll.hasVerticalScroller = true
        let view = NSTextView(usingTextLayoutManager: false)
        view.frame = NSRect(origin: .zero, size: scroll.contentSize)
        view.autoresizingMask = .width
        view.isEditable = false
        view.isSelectable = false
        view.font = font
        view.minSize = NSSize(width: 0, height: scroll.contentSize.height)
        view.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        view.isVerticallyResizable = true
        view.textContainer?.widthTracksTextView = true
        scroll.documentView = view
        view.textStorage?.setAttributedString(styledBody(text, font: font))
        return scroll
    }

    private static func styledBody(_ text: String, font: NSFont) -> NSAttributedString {
        let indent = NSMutableParagraphStyle()
        indent.headIndent = ("  " as NSString).size(withAttributes: [.font: font]).width
        let plain: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: NSColor.textColor, .paragraphStyle: indent]
        let spaced = NSMutableParagraphStyle()
        spaced.paragraphSpacingBefore = headerGap
        func header(_ first: Bool) -> [NSAttributedString.Key: Any] {
            [
                .font: NSFontManager.shared.convert(font, toHaveTrait: .boldFontMask),
                .foregroundColor: NSColor.textColor,
                .backgroundColor: NSColor.selectedContentBackgroundColor.withAlphaComponent(0.25),
                .paragraphStyle: first ? NSParagraphStyle.default : spaced,
            ]
        }
        let result = NSMutableAttributedString()
        let lines = text.components(separatedBy: "\n")
        for (index, line) in lines.enumerated() {
            let end = index < lines.count - 1 ? "\n" : ""
            result.append(NSAttributedString(string: line + end, attributes: line.hasPrefix("──── ") ? header(index == 0) : plain))
        }
        return result
    }
}

@MainActor
enum TextPrompt {
    static func ask(_ message: String, text: String, action: String) async -> String? {
        let alert = NSAlert()
        alert.messageText = message
        alert.addButton(withTitle: action)
        alert.addButton(withTitle: "Cancel")
        let field = NSTextField(string: text)
        field.frame = NSRect(x: 0, y: 0, width: 260, height: 22)
        alert.accessoryView = field
        alert.window.initialFirstResponder = field
        alert.testProbe()
        let response: NSApplication.ModalResponse
        if let window = NSApp.promptWindow {
            response = await alert.beginSheetModal(for: window)
        } else if TestMode.current {
            return nil
        } else {
            response = alert.runModal()
        }
        return response == .alertFirstButtonReturn ? field.stringValue : nil
    }
}

@MainActor
enum ChoicePrompt {
    static func ask(_ message: String, options: [String], selected: Int = 0, action: String) async -> Int? {
        let alert = NSAlert()
        alert.messageText = message
        alert.addButton(withTitle: action)
        alert.addButton(withTitle: "Cancel")
        let popup = NSPopUpButton(frame: NSRect(x: 0, y: 0, width: 260, height: 26), pullsDown: false)
        for option in options {
            popup.menu?.addItem(NSMenuItem(title: option, action: nil, keyEquivalent: ""))
        }
        if options.indices.contains(selected) { popup.selectItem(at: selected) }
        alert.accessoryView = popup
        alert.window.initialFirstResponder = popup
        alert.testProbe(options)
        let response: NSApplication.ModalResponse
        if let window = NSApp.promptWindow {
            response = await alert.beginSheetModal(for: window)
        } else if TestMode.current {
            return nil
        } else {
            response = alert.runModal()
        }
        return response == .alertFirstButtonReturn ? popup.indexOfSelectedItem : nil
    }
}

struct TextFieldNodeView: View {
    let node: SDUINode

    @State private var text = ""
    @State private var typed = false
    @State private var pending: Task<Void, Never>?
    @State private var targeted = false
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme
    @Environment(\.focusRouter) private var router
    @Environment(\.sidebarSlot) private var sidebarSlot
    @Environment(\.sidebarField) private var sidebarField
    @Environment(\.filePicker) private var filePicker
    @Environment(\.keysRefocus) private var keysRefocus
    @Environment(\.treeHold) private var treeHold

    private static let liveDelay: Duration = .milliseconds(250)

    private var live: Bool { form == nil && node.bool("live") == true }

    private var drops: Bool { node.bool("drop") == true }

    private var lines: Int? {
        node.props["lines"]?.numberValue.flatMap { Int(exactly: $0) }.flatMap { $0 >= 2 ? min($0, 50) : nil }
    }

    var body: some View {
        field
            .focused($focused)
            .reportsFocus(.field, focused, slot: sidebarSlot.map { $0 + SidebarField.suffix })
            .onChange(of: router?.pending, initial: true) {
                guard let sidebarSlot else { return }
                if node.id == sidebarField, router?.take(sidebarSlot + SidebarField.suffix) == true { focused = true }
                if let id = node.id, router?.pending?.node != nil, router?.take(sidebarSlot, node: id) == true { focused = true }
            }
            .textFieldStyle(.plain)
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .padding(.horizontal, theme.spacing.m)
            .padding(.vertical, lines == nil ? 0 : theme.spacing.s)
            .frame(minWidth: 160, minHeight: theme.control, alignment: lines == nil ? .center : .topLeading)
            .fieldChrome(focused: focused || targeted, error: node.string("error"), dashed: drops)
            .overlay(alignment: .trailing) {
                if let treeHold, node.id == sidebarField {
                    HoldPill(hold: treeHold)
                        .padding(.trailing, theme.spacing.s)
                }
            }
            .modifier(FileDrop(active: drops, targeted: $targeted, drop: drop))
            .onSubmit(submit)
            .modifier(ExitToKeys(refocus: keysRefocus))
            .onChange(of: focused) { _, now in
                if now, node.id == sidebarField { treeHold?.land() }
                guard !now else { return }
                form?.flush()
                if !typed { sync(node.string("value")) }
            }
            .onChange(of: node.string("value"), initial: true) { _, value in
                if value == text { typed = false }
                if form?.autosave == true || live, focused, typed { return }
                sync(value)
            }
            .onChange(of: text, initial: true) { _, value in
                if lines == nil, value.contains(where: \.isNewline) {
                    text = value.split(whereSeparator: \.isNewline).joined(separator: " ")
                    return
                }
                guard let id = node.id else { return }
                form?.values[id] = .string(value)
            }
    }

    @ViewBuilder
    private var field: some View {
        let prompt = Text(node.string("placeholder") ?? "").foregroundStyle(theme.muted)
        if drops {
            HStack(spacing: theme.spacing.m) {
                Image(systemName: "paperclip")
                    .foregroundStyle(theme.muted)
                TextField("", text: binding, prompt: prompt)
            }
        } else if let lines {
            TextField("", text: binding, prompt: prompt, axis: .vertical).lineLimit(2...lines)
        } else {
            TextField("", text: binding, prompt: prompt)
        }
    }

    private func sync(_ value: String?) {
        text = node.id.flatMap { form?.draft($0)?.stringValue } ?? value ?? ""
        typed = false
    }

    private var binding: Binding<String> {
        Binding(get: { text }, set: { value in
            guard value != text else { return }
            if drops, text.isEmpty, value == "@", filePicker != nil { return pickFile() }
            typed = true
            text = value
            if let id = node.id { form?.touch(id) }
            form?.edited(after: FormState.typingDelay)
            if live { schedule(value) }
        })
    }

    private func schedule(_ value: String) {
        pending?.cancel()
        pending = Task { @MainActor in
            try? await Task.sleep(for: Self.liveDelay)
            guard !Task.isCancelled else { return }
            dispatch(node, .submit(.string(value)))
        }
    }

    private func submit() {
        pending?.cancel()
        guard let form, node.string("action") == nil else { return dispatch(node, .submit(.string(text))) }
        form.submit()
    }

    private func pickFile() {
        let node = node
        let dispatch = dispatch
        let focus = $focused
        filePicker?.pick { path in
            dispatch(node, .named("drop", .array([.string(path)])))
            focus.wrappedValue = true
        }
    }

    private func drop(_ urls: [URL]) -> Bool {
        let files = urls.filter(\.isFileURL).map { $0.standardizedFileURL.path }
        if !files.isEmpty {
            dispatch(node, .named("drop", .array(files.map(JSONValue.string))))
            return true
        }
        guard let link = urls.first(where: { ["http", "https"].contains($0.scheme?.lowercased()) }) else { return false }
        text = link.absoluteString
        typed = true
        focused = true
        return true
    }
}

struct ToggleNodeView: View {
    let node: SDUINode

    @State private var isOn = false
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    private var disabled: Bool { node.bool("disabled") == true }

    var body: some View {
        control
            .tint(theme.accent)
            .disabled(disabled)
            .onChange(of: node.bool("value"), initial: true) { _, value in
                isOn = node.id.flatMap { form?.draft($0)?.boolValue } ?? value ?? false
            }
            .onChange(of: isOn, initial: true) { _, value in
                guard let id = node.id else { return }
                form?.values[id] = .bool(value)
            }
    }

    @ViewBuilder private var control: some View {
        let label = node.string("label") ?? ""
        if node.string("style") == "checkbox" {
            Toggle(isOn: Binding(get: { isOn }, set: change)) {
                Text(label)
                    .font(theme.ui)
                    .foregroundStyle(disabled ? theme.muted : theme.text2)
                    .lineLimit(1)
            }
            .toggleStyle(.checkbox)
            .controlSize(.small)
            .fixedSize()
        } else if label.isEmpty {
            Toggle("", isOn: Binding(get: { isOn }, set: change))
                .toggleStyle(.switch)
                .controlSize(.mini)
                .labelsHidden()
        } else {
            HStack(spacing: theme.spacing.s) {
                Text(label)
                    .font(theme.ui)
                    .foregroundStyle(theme.text2)
                    .lineLimit(1)
                Toggle("", isOn: Binding(get: { isOn }, set: change))
                    .toggleStyle(.switch)
                    .controlSize(.mini)
                    .labelsHidden()
            }
        }
    }

    private func change(_ value: Bool) {
        isOn = value
        if let id = node.id {
            form?.values[id] = .bool(value)
            form?.touch(id)
        }
        form?.edited()
        if form == nil || node.string("action") != nil { dispatch(node, .toggle(value)) }
    }
}

struct RadioNodeView: View {
    let node: SDUINode

    @State private var hovering = false
    @State private var keyboardFocus = false
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    private var on: Bool { node.bool("on") == true }
    private var disabled: Bool { node.bool("disabled") == true }
    private var label: String { node.string("label") ?? "" }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            row
                .padding(.horizontal, theme.gutter)
            if !node.children.isEmpty {
                VStack(alignment: .leading, spacing: theme.spacing.s) {
                    SDUIChildren(node: node)
                }
                .padding(.leading, Self.mark + theme.spacing.s)
            }
        }
    }

    private static let mark: CGFloat = 14

    private var row: some View {
        Button(action: pick) {
            HStack(spacing: theme.spacing.s) {
                mark
                if !label.isEmpty {
                    Text(label)
                        .font(theme.ui)
                        .foregroundStyle(disabled ? theme.muted : theme.foreground)
                        .lineLimit(1)
                }
            }
            .frame(minHeight: theme.control + 6)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .focusable(!disabled, interactions: .activate)
        .focusEffectDisabled()
        .focused($focused)
        .onChange(of: focused) { _, now in
            keyboardFocus = now && !ButtonNodeView.mouseEvents.contains(NSApp.currentEvent?.type ?? .keyDown)
        }
        .onKeyPress(keys: [.space, .return]) { _ in
            pick()
            return .handled
        }
        .onHover { hovering = $0 }
        .accessibilityLabel(label)
        .accessibilityAddTraits(on ? .isSelected : [])
    }

    private var mark: some View {
        ZStack {
            if on {
                Circle().fill(theme.accent)
                Circle().fill(theme.onAccent).frame(width: 6, height: 6)
            } else {
                Circle().fill(hovering && !disabled ? theme.hover : .clear)
                Circle().strokeBorder(theme.borderStrong, lineWidth: 1)
            }
        }
        .frame(width: Self.mark, height: Self.mark)
        .overlay {
            if keyboardFocus {
                Circle().stroke(theme.accent, lineWidth: theme.focusRing).padding(-3)
            }
        }
        .opacity(disabled ? 0.5 : 1)
    }

    private func pick() {
        guard !disabled, !on, let value = node.string("value") else { return }
        dispatch(node, .change(value))
    }
}

struct SelectNodeView: View {
    let node: SDUINode

    @State private var value = ""
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    @State private var anchor = SelectMenuAnchor()

    private var options: [SDUIOption] { node.options() }

    private var label: String { options.first { $0.value == value }?.label ?? value }

    private var combo: KeyCombo? { node.string("shortcut").flatMap(KeyCombo.init) }

    var body: some View {
        HStack(spacing: theme.spacing.m) {
            ViewThatFits(in: .horizontal) {
                menu
                    .fixedSize()
                menu
            }
            if let key = combo?.label {
                Text(key)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .fixedSize()
            }
        }
        .background(SelectMenuAnchorView(anchor: anchor))
        .keyMonitor(active: combo != nil, handle: openFromKey)
        .help([label, combo?.label].compactMap { $0 }.joined(separator: " "))
        .testMenu("\(node.id ?? "select").menu", options.map { LayoutMenuItem(title: $0.label) })
        .padding(.horizontal, theme.spacing.m)
        .frame(height: theme.control)
        .fieldChrome(focused: false, error: node.string("error"))
        .onChange(of: node.string("value"), initial: true) { _, current in
            value = node.id.flatMap { form?.draft($0)?.stringValue } ?? current ?? options.first?.value ?? ""
        }
        .onChange(of: value, initial: true) { _, current in
            guard let id = node.id else { return }
            form?.values[id] = .string(current)
        }
    }

    private var menu: some View {
        Menu {
            ForEach(options) { option in
                Button(option.label) { choose(option.value) }
            }
        } label: {
            Text(label)
                .font(theme.ui)
                .lineLimit(1)
                .truncationMode(.tail)
        }
        .menuStyle(.borderlessButton)
        .environment(\.colorScheme, theme.isDark ? .dark : .light)
    }

    private func openFromKey(_ event: NSEvent) -> Bool {
        guard combo?.fires(on: event) == true, let view = anchor.view else { return false }
        let menu = NSMenu()
        for option in options {
            let item = SelectMenuItem(title: option.label) { choose(option.value) }
            item.state = option.value == value ? .on : .off
            menu.addItem(item)
        }
        let current = menu.items.first { $0.state == .on }
        DispatchQueue.main.async {
            menu.popUp(positioning: current, at: NSPoint(x: 0, y: view.isFlipped ? 0 : view.bounds.height), in: view)
        }
        return true
    }

    private func choose(_ option: String) {
        value = option
        if let id = node.id {
            form?.values[id] = .string(option)
            form?.touch(id)
        }
        form?.edited()
        if form == nil || node.string("action") != nil { dispatch(node, .change(option)) }
    }
}

final class SelectMenuAnchor {
    weak var view: NSView?
}

private struct SelectMenuAnchorView: NSViewRepresentable {
    let anchor: SelectMenuAnchor

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        anchor.view = view
        return view
    }

    func updateNSView(_ view: NSView, context: Context) {
        anchor.view = view
    }
}

private final class SelectMenuItem: NSMenuItem {
    private let pick: () -> Void

    init(title: String, pick: @escaping () -> Void) {
        self.pick = pick
        super.init(title: title, action: #selector(fire), keyEquivalent: "")
        target = self
    }

    required init(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    @objc private func fire() {
        pick()
    }
}

extension View {
    func fieldChrome(focused: Bool, error: String? = nil, dashed: Bool = false) -> some View {
        modifier(FieldChrome(focused: focused, error: error.flatMap { $0.isEmpty ? nil : $0 }, dashed: dashed))
    }
}

private struct FieldChrome: ViewModifier {
    let focused: Bool
    let error: String?
    var dashed = false

    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        let tone = error == nil ? theme.accent : theme.danger
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            content
                .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.background))
                .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(focused || error != nil ? tone : theme.borderStrong, style: StrokeStyle(lineWidth: 1, dash: dashed && !focused && error == nil ? [4, 3] : [])))
                .background(RoundedRectangle(cornerRadius: theme.radius.m).stroke(focused ? theme.toneBg(tone) : .clear, lineWidth: 6))
            if let error {
                Text(error)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.danger)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .help(error)
            }
        }
    }
}

@MainActor
enum MenuKeys {
    static func claims(_ menu: NSMenu, _ event: NSEvent) -> Bool {
        item(in: menu, matching: event) != nil
    }

    static func item(in menu: NSMenu, matching event: NSEvent) -> NSMenuItem? {
        let flags = event.modifierFlags.intersection([.command, .control, .option, .shift])
        let key = (event.charactersIgnoringModifiers?.lowercased() ?? "").replacingOccurrences(of: "\u{7f}", with: "\u{8}")
        for item in menu.items {
            if let found = item.submenu.flatMap({ Self.item(in: $0, matching: event) }) { return found }
            guard !item.keyEquivalent.isEmpty else { continue }
            if item.keyEquivalentModifierMask.contains(.function), !event.modifierFlags.contains(.function) { continue }
            var mask = item.keyEquivalentModifierMask.intersection([.command, .control, .option, .shift])
            if item.keyEquivalent != item.keyEquivalent.lowercased() { mask.insert(.shift) }
            if item.keyEquivalent.lowercased() == key, mask == flags { return item }
        }
        return nil
    }
}

private struct ExitToKeys: ViewModifier {
    let refocus: KeysRefocus?

    func body(content: Content) -> some View {
        if let refocus {
            content.onExitCommand { refocus.take() }
        } else {
            content
        }
    }
}
