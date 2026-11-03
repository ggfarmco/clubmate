import Observation
import SwiftUI

import ClubmateKit

typealias UIEventSink = @Sendable (UIEvent) async -> Result<JSONValue, ClubmateError>

struct SDUIDispatch: Sendable, Equatable {
    var slot = ""
    var workspaceID = ""
    var sink: UIEventSink = { _ in .success(.null) }
    var banner: EventBanner?

    nonisolated static func == (lhs: SDUIDispatch, rhs: SDUIDispatch) -> Bool {
        lhs.slot == rhs.slot && lhs.workspaceID == rhs.workspaceID && lhs.banner === rhs.banner
    }

    @MainActor
    func callAsFunction(_ node: SDUINode, _ trigger: UIAction.Trigger, done: (@MainActor (Bool) -> Void)? = nil) {
        let event = UIAction.event(for: node, trigger: trigger, slot: slot, workspaceID: workspaceID)
        Task { [sink, banner] in
            let result = await sink(event)
            if case .failure(let error) = result {
                banner?.show(error.message)
            }
            if case .success = result { done?(true) } else { done?(false) }
        }
    }

    @MainActor
    func choose(_ entry: SDUIMenuEntry, item: String, in node: SDUINode) {
        guard let message = entry.confirm else { return self(node, .named(entry.action, .string(item))) }
        Task {
            if await Confirmation.ask(message, action: entry.label, destructive: true) { self(node, .named(entry.action, .string(item))) }
        }
    }
}

private struct SDUIDispatchKey: EnvironmentKey {
    static let defaultValue = SDUIDispatch()
}

extension EnvironmentValues {
    var sduiDispatch: SDUIDispatch {
        get { self[SDUIDispatchKey.self] }
        set { self[SDUIDispatchKey.self] = newValue }
    }
}

private struct ComposerDraftsKey: EnvironmentKey {
    static let defaultValue: ComposerDrafts? = nil
}

extension EnvironmentValues {
    var composerDrafts: ComposerDrafts? {
        get { self[ComposerDraftsKey.self] }
        set { self[ComposerDraftsKey.self] = newValue }
    }
}

struct FocusTarget: Equatable {
    let slot: String
    let serial: Int
}

@MainActor
@Observable
final class FocusRouter {
    private(set) var pending: FocusTarget?
    @ObservationIgnored private var serial = 0

    func request(_ slot: String) {
        serial += 1
        pending = FocusTarget(slot: slot, serial: serial)
    }

    func requestMain(from slot: String) {
        guard let dot = slot.lastIndex(of: ".") else { return }
        request(String(slot[..<dot]) + ".main")
    }

    func take(_ slot: String) -> Bool {
        guard pending?.slot == slot else { return false }
        pending = nil
        return true
    }
}

private struct FocusRouterKey: EnvironmentKey {
    static let defaultValue: FocusRouter? = nil
}

extension EnvironmentValues {
    var focusRouter: FocusRouter? {
        get { self[FocusRouterKey.self] }
        set { self[FocusRouterKey.self] = newValue }
    }
}

private struct PaneFocusKey: EnvironmentKey {
    static let defaultValue: PaneFocus? = nil
}

extension EnvironmentValues {
    var paneFocus: PaneFocus? {
        get { self[PaneFocusKey.self] }
        set { self[PaneFocusKey.self] = newValue }
    }
}

private struct PaneFocusOwnerKey: EnvironmentKey {
    static let defaultValue: String? = nil
}

extension EnvironmentValues {
    var paneFocusOwner: String? {
        get { self[PaneFocusOwnerKey.self] }
        set { self[PaneFocusOwnerKey.self] = newValue }
    }
}

private struct PaneFocusReport: ViewModifier {
    let pane: FocusedPane
    let focused: Bool
    let slot: String?

    @State private var id = UUID().uuidString
    @Environment(\.paneFocus) private var panes
    @Environment(\.sduiDispatch) private var dispatch

    func body(content: Content) -> some View {
        content
            .onChange(of: focused, initial: true) { report(focused) }
            .onChange(of: pane) { report(focused) }
            .onDisappear { report(false) }
    }

    private func report(_ focused: Bool) {
        panes?.report(pane, from: id, slot: slot ?? dispatch.slot, focused: focused)
    }
}

extension View {
    func reportsFocus(_ pane: FocusedPane, _ focused: Bool, slot: String? = nil) -> some View {
        modifier(PaneFocusReport(pane: pane, focused: focused, slot: slot))
    }
}

private struct SidebarSideKey: EnvironmentKey {
    static let defaultValue = SidebarSide.left
}

extension EnvironmentValues {
    var sidebarSide: SidebarSide {
        get { self[SidebarSideKey.self] }
        set { self[SidebarSideKey.self] = newValue }
    }
}

enum SidebarField {
    static let suffix = ".field"
}

