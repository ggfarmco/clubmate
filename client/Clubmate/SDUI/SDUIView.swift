import Observation
import SwiftUI

import ClubmateKit

typealias UIEventSink = @Sendable (UIEvent) async -> Result<JSONValue, ClubmateError>
typealias CommandSink = @Sendable (CommandInvocation) async -> Result<JSONValue, ClubmateError>

struct SDUIDispatch: Sendable {
    var slot = ""
    var projectID = ""
    var owner = ""
    var sink: UIEventSink = { _ in .success(.null) }
    var command: CommandSink = { _ in .success(.null) }
    var banner: EventBanner?

    @MainActor
    func callAsFunction(_ node: SDUINode, _ trigger: UIAction.Trigger) {
        let event = UIAction.event(for: node, trigger: trigger, slot: slot, projectID: projectID)
        Task { [sink, banner] in
            if case .failure(let error) = await sink(event) {
                banner?.show(error.message)
            }
        }
    }

    @MainActor
    func run(_ invocation: CommandInvocation) {
        Task { [command, banner] in
            if case .failure(let error) = await command(invocation) {
                banner?.show(error.message)
            }
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

struct SDUIView: View {
    let node: SDUINode
    let slot: String
    let projectID: String
    var owner = ""
    let send: UIEventSink
    var run: CommandSink = { _ in .success(.null) }

    @Environment(EventBanner.self) private var banner: EventBanner?

    var body: some View {
        SDUINodeView(node: node)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .environment(\.sduiDispatch, SDUIDispatch(slot: slot, projectID: projectID, owner: owner, sink: send, command: run, banner: banner))
    }
}

struct SDUINodeView: View {
    let node: SDUINode

    var body: some View {
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
