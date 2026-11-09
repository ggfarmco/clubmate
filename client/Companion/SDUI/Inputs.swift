import SwiftUI
import UIKit

import CompanionKit

@MainActor
enum SDUIPress {
    static func run(_ button: SDUINode, context: SDUIContext, form: SDUIFormState?) {
        if let copy = button.string("copy") {
            UIPasteboard.general.string = copy
        } else if let form, button.string("action") == nil {
            form.submit()
        } else if let form, button.bool("withForm") == true {
            context.run { await $0.fire(button, .submit(form.collected)) }
        } else {
            context.run { await $0.fire(button, .click) }
        }
    }
}

struct ButtonNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var confirming = false
    @State private var pressed = 0
    @Environment(\.sduiForm) private var form

    private var loading: String? {
        if let verb = node.string("loading"), !verb.isEmpty { return verb }
        if form != nil, node.string("action") == nil, form?.saving == true { return UIFormSave.verb }
        return nil
    }

    private var disabled: Bool {
        node.bool("disabled") == true || node.string("loading")?.isEmpty == false
    }

    var body: some View {
        let label = node.string("label") ?? ""
        Button {
            if node.confirmation() != nil { confirming = true } else { press() }
        } label: {
            SDUIButtonLabel(label: label, icon: node.string("icon"), variant: node.string("variant"), iconOnly: node.bool("iconOnly") == true && node.string("icon") != nil, loading: loading)
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled ? 0.5 : 1)
        .accessibilityLabel(label)
        .sensoryFeedback(.success, trigger: pressed)
        .alert(node.confirmation() ?? "", isPresented: $confirming) {
            Button(node.confirmActionTitle(), role: node.string("variant") == "danger" ? .destructive : nil) { press() }
            Button("Cancel", role: .cancel) {}
        }
    }

    private func press() {
        if node.string("copy") != nil { pressed += 1 }
        SDUIPress.run(node, context: context, form: form)
    }
}

struct SDUIButtonLabel: View {
    let label: String
    let icon: String?
    let variant: String?
    let iconOnly: Bool
    let loading: String?

    private var foreground: Color {
        switch variant {
        case "primary": .onAccent
        case "danger": .dangerInk
        default: .primary
        }
    }

    private var background: Color {
        switch variant {
        case "primary": .accentColor
        case "secondary", "danger": .surface2
        default: .clear
        }
    }

    var body: some View {
        if iconOnly, let icon {
            Image(systemName: icon)
                .font(.body.weight(.semibold))
                .foregroundStyle(foreground)
                .frame(width: 44, height: 44)
                .background(variant == nil || variant == "ghost" ? Color.surface2 : background, in: .circle)
                .contentShape(.circle)
        } else {
            HStack(spacing: 6) {
                if loading != nil {
                    ProgressView().controlSize(.small)
                } else if let icon {
                    Image(systemName: icon)
                }
                Text(loading ?? label)
                    .lineLimit(1)
            }
            .font(variant == "primary" ? .headline : .body.weight(.medium))
            .foregroundStyle(foreground)
            .padding(.horizontal, background == .clear ? 4 : 18)
            .frame(minHeight: 44)
            .background(background, in: .capsule)
            .contentShape(.capsule)
        }
    }
}

