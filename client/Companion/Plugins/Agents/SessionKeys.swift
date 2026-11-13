import SwiftUI

import CompanionKit

struct AlwaysAsk: Equatable {
    let option: String
    let nonce = UUID()
}

struct KeyedAction {
    let label: String
    let run: () -> Void
}

struct SessionKeyActions {
    let enabled: Bool
    let answers: [Int: KeyedAction]
    let send: KeyedAction?
}

extension FocusedValues {
    @Entry var sessionKeys: SessionKeyActions?
}

extension View {
    func sessionKeys(enabled: Bool, pending: PermissionRequest?, answer: @escaping (PermissionRequest, String) -> Void, confirmAlways: @escaping (String) -> Void, send: (() -> Void)?) -> some View {
        modifier(SessionKeysModifier(enabled: enabled, pending: pending, answer: answer, confirmAlways: confirmAlways, send: send))
    }
}

struct SessionKeysModifier: ViewModifier {
    let enabled: Bool
    let pending: PermissionRequest?
    let answer: (PermissionRequest, String) -> Void
    let confirmAlways: (String) -> Void
    let send: (() -> Void)?
    @State private var answered: String?
    @State private var armed: String?

    func body(content: Content) -> some View {
        content
            .focusedSceneValue(\.sessionKeys, SessionKeyActions(enabled: enabled, answers: answers, send: send.map { KeyedAction(label: "Send", run: $0) }))
            .task(id: pending?.id) {
                armed = nil
                try? await Task.sleep(for: .milliseconds(600))
                armed = pending?.id
            }
    }

    private var answers: [Int: KeyedAction] {
        guard let pending, pending.id != answered, pending.id == armed else { return [:] }
        var out: [Int: KeyedAction] = [:]
        for digit in 1...PermissionRequest.keyCount {
            guard let option = pending.keyed(digit) else { continue }
            out[digit] = KeyedAction(label: option.label) {
                if option.kind == .allowAlways {
                    confirmAlways(option.id)
                } else {
                    answered = pending.id
                    answer(pending, option.id)
                }
            }
        }
        return out
    }
}

struct SessionCommands: Commands {
    @FocusedValue(\.sessionKeys) private var keys

    var body: some Commands {
        CommandMenu("Session") {
            Button(keys?.send?.label ?? "Send") { keys?.send?.run() }
                .keyboardShortcut(.return, modifiers: .command)
                .disabled(keys?.enabled != true || keys?.send == nil)
            if let keys {
                ForEach(1...PermissionRequest.keyCount, id: \.self) { digit in
                    if let action = keys.answers[digit] {
                        Button(action.label, action: action.run)
                            .keyboardShortcut(KeyEquivalent(Character(String(digit))), modifiers: .option)
                            .disabled(!keys.enabled)
                    }
                }
            }
        }
    }
}
