import AppKit
import SwiftUI

import ClubmateKit

struct KeepAwakeItem: View {
    let model: KeepAwakeModel
    let barWidth: CGFloat
    let settings: () -> Void
    let openSession: (_ session: String, _ workspace: String) -> Void

    @State private var hovering = false
    @State private var open = false
    @State private var handle = KeepAwakeTrigger.Handle()
    @Environment(\.theme) private var theme

    var body: some View {
        if let state = model.state {
            item(state)
        }
    }

    private func item(_ state: KeepAwake) -> some View {
        Group {
            if state.showsText(barWidth: barWidth) {
                ViewThatFits(in: .horizontal) {
                    label(state, text: true)
                    label(state, text: false)
                }
            } else {
                label(state, text: false)
            }
        }
        .font(theme.uiSmall)
        .padding(.horizontal, theme.spacing.xs)
        .frame(height: 18)
        .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(open ? theme.pressed : hovering ? theme.hover : .clear))
        .overlay {
            KeepAwakeTrigger(
                handle: handle,
                hovering: $hovering,
                tooltip: help(state),
                menu: { menu(state) },
                opened: { open = $0 },
                toggle: { Task { await model.toggle() } }
            )
        }
        .accessibilityElement(children: .ignore)
        .accessibilityAddTraits(.isButton)
        .accessibilityLabel("Keep Awake")
        .accessibilityValue(state.on ? state.text : "Off")
        .accessibilityHint(state.detail)
        .accessibilityAction { handle.view?.show() }
        .testMenu("awake", KeepAwakeMenu(state).layout)
    }

    private func label(_ state: KeepAwake, text: Bool) -> some View {
        HStack(spacing: theme.spacing.xs) {
            GourdGlyph(awake: state.look == .awake, size: 14)
                .foregroundStyle(glyphColor(state.look))
            if text {
                Text(state.text)
                    .foregroundStyle(theme.muted)
                    .fixedSize()
            }
        }
    }

    private func glyphColor(_ look: KeepAwake.Look) -> Color {
        switch look {
        case .off: theme.faint
        case .armed: theme.muted
        case .awake: theme.foreground
        }
    }

    private func help(_ state: KeepAwake) -> String {
        [state.detail, model.error].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n")
    }

    private func menu(_ state: KeepAwake) -> NSMenu {
        let menu = NSMenu()
        menu.autoenablesItems = false
        let entries = KeepAwakeMenu(state)
        let holds = entries.items.filter { $0.kind == .hold }
        let tab = HoldLine.tabStop(holds)
        for entry in entries.items {
            menu.addItem(item(entry, look: state.look, tab: tab))
        }
        return menu
    }

    private func item(_ entry: KeepAwakeMenu.Item, look: KeepAwake.Look, tab: CGFloat) -> NSMenuItem {
        switch entry.kind {
        case .separator:
            return .separator()
        case .section:
            let item = NSMenuItem.sectionHeader(title: entry.title)
            item.isEnabled = false
            return item
        case .status:
            let item = NSMenuItem(title: entry.title, action: nil, keyEquivalent: "")
            item.attributedTitle = NSAttributedString(string: entry.title, attributes: [.font: NSFont.boldSystemFont(ofSize: NSFont.systemFontSize), .foregroundColor: NSColor.labelColor])
            item.image = GourdGlyph.image(awake: look == .awake)
            item.isEnabled = false
            return item
        case .hold:
            let item = ActionMenuItem(title: entry.title, run: entry.action.map(run))
            item.attributedTitle = HoldLine.title(entry.shown, tab: tab)
            item.indentationLevel = 1
            item.isEnabled = entry.enabled
            return item
        case .item:
            let item = ActionMenuItem(title: entry.title, run: entry.action.map(run))
            item.state = entry.checked ? .on : .off
            item.isEnabled = entry.enabled
            if entry.toggleKey {
                item.keyEquivalent = "a"
                item.keyEquivalentModifierMask = [.command, .option]
            }
            if !entry.children.isEmpty {
                let submenu = NSMenu()
                submenu.autoenablesItems = false
                for child in entry.children {
                    submenu.addItem(self.item(child, look: look, tab: tab))
                }
                item.submenu = submenu
            }
            return item
        }
    }

    private func run(_ action: KeepAwakeMenu.Action) -> () -> Void {
        { [model, settings, openSession] in
            switch action {
            case .set(let mode, let hours): Task { await model.set(mode: mode, hours: hours) }
            case .turnOff: Task { await model.turnOff() }
            case .settings: settings()
            case .openSession(let session, let workspace): openSession(session, workspace)
            }
        }
    }
}