struct TextFieldNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var text = ""
    @State private var pending: Task<Void, Never>?
    @FocusState private var focused: Bool
    @Environment(\.sduiForm) private var form
    @Environment(\.sduiFormRow) private var formRow
    @Environment(\.sduiInset) private var inset

    private static let liveDelay = Duration.milliseconds(250)

    private var live: Bool { form == nil && node.bool("live") == true }

    private var lines: Int? {
        node.props["lines"]?.numberValue.flatMap { Int(exactly: $0) }.flatMap { $0 >= 2 ? min($0, 50) : nil }
    }

    private var error: String? {
        node.string("error").flatMap { $0.isEmpty ? nil : $0 }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            field
                .focused($focused)
                .onSubmit(submit)
                .padding(.horizontal, formRow ? 0 : 14)
                .padding(.vertical, formRow ? 4 : 10)
                .frame(minHeight: formRow ? 32 : 44)
                .background {
                    if !formRow { RoundedRectangle(cornerRadius: 12).fill(Color.surface2) }
                }
                .overlay {
                    if error != nil, !formRow { RoundedRectangle(cornerRadius: 12).strokeBorder(Color.danger, lineWidth: 1) }
                }
            if let error {
                Text(error)
                    .font(.footnote)
                    .foregroundStyle(Color.dangerInk)
            }
        }
        .padding(.horizontal, inset)
        .onChange(of: focused) { _, now in
            if !now { form?.flush() }
        }
        .onChange(of: node.string("value"), initial: true) { _, value in
            if form?.autosave == true || live, focused { return }
            text = value ?? ""
        }
        .onChange(of: text, initial: true) { _, value in
            guard let id = node.id else { return }
            form?.values[id] = .string(value)
        }
    }

    @ViewBuilder
    private var field: some View {
        let placeholder = node.string("placeholder") ?? ""
        if let lines {
            TextField(placeholder, text: typed, axis: .vertical).lineLimit(2...lines)
        } else {
            TextField(placeholder, text: typed)
        }
    }

    private var typed: Binding<String> {
        Binding(get: { text }, set: { value in
            guard value != text else { return }
            text = value
            form?.typed()
            if live { schedule(value) }
        })
    }

    private func schedule(_ value: String) {
        pending?.cancel()
        pending = Task {
            try? await Task.sleep(for: Self.liveDelay)
            guard !Task.isCancelled else { return }
            await context.fire(node, .submit(.string(value)))
        }
    }

    private func submit() {
        pending?.cancel()
        if let form, node.string("action") == nil { return form.submit() }
        let value = text
        context.run { await $0.fire(node, .submit(.string(value))) }
    }
}

struct FolderFieldNode: View {
    let node: SDUINode
    @Environment(\.sduiForm) private var form
    @Environment(\.sduiInset) private var inset

    var body: some View {
        let values = node.values()
        VStack(alignment: .leading, spacing: 4) {
            Text(node.string("label") ?? "")
            ForEach(values, id: \.self) { path in
                Label(path, systemImage: "folder")
                    .font(.footnote.monospaced())
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            Text("Choose folders on the Mac")
                .font(.footnote)
                .foregroundStyle(.tertiary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, inset)
        .onChange(of: values, initial: true) { _, paths in
            guard let id = node.id else { return }
            form?.values[id] = .array(paths.map(JSONValue.string))
        }
    }
}

struct ToggleNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var isOn = false
    @Environment(\.sduiForm) private var form

    var body: some View {
        Toggle("", isOn: Binding(get: { isOn }, set: change))
            .labelsHidden()
            .tint(.accentColor)
            .fixedSize()
            .disabled(node.bool("disabled") == true)
            .accessibilityIdentifier(node.id ?? "toggle")
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
        let form = form
        context.run { await $0.changed(node, .toggle(value), in: form) }
    }
}

struct SelectNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var value = ""
    @Environment(\.sduiForm) private var form
    @Environment(\.sduiFormRow) private var formRow
    @Environment(\.sduiInset) private var inset

    private var options: [SDUIOption] { node.options() }

    var body: some View {
        let label = options.first { $0.value == value }?.label ?? value
        VStack(alignment: .leading, spacing: 4) {
            Menu {
                Picker(selection: Binding(get: { value }, set: choose)) {
                    ForEach(options) { option in
                        Text(option.label).tag(option.value)
                    }
                } label: {
                    EmptyView()
                }
            } label: {
                HStack(spacing: 6) {
                    Text(label)
                        .lineLimit(1)
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.footnote.weight(.semibold))
                }
                .padding(.horizontal, formRow ? 0 : 14)
                .frame(minHeight: 44)
                .background {
                    if !formRow { Capsule().fill(Color.surface2) }
                }
                .overlay {
                    if node.string("error")?.isEmpty == false, !formRow { Capsule().strokeBorder(Color.danger, lineWidth: 1) }
                }
                .contentShape(.rect)
            }
            .tint(.primary)
            if let error = node.string("error"), !error.isEmpty {
                Text(error)
                    .font(.footnote)
                    .foregroundStyle(Color.dangerInk)
            }
        }
        .padding(.horizontal, inset)
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
        let form = form
        context.run { await $0.changed(node, .change(option), in: form) }
    }
}
