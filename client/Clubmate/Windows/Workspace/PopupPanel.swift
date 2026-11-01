import AppKit
import SwiftUI
import WebKit

import ClubmateKit

extension View {
    func slotPopup(_ session: WorkspaceSession) -> some View {
        modifier(SlotPopup(session: session))
    }
}

private struct SlotPopup: ViewModifier {
    let session: WorkspaceSession

    @Environment(AppModel.self) private var app
    @Environment(\.focusRouter) private var router
    @State private var presentation = PopupPresentation()
    @State private var responder = PopupResponder()

    func body(content: Content) -> some View {
        content
            .background(ResponderAnchor(responder: responder))
            .overlay {
                if let store = session.store, let slot = presentation.slot, !session.popupPoppedOut {
                    PopupPanel(store: store, slot: slot, active: session.overlay == nil) { [session] action in
                        switch action {
                        case .popOut: popOut()
                        case .close: session.closePopup(slot)
                        }
                    }
                }
            }
            .onChange(of: session.store?.popup, initial: true) { _, current in
                apply(presentation.update(current))
            }
    }

    private func apply(_ change: PopupPresentation.Change?) {
        switch change {
        case .opened(let slot):
            if session.popupPoppedOut { return host.show() }
            responder.remember()
            router?.request(slot)
        case .switched(let slot):
            if session.popupPoppedOut { return host.show() }
            router?.request(slot)
        case .closed:
            if session.popupPoppedOut { return host.hide() }
            returnFocus()
        case nil:
            break
        }
    }

    private func popOut() {
        session.popupPoppedOut = true
        returnFocus()
        host.show()
    }

    private func returnFocus() {
        guard !responder.restore(), let section = session.store?.selectedSection else { return }
        router?.request("\(section).main")
    }

    private var host: PopupWindowHost {
        if let host = session.popupHost { return host }
        let host = PopupWindowHost(session: session, app: app)
        session.popupHost = host
        return host
    }
}

enum PopupAction: String {
    case popOut
    case close
}

struct PopupPanel: View {
    let store: WorkspaceUIStore
    let slot: String
    let active: Bool
    let perform: @MainActor (PopupAction) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        GeometryReader { proxy in
            ZStack {
                Color.black.opacity(theme.isDark ? 0.45 : 0.25)
                    .contentShape(Rectangle())
                    .onTapGesture { perform(.close) }
                PopupContent(store: store, slot: slot, poppedOut: false, perform: perform)
                    .frame(width: proxy.size.width * 0.8, height: proxy.size.height * 0.8)
                    .background(theme.background)
                    .clipShape(RoundedRectangle(cornerRadius: theme.radius.xl))
                    .overlay(RoundedRectangle(cornerRadius: theme.radius.xl).stroke(theme.borderStrong, lineWidth: 1))
                    .shadow(color: .black.opacity(0.35), radius: 16, y: 6)
            }
        }
        .keyMonitor(active: active) { event in
            guard event.keyCode == Self.escape, event.modifierFlags.intersection(.deviceIndependentFlagsMask).subtracting(.capsLock).isEmpty else { return false }
            perform(.close)
            return true
        }
        .modifier(PopupFocus(slot: slot))
        .transition(.opacity)
    }

    static let escape: UInt16 = 53
}

struct PopupContent: View {
    let store: WorkspaceUIStore
    let slot: String
    let poppedOut: Bool
    let perform: @MainActor (PopupAction) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        SlotPane(store: store, slot: slot)
            .environment(\.headerTrailingInset, chromeWidth)
            .overlay(alignment: .topTrailing) {
                HStack(spacing: theme.spacing.s) {
                    ForEach(buttons, id: \.id) { button in
                        SDUINodeView(node: button)
                    }
                }
                .padding(.horizontal, theme.gutter)
                .padding(.vertical, theme.spacing.l)
                .environment(\.sduiDispatch, dispatch)
            }
    }

    private var buttons: [SDUINode] {
        var buttons: [SDUINode] = []
        if !poppedOut { buttons.append(Self.button(.popOut, label: "Pop out", icon: "rectangle.portrait.on.rectangle.portrait", shortcut: Self.popOutKey)) }
        buttons.append(Self.button(.close, label: "Close", icon: "xmark", shortcut: nil))
        return buttons
    }

    private var chromeWidth: CGFloat {
        CGFloat(buttons.count) * (theme.control + theme.spacing.s) + theme.gutter
    }

    private var dispatch: SDUIDispatch {
        let perform = perform
        return SDUIDispatch(slot: slot, workspaceID: store.workspaceID, sink: { event in
            await MainActor.run { PopupAction(rawValue: event.nodeId).map(perform) }
            return .success(.null)
        })
    }

    static let popOutKey = "cmd+shift+o"

    private static func button(_ action: PopupAction, label: String, icon: String, shortcut: String?) -> SDUINode {
        var props: [String: JSONValue] = ["label": .string(label), "icon": .string(icon), "variant": .string("ghost"), "iconOnly": .bool(true)]
        if let shortcut { props["shortcut"] = .string(shortcut) }
        return SDUINode(type: "button", id: action.rawValue, props: props)
    }
}

private struct PopupFocus: ViewModifier {
    let slot: String

    @Environment(\.focusRouter) private var router
    @State private var anchor = PopupFocusAnchor.Box()

    func body(content: Content) -> some View {
        content
            .background(PopupFocusAnchor(box: anchor))
            .onChange(of: router?.pending, initial: true) { claim() }
            .onChange(of: slot) { claim() }
    }

    private func claim() {
        guard router?.take(slot) == true else { return }
        anchor.focus()
    }
}

private struct PopupFocusAnchor: NSViewRepresentable {
    @MainActor
    final class Box {
        weak var view: AnchorView?

        func focus() {
            guard let view else { return }
            DispatchQueue.main.async { view.focusContent() }
        }
    }

    final class AnchorView: NSView {
        override var acceptsFirstResponder: Bool { true }

        func focusContent() {
            guard let window, let root = window.contentView else { return }
            let frame = convert(bounds, to: nil)
            let document = root.descendants(WKWebView.self).first { frame.contains($0.convert($0.bounds, to: nil)) }
            window.makeFirstResponder(document ?? self)
        }
    }

    let box: Box

    func makeNSView(context: Context) -> AnchorView {
        let view = AnchorView()
        box.view = view
        return view
    }

    func updateNSView(_ view: AnchorView, context: Context) {
        box.view = view
    }
}

@MainActor
final class PopupResponder {
    weak var anchor: NSView?
    private weak var saved: NSResponder?

    func remember() {
        saved = anchor?.window?.firstResponder
    }

    func restore() -> Bool {
        defer { saved = nil }
        guard let window = anchor?.window, let view = saved as? NSView, view.window === window, view !== window.contentView else { return false }
        return window.makeFirstResponder(view)
    }
}

private struct ResponderAnchor: NSViewRepresentable {
    let responder: PopupResponder

    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        responder.anchor = view
        return view
    }

    func updateNSView(_ view: NSView, context: Context) {
        responder.anchor = view
    }
}

extension NSView {
    func descendants<View: NSView>(_ type: View.Type) -> [View] {
        subviews.flatMap { subview in (subview as? View).map { [$0] } ?? subview.descendants(type) }
    }
}
