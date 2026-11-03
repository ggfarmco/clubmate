import AppKit
import SwiftUI

import ClubmateKit

@MainActor
final class SDUIPopover {
    static let dismissAction = "dismiss"
    static let width: CGFloat = 440

    var dismiss: () -> Void = {}
    var submit: (() -> Void)?

    static func keyLabel(_ node: SDUINode) -> String? {
        switch node.string("action") {
        case nil: "↩"
        case dismissAction: "⎋"
        default: nil
        }
    }

    func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection([.command, .control, .option, .shift])
        let responder = event.window?.firstResponder
        switch event.keyCode {
        case 53 where flags.isEmpty:
            dismiss()
            return true
        case 36, 76:
            if (responder as? NSTextInputClient)?.hasMarkedText() == true { return false }
            guard flags == .command || flags.isEmpty && !(responder is NSTextView), let submit else { return false }
            submit()
            return true
        default:
            return false
        }
    }
}

private struct SDUIPopoverKey: EnvironmentKey {
    static let defaultValue: SDUIPopover? = nil
}

extension EnvironmentValues {
    var sduiPopover: SDUIPopover? {
        get { self[SDUIPopoverKey.self] }
        set { self[SDUIPopoverKey.self] = newValue }
    }
}

struct SDUIPopoverView: View {
    let form: SDUINode
    let popover: SDUIPopover

    @Environment(\.theme) private var theme

    var body: some View {
        SDUINodeView(node: form)
            .environment(\.sduiPopover, popover)
            .padding(.horizontal, theme.gutter * 2)
            .frame(width: SDUIPopover.width, alignment: .leading)
            .fixedSize(horizontal: false, vertical: true)
            .presentationBackground(theme.surface3)
            .keyMonitor(active: true, handle: popover.handle)
    }
}

struct PopoverPresenter: ViewModifier {
    let target: String?
    let shown: Binding<Bool>

    func body(content: Content) -> some View {
        if let target {
            content.testPresenter(target, shown)
        } else {
            content
        }
    }
}

struct FileDrop: ViewModifier {
    let active: Bool
    @Binding var targeted: Bool
    let drop: ([URL]) -> Bool

    func body(content: Content) -> some View {
        if active {
            content.dropDestination(for: URL.self) { urls, _ in drop(urls) } isTargeted: { targeted = $0 }
        } else {
            content
        }
    }
}
