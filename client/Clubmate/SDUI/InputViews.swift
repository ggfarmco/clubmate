import AppKit
import SwiftUI

import ClubmateKit

struct ButtonNodeView: View {
    let node: SDUINode

    @State private var hovering = false
    @State private var keyboardFocus = false
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme
    @Environment(\.buttonKeyHint) private var keyHint

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
            .keyMonitor(active: combo != nil && !disabled, handle: press.handle)
            .help(iconOnly ? tooltip : "")
            .accessibilityLabel(node.string("label") ?? "")
    }

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
            if keyboardFocus && focused {
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
                    .foregroundStyle(variant == "primary" ? theme.onAccent : theme.muted)
            }
        }
    }

    private var press: ButtonPress { ButtonPress(node: node, dispatch: dispatch, form: form) }

    private var keyLabel: String? { combo?.label ?? keyHint }

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

@MainActor
struct ButtonPress {
    let node: SDUINode
    let dispatch: SDUIDispatch
    let form: FormState?

    var combo: KeyCombo? { node.string("shortcut").flatMap(KeyCombo.init) }
    var loading: String? { node.string("loading").flatMap { $0.isEmpty ? nil : $0 } }
    var disabled: Bool { node.bool("disabled") == true || loading != nil }
    var saving: String? { form?.saving == true && node.string("action") == nil ? UIFormSave.verb : nil }

    func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        guard let combo, combo.matches(keyCode: event.keyCode, command: flags.contains(.command), control: flags.contains(.control), option: flags.contains(.option), shift: flags.contains(.shift)) else { return false }
        if combo.bare, event.window?.firstResponder is NSTextView { return false }
        if NSApp.mainMenu.map({ MenuKeys.claims($0, event) }) == true { return false }
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
        guard let form, node.string("action") == nil else { return dispatch(node, .click) }
        form.submit()
    }
}

@MainActor
enum Clipboard {
    static let testName = NSPasteboard.Name("com.clubmate.test")

    static func copy(_ text: String) {
        let board = TestMode.current ? NSPasteboard(name: testName) : .general
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
            label.frame.size = NSSize(width: bodySize.width, height: label.fittingSize.height)
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
        }
        return container
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

struct FolderFieldNodeView: View {
    let node: SDUINode

    @State private var paths: [String] = []
    @State private var hovering = false
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            Button(action: pick) {
                HStack(spacing: theme.spacing.m) {
                    Image(systemName: "folder")
                    Text(node.string("label") ?? "")
                }
                .font(theme.ui)
                .foregroundStyle(theme.foreground)
                .padding(.horizontal, theme.gutter)
                .frame(height: theme.rowHeight)
                .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(hovering ? theme.hover : .clear))
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .onHover { hovering = $0 }
            ForEach(paths, id: \.self) { path in
                Text((path as NSString).abbreviatingWithTildeInPath)
                    .font(theme.uiSmall.monospaced())
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .padding(.horizontal, theme.gutter * 2)
            }
        }
        .onChange(of: node.values(), initial: true) { _, values in
            paths = values
        }
        .onChange(of: paths, initial: true) { _, value in
            guard let id = node.id else { return }
            form?.values[id] = .array(value.map(JSONValue.string))
        }
    }

    private func pick() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = true
        panel.showsHiddenFiles = true
        panel.prompt = "Choose"
        guard !TestMode.current, panel.runModal() == .OK else { return }
        paths = panel.urls.map(\.path)
        if let id = node.id { form?.values[id] = .array(paths.map(JSONValue.string)) }
        form?.edited()
        if form == nil || node.string("action") != nil { dispatch(node, .pick(paths)) }
    }
}

struct TextFieldNodeView: View {
    let node: SDUINode

    @State private var text = ""
    @State private var pending: Task<Void, Never>?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme
    @Environment(\.focusRouter) private var router
    @Environment(\.sidebarSlot) private var sidebarSlot

    private static let liveDelay: Duration = .milliseconds(250)

    private var live: Bool { form == nil && node.bool("live") == true }

    private var lines: Int? {
        node.props["lines"]?.numberValue.flatMap { Int(exactly: $0) }.flatMap { $0 >= 2 ? min($0, 50) : nil }
    }

