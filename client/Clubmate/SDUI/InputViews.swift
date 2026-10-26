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
            HStack(spacing: 5) {
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
        guard let form else { return dispatch(node, .click) }
        form.submit()
    }
}

struct TextFieldNodeView: View {
    let node: SDUINode

    @State private var text = ""
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.theme) private var theme

    var body: some View {
        TextField("", text: $text, prompt: Text(node.string("placeholder") ?? "").foregroundStyle(theme.muted))
            .textFieldStyle(.plain)
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .padding(.horizontal, 6)
            .frame(minWidth: 160, minHeight: theme.rowHeight)
            .overlay(RoundedRectangle(cornerRadius: 3).stroke(theme.border, lineWidth: 1))
            .onSubmit(submit)
            .onChange(of: node.string("value"), initial: true) { _, value in
                text = value ?? ""
            }
            .onChange(of: text, initial: true) { _, value in
                guard let id = node.id else { return }
                form?.values[id] = .string(value)
            }
    }

    private func submit() {
        guard let form else { return dispatch(node, .submit(.string(text))) }
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
        if form == nil { dispatch(node, .toggle(value)) }
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
        .padding(.horizontal, 6)
        .frame(height: theme.rowHeight)
        .overlay(RoundedRectangle(cornerRadius: 3).stroke(theme.border, lineWidth: 1))
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
        if form == nil { dispatch(node, .change(option)) }
    }
}
