import SwiftUI

import CompanionKit

struct SessionKeyActions {
    let enabled: Bool
    let pending: PermissionRequest?
    let answer: (PermissionRequest, PermissionOption) -> Void
    let send: () -> Void
}

extension FocusedValues {
    @Entry var sessionKeys: SessionKeyActions?
}

extension View {
    func sessionKeys(enabled: Bool, pending: PermissionRequest?, answer: @escaping (PermissionRequest, PermissionOption) -> Void, send: @escaping () -> Void) -> some View {
        modifier(SessionKeysModifier(enabled: enabled, pending: pending, answer: answer, send: send))
    }
}

struct SessionKeysModifier: ViewModifier {
    let enabled: Bool
    let pending: PermissionRequest?
    let answer: (PermissionRequest, PermissionOption) -> Void
    let send: () -> Void
    @State private var answered: String?
    @State private var armed: String?

    func body(content: Content) -> some View {
        content
            .focusedSceneValue(\.sessionKeys, SessionKeyActions(enabled: enabled, pending: pending?.id == answered || pending?.id != armed ? nil : pending, answer: { request, option in
                answered = request.id
                answer(request, option)
            }, send: send))
            .task(id: pending?.id) {
                armed = nil
                try? await Task.sleep(for: .milliseconds(600))
                armed = pending?.id
            }
    }
}

struct SessionCommands: Commands {
    @FocusedValue(\.sessionKeys) private var keys

    var body: some Commands {
        CommandMenu("Session") {
            Button("Send") { keys?.send() }
                .keyboardShortcut(.return, modifiers: .command)
                .disabled(keys?.enabled != true)
            if let keys, let pending = keys.pending {
                let kinds = pending.options.map(\.kind)
                ForEach(1...PermissionKeys.count, id: \.self) { digit in
                    if let index = PermissionKeys.option(digit, kinds: kinds) {
                        let option = pending.options[index]
                        Button(option.name) { keys.answer(pending, option) }
                            .keyboardShortcut(KeyEquivalent(Character(String(digit))), modifiers: .option)
                            .disabled(!keys.enabled)
                    }
                }
            }
        }
    }
}
