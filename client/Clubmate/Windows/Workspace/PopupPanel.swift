import AppKit
import SwiftUI
import WebKit

import ClubmateDocument
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
                if let store = session.store, let slot = presentation.slot, !session.popupPoppedOut, !session.popupHidden {
                    PopupPanel(store: store, slot: slot, zoom: session.popupZoom, find: session.popupFind, active: session.overlay == nil) { [session] action in
                        switch action {
                        case .popOut: popOut()
                        case .close: session.closePopup(slot)
                        case .switchFocus: session.switchPopupFocus()
                        case .resetZoom: break
                        }
                    }
                }
            }
            .onChange(of: session.store?.popup, initial: true) { _, current in
                apply(presentation.update(current))
            }
            .onChange(of: session.popupFocusRequest) { switchFocus() }
            .onChange(of: router?.pending) { _, pending in
                guard session.popupHidden, let slot = presentation.slot, pending?.slot == slot else { return }
                responder.remember()
                session.popupHidden = false
            }
    }

    private func apply(_ change: PopupPresentation.Change?) {
        switch change {
        case .opened(let slot):
            responder.remember()
            if session.popupPoppedOut { return host.show() }
            router?.request(slot)
        case .switched(let slot):
            if session.popupPoppedOut { return host.show() }
            if session.popupHidden {
                responder.remember()
                session.popupHidden = false
            }
            router?.request(slot)
        case .closed:
            session.popupZoom.apply(.fit)
            session.popupFind.hide()
            if session.popupPoppedOut { return host.hide() }
            if session.popupHidden { return session.popupHidden = false }
            returnFocus()
        case nil:
            break
        }
    }

    private func switchFocus() {
        guard let slot = presentation.slot else { return }
        if session.popupPoppedOut {
            guard host.isKey else {
                responder.remember()
                return host.show()
            }
            responder.anchor?.window?.makeKeyAndOrderFront(nil)
            return returnFocus()
        }
        if session.popupHidden {
            responder.remember()
            session.popupHidden = false
            router?.request(slot)
        } else {
            session.popupHidden = true
            returnFocus()
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
    case switchFocus
    case resetZoom
}

struct PopupPanel: View {
    let store: WorkspaceUIStore
    let slot: String
    let zoom: DocumentZoom
    let find: DocumentFind
    let active: Bool
    let perform: @MainActor (PopupAction) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        GeometryReader { proxy in
            ZStack {
                Color.black.opacity(theme.isDark ? 0.45 : 0.25)
                    .contentShape(Rectangle())
                    .onTapGesture { perform(.close) }
                PopupContent(store: store, slot: slot, zoom: zoom, find: find, poppedOut: false, perform: perform)
                    .frame(width: proxy.size.width * 0.8, height: proxy.size.height * 0.8)
                    .background(theme.background)
                    .clipShape(RoundedRectangle(cornerRadius: theme.radius.xl))
                    .overlay(RoundedRectangle(cornerRadius: theme.radius.xl).stroke(theme.borderStrong, lineWidth: 1))
                    .shadow(color: .black.opacity(0.35), radius: 16, y: 6)
            }
        }
        .keyMonitor(active: active) { event in
            Self.handle(event, store: store, slot: slot, zoom: zoom, find: find, perform: perform)
        }
        .modifier(PopupFocus(slot: slot))
        .transition(.opacity)
    }

    static func handle(_ event: NSEvent, store: WorkspaceUIStore, slot: String, zoom: DocumentZoom, find: DocumentFind, perform: (PopupAction) -> Void) -> Bool {
        let flags = event.modifierFlags
        let key = PopupKeys.key(
            keyCode: event.keyCode, characters: event.charactersIgnoringModifiers,
            command: flags.contains(.command), control: flags.contains(.control), option: flags.contains(.option), shift: flags.contains(.shift),
            document: store.slot(slot)?.tree.hasDocument == true, lightbox: zoom.lightbox, finding: find.visible
        )
        switch key {
        case .close: perform(.close)
        case .switchFocus: perform(.switchFocus)
        case .zoomIn: zoom.apply(.zoomIn)
        case .zoomOut: zoom.apply(.zoomOut)
        case .zoomReset: zoom.apply(.fit)
        case .find: find.show()
        case .findNext: Task { await find.next() }
        case .findPrevious: Task { await find.previous() }
        case .closeFind: find.hide()
        case nil: return false
        }
        return true
    }
}

struct PopupContent: View {
    let store: WorkspaceUIStore
    let slot: String
    let zoom: DocumentZoom
    let find: DocumentFind
    let poppedOut: Bool
    let perform: @MainActor (PopupAction) -> Void

    @Environment(\.theme) private var theme
    @State private var measured: CGFloat?

    var body: some View {
        SlotPane(store: store, slot: slot)
            .environment(\.headerTrailingInset, measured ?? chromeWidth)
            .environment(\.documentZoom, zoom)
            .environment(\.documentFind, find)
            .overlay(alignment: .topTrailing) {
                HStack(spacing: theme.spacing.s) {
                    ForEach(buttons, id: \.id) { button in
                        SDUINodeView(node: button)
                            .environment(\.buttonKeyHint, button.id == PopupAction.resetZoom.rawValue ? Self.resetZoomHint : nil)
                    }
                }
                .padding(.horizontal, theme.gutter)
                .padding(.vertical, theme.spacing.l)
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { measured = $0 }
                .environment(\.sduiDispatch, dispatch)
            }
    }

    private var buttons: [SDUINode] {
        var buttons: [SDUINode] = []
        if zoom.scale != 1, store.slot(slot)?.tree.hasDocument == true {
            buttons.append(SDUINode(type: "button", id: PopupAction.resetZoom.rawValue, props: ["label": .string("\(Int((zoom.scale * 100).rounded()))%"), "variant": .string("ghost")]))
        }
        if !poppedOut { buttons.append(Self.button(.popOut, label: "Pop out", icon: "rectangle.portrait.on.rectangle.portrait", shortcut: Self.popOutKey)) }
        buttons.append(Self.button(.close, label: "Close", icon: "xmark", shortcut: nil))
        return buttons
    }

    private var chromeWidth: CGFloat {
        CGFloat(buttons.count) * (theme.control + theme.spacing.s) + theme.gutter
    }

    private var dispatch: SDUIDispatch {
        let perform = perform
        let zoom = zoom
        return SDUIDispatch(slot: slot, workspaceID: store.workspaceID, sink: { event in
            await MainActor.run {
                switch PopupAction(rawValue: event.nodeId) {
                case .resetZoom: zoom.apply(.fit)
                case let action?: perform(action)
                case nil: break
                }
            }
            return .success(.null)
        })
    }

    static let popOutKey = "cmd+shift+o"
    static let resetZoomHint = "⌘0"

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
