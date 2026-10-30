import AppKit
import SwiftUI

import ClubmateKit

struct FilePicker {
    let pick: @MainActor (@escaping @MainActor (String) -> Void) -> Void
}

private struct FilePickerKey: EnvironmentKey {
    static let defaultValue: FilePicker? = nil
}

extension EnvironmentValues {
    var filePicker: FilePicker? {
        get { self[FilePickerKey.self] }
        set { self[FilePickerKey.self] = newValue }
    }
}

@MainActor
private enum ComposerFocus {
    static var afterSend: String?

    static func session(of id: String?) -> String? {
        id.flatMap { $0.split(separator: "/", maxSplits: 1).first.map(String.init) }
    }
}

struct ComposerNodeView: View {
    let node: SDUINode

    @State private var model = ComposerModel()
    @State private var selection: TextSelection?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.filePicker) private var filePicker
    @Environment(\.theme) private var theme

    private var running: Bool { node.bool("running") == true }

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            if !model.suggestions.isEmpty { suggestions }
            editor
            controls
        }
        .padding(.horizontal, theme.gutter * 2)
        .padding(.vertical, theme.gutter + theme.spacing.s)
        .overlay(alignment: .top) { theme.border.frame(height: 1) }
        .keyMonitor(active: focused, handle: handle)
        .onChange(of: node.composerCommands(), initial: true) { _, commands in model.commands = commands }
        .onChange(of: running, initial: true) { _, running in model.running = running }
        .onAppear {
            guard let session = ComposerFocus.afterSend, session == ComposerFocus.session(of: node.id) else { return }
            ComposerFocus.afterSend = nil
            focused = true
        }
    }

    private var editor: some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            let attachments = node.attachments()
            if !attachments.isEmpty {
                ScrollView(.horizontal) {
                    HStack(spacing: theme.spacing.m) {
                        ForEach(attachments) { attachment in chip(attachment) }
                    }
                }
                .scrollIndicators(.never)
            }
            Text(model.text.isEmpty ? " " : model.text + " ")
                .font(theme.ui)
                .lineLimit(10)
                .frame(maxWidth: .infinity, minHeight: theme.rowHeight * 2, alignment: .topLeading)
                .padding(.horizontal, theme.spacing.m)
                .opacity(0)
                .overlay {
                    TextEditor(text: $model.text, selection: $selection)
                        .font(theme.ui)
                        .foregroundStyle(theme.foreground)
                        .scrollContentBackground(.hidden)
                        .focused($focused)
                }
                .overlay(alignment: .topLeading) {
                    if model.text.isEmpty {
                        Text(node.string("placeholder") ?? "")
                            .font(theme.ui)
                            .foregroundStyle(theme.muted)
                            .padding(.horizontal, theme.spacing.m)
                            .allowsHitTesting(false)
                    }
                }
        }
        .padding(theme.gutter)
        .background(RoundedRectangle(cornerRadius: 8).fill(theme.foreground.opacity(0.04)))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(focused ? theme.accent.opacity(0.6) : theme.border, lineWidth: 1))
        .contentShape(Rectangle())
        .onTapGesture { focused = true }
    }

    private func chip(_ attachment: SDUIAttachment) -> some View {
        HStack(spacing: theme.spacing.s) {
            Text(attachment.label)
                .lineLimit(1)
                .truncationMode(.middle)
            if let key = attachment.key {
                Button {
                    dispatch(node, .named("detach", .object(["id": .string(key)])))
                } label: {
                    Image(systemName: "xmark")
                        .font(theme.glyph)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("Remove")
            }
        }
        .font(theme.uiSmall)
        .foregroundStyle(theme.foreground)
        .padding(theme.spacing.chip)
        .overlay(RoundedRectangle(cornerRadius: 5).stroke(theme.border, lineWidth: 1))
    }

    private var suggestions: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(model.suggestions.enumerated()), id: \.element.id) { index, command in
                HStack(spacing: theme.gutter) {
                    Text("/" + command.name)
                        .fontWeight(.semibold)
                    Text(command.description)
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                    Spacer(minLength: theme.gutter)
                    if !command.hint.isEmpty {
                        Text(command.hint)
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.muted)
                            .lineLimit(1)
                    }
                }
                .font(theme.ui)
                .foregroundStyle(theme.foreground)
                .padding(.horizontal, theme.gutter)
                .frame(height: theme.rowHeight + theme.spacing.xs)
                .background(index == model.highlighted ? theme.accent.opacity(0.25) : .clear, ignoresSafeAreaEdges: [])
                .contentShape(Rectangle())
                .onTapGesture {
                    model.choose(command)
                    focused = true
                }
            }
        }
        .padding(.vertical, theme.spacing.s)
        .agentCard()
    }

    private var controls: some View {
        HStack(spacing: theme.gutter) {
            if let agent = node.string("agent") {
                Text(agent)
                    .foregroundStyle(theme.foreground)
            }
            let modes = node.modes()
            if !modes.isEmpty {
                Menu {
                    ForEach(modes) { mode in
                        Button(mode.label) { dispatch(node, .named("mode", .object(["mode": .string(mode.value)]))) }
                    }
                } label: {
                    Text(modes.first { $0.value == node.string("mode") }?.label ?? node.string("mode") ?? "Mode")
                        .font(theme.ui)
                }
                .menuStyle(.borderlessButton)
                .fixedSize()
                .help("Permission mode")
            }
            pickers
            attachMenu
            Text(running ? "Esc stop" : "⌘↩ send")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
            Spacer(minLength: theme.gutter)
            if running {
                CardButton(label: "Stop", icon: "stop.fill", tone: .danger) { dispatch(node, .named("stop", .null)) }
            } else {
                CardButton(label: "Send", icon: "paperplane.fill", tone: .primary, disabled: model.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) {
                    if case .send(let text) = model.handle(.send) { send(text) }
                }
            }
        }
        .font(theme.ui)
    }

    private var pickers: some View {
        ForEach(node.pickers()) { picker in
            Menu {
                ForEach(picker.options) { option in
                    Button(option.label) {
                        dispatch(node, .named("config", .object(["id": .string(picker.id), "value": .string(option.value)])))
                    }
                }
            } label: {
                Text(picker.label)
                    .font(theme.ui)
            }
            .menuStyle(.borderlessButton)
            .fixedSize()
            .help(picker.help ?? picker.label)
        }
    }

    @ViewBuilder private var attachMenu: some View {
        let kinds = node.attachKinds()
        if !kinds.isEmpty {
            Menu {
                if kinds.contains("file"), filePicker != nil {
                    Button("File…") { pickFile() }
                }
                if kinds.contains("selection") {
                    Button("Editor Selection") { dispatch(node, .named("attachSelection", .null)) }
                }
            } label: {
                Image(systemName: "paperclip")
            }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .fixedSize()
            .help("Attach")
        }
    }

    private func send(_ text: String) {
        let session = ComposerFocus.session(of: node.id)
        ComposerFocus.afterSend = session
        dispatch(node, .named("send", .object(["text": .string(text)]))) { sent in
            if !sent, ComposerFocus.afterSend == session { ComposerFocus.afterSend = nil }
        }
    }

    private var caret: String.Index? {
        guard case .selection(let range)? = selection?.indices else { return nil }
        return range.lowerBound
    }

    private func pickFile() {
        guard let filePicker else { return }
        let node = node
        let dispatch = dispatch
        let focus = $focused
        filePicker.pick { path in
            dispatch(node, .named("attachFile", .object(["path": .string(path)])))
            focus.wrappedValue = true
        }
    }

    private func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        guard let key = ComposerKey(
            keyCode: event.keyCode,
            characters: event.characters,
            command: flags.contains(.command),
            control: flags.contains(.control),
            option: flags.contains(.option),
            shift: flags.contains(.shift)
        ) else { return false }
        if key == .attach, filePicker == nil || !node.attachKinds().contains("file") { return false }
        switch model.handle(key, before: model.character(before: caret)) {
        case .send(let text): send(text)
        case .stop: dispatch(node, .named("stop", .null))
        case .attachFile: pickFile()
        case .handled: break
        case nil: return false
        }
        return true
    }
}