private struct SidebarHasFieldKey: EnvironmentKey {
    static let defaultValue = false
}

extension EnvironmentValues {
    var sidebarHasField: Bool {
        get { self[SidebarHasFieldKey.self] }
        set { self[SidebarHasFieldKey.self] = newValue }
    }
}

private struct SidebarTabsKey: EnvironmentKey {
    static let defaultValue: SDUINode? = nil
}

extension EnvironmentValues {
    var sidebarTabs: SDUINode? {
        get { self[SidebarTabsKey.self] }
        set { self[SidebarTabsKey.self] = newValue }
    }
}

private struct SidebarSlotKey: EnvironmentKey {
    static let defaultValue: String? = nil
}

extension EnvironmentValues {
    var sidebarSlot: String? {
        get { self[SidebarSlotKey.self] }
        set { self[SidebarSlotKey.self] = newValue }
    }
}

struct SDUIView: View, Equatable {
    let node: SDUINode
    let slot: String
    let workspaceID: String
    var rev: Int64 = 0
    var drafts = ComposerDrafts()
    let send: UIEventSink

    nonisolated static func == (lhs: SDUIView, rhs: SDUIView) -> Bool {
        lhs.rev == rhs.rev && lhs.drafts === rhs.drafts && lhs.slot == rhs.slot && lhs.workspaceID == rhs.workspaceID && lhs.node == rhs.node
    }

    @Environment(EventBanner.self) private var banner: EventBanner?
    @Environment(\.theme) private var theme

    private var topInset: CGFloat {
        slot.hasSuffix(".main") && !["surface", "webview"].contains(node.type) ? theme.gutter : 0
    }

    var body: some View {
        SDUINodeView(node: node)
            .padding(.top, topInset)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .environment(\.sduiDispatch, SDUIDispatch(slot: slot, workspaceID: workspaceID, sink: send, banner: banner))
            .environment(\.composerDrafts, drafts)
            .environment(\.sduiParent, slot)
            .environment(\.paneFocusOwner, node.paneFocusOwner ?? "")
    }
}

struct SDUINodeView: View, Equatable {
    let node: SDUINode

    nonisolated static func == (lhs: SDUINodeView, rhs: SDUINodeView) -> Bool {
        lhs.node == rhs.node
    }

    var body: some View {
        if TestMode.current {
            content.modifier(SDUIProbe(node: node))
        } else {
            content
        }
    }

    @ViewBuilder
    private var content: some View {
        switch node.type {
        case "stack": StackNodeView(node: node)
        case "split": SplitNodeView(node: node)
        case "scroll": ScrollNodeView(node: node)
        case "tabs": TabsNodeView(node: node)
        case "list": OwnScroll { ListNodeView(node: node) }
        case "tree": OwnScroll { TreeNodeView(node: node) }
        case "table": TableNodeView(node: node)
        case "sparkline": SparklineNodeView(node: node)
        case "text": TextNodeView(node: node)
        case "badge": BadgeNodeView(node: node)
        case "icon": IconNodeView(node: node)
        case "markdown": MarkdownNodeView(node: node)
        case "document": DocumentNodeView(node: node)
        case "diff": DiffNodeView(node: node)
        case "button": ButtonNodeView(node: node)
        case "textField": TextFieldNodeView(node: node)
        case "folderField": FolderFieldNodeView(node: node)
        case "toggle": ToggleNodeView(node: node)
        case "progress": ProgressNodeView(node: node)
        case "select": SelectNodeView(node: node)
        case "form" where node.bool("sheet") == true: FormSheetHost(node: node)
        case "form": FormNodeView(node: node)
        case "transcript": TranscriptNodeView(node: node)
        case "message": MessageNodeView(node: node).equatable()
        case "plan": PlanNodeView(node: node)
        case "toolCard": ToolCardNodeView(node: node)
        case "card": CardNodeView(node: node)
        case "permissionCard": PermissionCardNodeView(node: node)
        case "composer": ComposerNodeView(node: node)
        case "surface": SurfaceHost(sessionID: node.string("sessionId") ?? "")
        case "webview": WebViewHost(key: node.string("key") ?? "", url: node.string("url").flatMap(URL.init(string:)), rev: node.props["rev"]?.numberValue.flatMap { Int(exactly: $0) } ?? 0)
        case "empty": EmptyNodeView(node: node)
        case "header": HeaderNodeView(node: node)
        case "graph": GraphNodeView(node: node)
        case "flow": FlowNodeView(node: node)
        case "lanes": LanesNodeView(node: node)
        case "runstrip": RunstripNodeView(node: node)
        default: UnsupportedNodeView(type: node.type)
        }
    }
}

struct SDUIChildren: View {
    let node: SDUINode

    var body: some View {
        ForEach(node.identifiedChildren) { child in
            SDUINodeView(node: child.node).equatable()
        }
    }
}
