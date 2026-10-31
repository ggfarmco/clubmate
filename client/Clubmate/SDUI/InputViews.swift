import AppKit
import SwiftUI

import ClubmateKit

struct ButtonNodeView: View {
    let node: SDUINode

    @State private var hovering = false
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: click) {
            HStack(spacing: theme.spacing.m) {
                if let icon = node.string("icon") {
                    Image(systemName: SymbolName.resolve(icon))
                }
                Text(node.string("label") ?? "")
            }
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .padding(.horizontal, theme.gutter)
            .frame(height: theme.rowHeight)
            .background(RoundedRectangle(cornerRadius: 3).fill(hovering ? theme.foreground.opacity(0.08) : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }

    private func click() {
        guard let message = node.confirmation() else { return perform() }
        Task {
            if await Confirmation.ask(message, action: node.confirmActionTitle()) { perform() }
        }
    }

    private func perform() {
        guard let form, node.string("action") == nil else { return dispatch(node, .click) }
        form.submit()
    }
}

@MainActor
enum Confirmation {
    private static let bodySize = NSSize(width: 560, height: 300)

    static func ask(_ request: ConfirmRequest, font: NSFont, window: NSWindow?, expiry: ConfirmExpiry?) async -> Bool {
        await ask(request.message, detail: request.detail, body: request.displayBody, font: font, action: "Allow", cancel: "Deny", destructive: true, window: window, expiry: expiry)
    }

    static func ask(_ message: String, detail: String = "", body: String? = nil, font: NSFont = .monospacedSystemFont(ofSize: NSFont.smallSystemFontSize, weight: .regular), action: String, cancel: String = "Cancel", destructive: Bool = false, window: NSWindow? = nil, expiry: ConfirmExpiry? = nil) async -> Bool {
        let alert = NSAlert()
        alert.messageText = message
        alert.informativeText = detail
        if let body { alert.accessoryView = bodyView(body, font: font) }
        let confirm = alert.addButton(withTitle: action)
        let dismiss = alert.addButton(withTitle: cancel)
        if destructive {
            confirm.hasDestructiveAction = true
            confirm.keyEquivalent = ""
            confirm.refusesFirstResponder = true
            dismiss.keyEquivalent = "\r"
            alert.window.initialFirstResponder = dismiss
        }
        let escape = destructive ? NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            guard event.window === alert.window, event.charactersIgnoringModifiers == "\u{1b}" else { return event }
            dismiss.performClick(nil)
            return nil
        } : nil
        defer { escape.map(NSEvent.removeMonitor) }
        guard let window = window ?? NSApp.promptWindow else {
            if TestMode.current { return false }
            expiry?.onExpire { NSApp.abortModal() }
            return alert.runModal() == .alertFirstButtonReturn
        }
        expiry?.onExpire { [weak window] in window?.endSheet(alert.window, returnCode: .alertSecondButtonReturn) }
        return await alert.beginSheetModal(for: window) == .alertFirstButtonReturn
    }

    private static func bodyView(_ text: String, font: NSFont) -> NSView {
        let scroll = NSScrollView(frame: NSRect(origin: .zero, size: bodySize))
        scroll.borderType = .bezelBorder
        scroll.hasVerticalScroller = true
        let view = NSTextView(usingTextLayoutManager: false)
        view.frame = NSRect(origin: .zero, size: scroll.contentSize)
        view.autoresizingMask = .width
        view.isEditable = false
        view.isSelectable = false
        view.isRichText = false
        view.font = font
        view.minSize = NSSize(width: 0, height: scroll.contentSize.height)
        view.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        view.isVerticallyResizable = true
        view.textContainer?.widthTracksTextView = true
        scroll.documentView = view
        view.string = text
        return scroll
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
                .background(RoundedRectangle(cornerRadius: 3).fill(hovering ? theme.foreground.opacity(0.08) : .clear))
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
        if form == nil || node.string("action") != nil { dispatch(node, .pick(paths)) }
    }
}

struct TextFieldNodeView: View {
    let node: SDUINode

    @State private var text = ""
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme
    @Environment(\.focusRouter) private var router
    @Environment(\.sidebarSlot) private var sidebarSlot

    private var lines: Int? {
        node.props["lines"]?.numberValue.flatMap { Int(exactly: $0) }.flatMap { $0 >= 2 ? min($0, 50) : nil }
    }

    var body: some View {
        field
            .focused($focused)
            .onChange(of: router?.pending, initial: true) {
                guard let sidebarSlot, router?.take(sidebarSlot + SidebarField.suffix) == true else { return }
                focused = true
            }
            .textFieldStyle(.plain)
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .padding(.horizontal, theme.spacing.m)
            .padding(.vertical, lines == nil ? 0 : theme.spacing.s)
            .frame(minWidth: 160, minHeight: theme.rowHeight, alignment: lines == nil ? .center : .topLeading)
            .overlay(RoundedRectangle(cornerRadius: 3).stroke(theme.border, lineWidth: 1))
            .padding(.horizontal, theme.gutter)
            .onSubmit(submit)
            .onChange(of: node.string("value"), initial: true) { _, value in
                text = value ?? ""
            }
            .onChange(of: text, initial: true) { _, value in
                guard let id = node.id else { return }
                form?.values[id] = .string(value)
            }
    }

    @ViewBuilder
    private var field: some View {
        let prompt = Text(node.string("placeholder") ?? "").foregroundStyle(theme.muted)
        if let lines {
            TextField("", text: $text, prompt: prompt, axis: .vertical).lineLimit(2...lines)
        } else {
            TextField("", text: $text, prompt: prompt)
        }
    }

    private func submit() {
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

    var body: some View {
        Menu {
            ForEach(options) { option in
                Button(option.label) { choose(option.value) }
            }
        } label: {
            Text(options.first { $0.value == value }?.label ?? value)
                .font(theme.ui)
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
        .testMenu("\(node.id ?? "select").menu", options.map { LayoutMenuItem(title: $0.label) })
        .padding(.horizontal, theme.spacing.m)
        .frame(height: theme.rowHeight)
        .overlay(RoundedRectangle(cornerRadius: 3).stroke(theme.border, lineWidth: 1))
        .padding(.horizontal, theme.gutter)
        .onChange(of: node.string("value"), initial: true) { _, current in
            value = current ?? options.first?.value ?? ""
        }
        .onChange(of: value, initial: true) { _, current in
            guard let id = node.id else { return }
            form?.values[id] = .string(current)
        }
    }

    private func choose(_ option: String) {
        value = option
        if form == nil || node.string("action") != nil { dispatch(node, .change(option)) }
    }
}
