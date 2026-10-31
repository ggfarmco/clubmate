import Observation
import SwiftUI

import ClubmateKit

typealias UIEventSink = @Sendable (UIEvent) async -> Result<JSONValue, ClubmateError>

struct SDUIDispatch: Sendable {
    var slot = ""
    var projectID = ""
    var sink: UIEventSink = { _ in .success(.null) }
    var banner: EventBanner?

    @MainActor
    func callAsFunction(_ node: SDUINode, _ trigger: UIAction.Trigger, done: (@MainActor (Bool) -> Void)? = nil) {
        let event = UIAction.event(for: node, trigger: trigger, slot: slot, projectID: projectID)
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

private struct SidebarSlotKey: EnvironmentKey {
    static let defaultValue: String? = nil
}

extension EnvironmentValues {
    var sidebarSlot: String? {
        get { self[SidebarSlotKey.self] }
        set { self[SidebarSlotKey.self] = newValue }
    }
}

struct SDUIView: View {
    let node: SDUINode
    let slot: String
    let projectID: String
    var drafts = ComposerDrafts()
    let send: UIEventSink

    @Environment(EventBanner.self) private var banner: EventBanner?
    @Environment(\.theme) private var theme

    private var topInset: CGFloat {
        slot.hasSuffix(".main") && node.type != "surface" ? theme.gutter : 0
    }

    var body: some View {
        SDUINodeView(node: node)
            .padding(.top, topInset)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .environment(\.sduiDispatch, SDUIDispatch(slot: slot, projectID: projectID, sink: send, banner: banner))
            .environment(\.composerDrafts, drafts)
    }
}

struct SDUINodeView: View {
    let node: SDUINode

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
        case "list": ListNodeView(node: node)
        case "tree": TreeNodeView(node: node)
        case "table": TableNodeView(node: node)
        case "sparkline": SparklineNodeView(node: node)
        case "text": TextNodeView(node: node)
        case "badge": BadgeNodeView(node: node)
        case "icon": IconNodeView(node: node)
        case "markdown": MarkdownNodeView(node: node)
        case "diff": DiffNodeView(node: node)
        case "button": ButtonNodeView(node: node)
        case "textField": TextFieldNodeView(node: node)
        case "folderField": FolderFieldNodeView(node: node)
        case "toggle": ToggleNodeView(node: node)
        case "select": SelectNodeView(node: node)
        case "form": FormNodeView(node: node)
        case "transcript": TranscriptNodeView(node: node)
        case "message": MessageNodeView(node: node).equatable()
        case "plan": PlanNodeView(node: node)
        case "toolCard": ToolCardNodeView(node: node)
        case "permissionCard": PermissionCardNodeView(node: node)
        case "composer": ComposerNodeView(node: node)
        case "surface": SurfaceHost(sessionID: node.string("sessionId") ?? "")
        case "empty": EmptyNodeView(text: node.string("text") ?? "")
        default: UnsupportedNodeView(type: node.type)
        }
    }
}

struct SDUIChildren: View {
    let node: SDUINode

    var body: some View {
        ForEach(node.identifiedChildren) { child in
            SDUINodeView(node: child.node)
        }
    }
}