    var body: some View {
        field
            .focused($focused)
            .reportsFocus(.field, focused)
            .onChange(of: router?.pending, initial: true) {
                guard let sidebarSlot, router?.take(sidebarSlot + SidebarField.suffix) == true else { return }
                focused = true
            }
            .textFieldStyle(.plain)
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .padding(.horizontal, theme.spacing.m)
            .padding(.vertical, lines == nil ? 0 : theme.spacing.s)
            .frame(minWidth: 160, minHeight: theme.control, alignment: lines == nil ? .center : .topLeading)
            .fieldChrome(focused: focused, error: node.string("error"))
            .padding(.horizontal, theme.gutter)
            .onSubmit(submit)
            .onChange(of: focused) { _, now in
                if !now { form?.flush() }
            }
            .onChange(of: node.string("value"), initial: true) { _, value in
                if form?.autosave == true || live, focused { return }
                text = value ?? ""
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
        if let lines {
            TextField("", text: typed, prompt: prompt, axis: .vertical).lineLimit(2...lines)
        } else {
            TextField("", text: typed, prompt: prompt)
        }
    }

    private var typed: Binding<String> {
        Binding(get: { text }, set: { value in
            guard value != text else { return }
            text = value
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
}

struct ToggleNodeView: View {
    let node: SDUINode

    @State private var isOn = false
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    var body: some View {
        Toggle("", isOn: Binding(get: { isOn }, set: change))
            .toggleStyle(.switch)
            .controlSize(.mini)
            .labelsHidden()
            .tint(theme.accent)
            .padding(.horizontal, theme.gutter)
            .onChange(of: node.bool("value"), initial: true) { _, value in
                isOn = value ?? false
            }
            .onChange(of: isOn, initial: true) { _, value in
                guard let id = node.id else { return }
                form?.values[id] = .bool(value)
            }
    }

    private func change(_ value: Bool) {
        isOn = value
        if let id = node.id { form?.values[id] = .bool(value) }
        form?.edited()
        if form == nil || node.string("action") != nil { dispatch(node, .toggle(value)) }
    }
}

struct SelectNodeView: View {
    let node: SDUINode

    @State private var value = ""
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    private var options: [SDUIOption] { node.options() }

    private var label: String { options.first { $0.value == value }?.label ?? value }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            menu
                .fixedSize()
            menu
        }
        .help(label)
        .testMenu("\(node.id ?? "select").menu", options.map { LayoutMenuItem(title: $0.label) })
        .padding(.horizontal, theme.spacing.m)
        .frame(height: theme.control)
        .fieldChrome(focused: false, error: node.string("error"))
        .padding(.horizontal, theme.gutter)
        .onChange(of: node.string("value"), initial: true) { _, current in
            value = current ?? options.first?.value ?? ""
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

    private func choose(_ option: String) {
        value = option
        if let id = node.id { form?.values[id] = .string(option) }
        form?.edited()
        if form == nil || node.string("action") != nil { dispatch(node, .change(option)) }
    }
}

extension View {
    func fieldChrome(focused: Bool, error: String? = nil) -> some View {
        modifier(FieldChrome(focused: focused, error: error.flatMap { $0.isEmpty ? nil : $0 }))
    }
}

private struct FieldChrome: ViewModifier {
    let focused: Bool
    let error: String?

    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        let tone = error == nil ? theme.accent : theme.danger
        VStack(alignment: .leading, spacing: theme.spacing.xs) {
            content
                .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.background))
                .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(focused || error != nil ? tone : theme.borderStrong, lineWidth: 1))
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
        let flags = event.modifierFlags.intersection([.command, .control, .option, .shift])
        let key = event.charactersIgnoringModifiers?.lowercased() ?? ""
        return menu.items.contains { item in
            if let sub = item.submenu, claims(sub, event) { return true }
            guard !item.keyEquivalent.isEmpty else { return false }
            var mask = item.keyEquivalentModifierMask.intersection([.command, .control, .option, .shift])
            if item.keyEquivalent != item.keyEquivalent.lowercased() { mask.insert(.shift) }
            return item.keyEquivalent.lowercased() == key && mask == flags
        }
    }
}
