import AppKit
import SwiftUI

import ClubmateKit

struct KeysNodeView: View {
    let node: SDUINode

    @FocusState private var focused: Bool
    @Environment(\.isFocused) private var paneFocused
    @Environment(\.paneFocusOwner) private var focusOwner
    @Environment(\.focusRouter) private var router
    @Environment(\.sduiDispatch) private var dispatch

    private var combos: [(name: String, combo: KeyCombo)] {
        (node.props["keys"]?.arrayValue ?? []).compactMap { value in
            guard let name = value.stringValue, let combo = KeyCombo(name) else { return nil }
            return (name, combo)
        }
    }

    var body: some View {
        SDUIChildren(node: node)
            .environment(\.keysFocused, focused)
            .environment(\.keysRefocus, KeysRefocus { focused = true })
            .focusable()
            .focusEffectDisabled()
            .focused($focused)
            .background(Color.clear.contentShape(Rectangle()).onTapGesture { focused = true })
            .keyMonitor(active: focused, handle: handle)
            .reportsFocus(.openItems, focused)
            .onChange(of: paneFocused, initial: true) { _, value in
                if value, node.bool("pane") == true, focusOwner == nil || focusOwner == node.id { focused = true }
            }
            .onChange(of: router?.pending, initial: true) {
                if let id = node.id, router?.pending?.node != nil, router?.take(dispatch.slot, node: id) == true { focused = true }
            }
    }

    private func handle(_ event: NSEvent) -> Bool {
        guard let key = combos.first(where: { $0.combo.fires(on: event) }) else { return false }
        dispatch(node, .submit(.string(key.name)))
        return true
    }
}

struct KeysRefocus {
    let take: @MainActor () -> Void
}

extension EnvironmentValues {
    @Entry var keysFocused = false
    @Entry var keysRefocus: KeysRefocus?
}