@MainActor
private enum HoldLine {
    static let font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
    static let gap: CGFloat = 32

    static func tabStop(_ holds: [KeepAwakeMenu.Item]) -> CGFloat {
        holds.map { width($0.title) + gap + width($0.trailing) }.max() ?? 0
    }

    static func title(_ shown: String, tab: CGFloat) -> NSAttributedString {
        let paragraph = NSMutableParagraphStyle()
        paragraph.tabStops = [NSTextTab(textAlignment: .right, location: tab)]
        return NSAttributedString(string: shown, attributes: [.font: font, .foregroundColor: NSColor.secondaryLabelColor, .paragraphStyle: paragraph])
    }

    private static func width(_ text: String) -> CGFloat {
        ceil((text as NSString).size(withAttributes: [.font: font]).width)
    }
}

private final class ActionMenuItem: NSMenuItem {
    private let run: (() -> Void)?

    init(title: String, run: (() -> Void)?) {
        self.run = run
        super.init(title: title, action: run == nil ? nil : #selector(fire), keyEquivalent: "")
        target = self
    }

    required init(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    @objc private func fire() {
        run?()
    }
}

private struct KeepAwakeTrigger: NSViewRepresentable {
    @MainActor
    final class Handle {
        weak var view: TriggerView?
    }

    let handle: Handle
    @Binding var hovering: Bool
    let tooltip: String
    let menu: () -> NSMenu
    let opened: (Bool) -> Void
    let toggle: () -> Void

    func makeNSView(context: Context) -> TriggerView {
        TriggerView()
    }

    func updateNSView(_ view: TriggerView, context: Context) {
        handle.view = view
        view.toolTip = tooltip.isEmpty ? nil : tooltip
        view.hovered = { hovering = $0 }
        view.makeMenu = menu
        view.opened = opened
        view.toggle = toggle
    }

    final class TriggerView: NSView {
        var hovered: ((Bool) -> Void)?
        var makeMenu: (() -> NSMenu)?
        var opened: ((Bool) -> Void)?
        var toggle: (() -> Void)?

        private var area: NSTrackingArea?

        override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

        override func updateTrackingAreas() {
            super.updateTrackingAreas()
            if let area { removeTrackingArea(area) }
            let area = NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeAlways, .inVisibleRect], owner: self)
            addTrackingArea(area)
            self.area = area
        }

        override func mouseEntered(with event: NSEvent) { hovered?(true) }

        override func mouseExited(with event: NSEvent) { hovered?(false) }

        override func mouseDown(with event: NSEvent) {
            if event.modifierFlags.contains(.option) {
                toggle?()
            } else {
                show()
            }
        }

        override func rightMouseDown(with event: NSEvent) {
            show()
        }

        func show() {
            guard let menu = makeMenu?() else { return }
            opened?(true)
            let origin = NSPoint(x: bounds.maxX - menu.size.width, y: isFlipped ? -4 : bounds.maxY + 4)
            menu.popUp(positioning: nil, at: NSPoint(x: max(0, origin.x), y: origin.y + (isFlipped ? -menu.size.height : menu.size.height)), in: self)
            opened?(false)
        }
    }
}
