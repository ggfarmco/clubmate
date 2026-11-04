import SwiftUI

import CompanionKit

@MainActor
struct SDUIContext {
    let slot: String
    let send: (String, String, JSONValue?) async -> Void
    let open: (String) -> Void
    var openDoc: ((String) -> Void)? = nil

    var mainSlot: String? {
        guard slot.hasSuffix(".sidebar") else { return nil }
        return String(slot.dropLast("sidebar".count)) + "main"
    }

    func fire(_ node: SDUINode, _ trigger: ClubmateProtocol.UIAction.Trigger) async {
        let event = ClubmateProtocol.UIAction.event(for: node, trigger: trigger, slot: slot, workspaceID: "")
        if event.action == "openDoc", let openDoc, let path = event.value.stringValue { return openDoc(path) }
        await send(event.nodeId, event.action, event.value)
    }

    func select(_ node: SDUINode, _ id: String) async {
        if let mainSlot { open(mainSlot) }
        await fire(node, .select(id))
    }

    func changed(_ node: SDUINode, _ trigger: ClubmateProtocol.UIAction.Trigger, in form: SDUIFormState?) async {
        if let id = node.id { form?.values[id] = trigger.value }
        if form == nil || node.string("action") != nil { await fire(node, trigger) }
        if let form, form.autosave { await form.send() }
    }

    func run(_ work: @escaping @MainActor (SDUIContext) async -> Void) {
        Task { @MainActor in await work(self) }
    }
}

enum SDUIKind: String {
    case stack, split, scroll, spacer, tabs, list, tree, table, sparkline, progress, text, badge, icon, markdown, document, diff
    case button, textField, folderField, toggle, select, form, surface, header, empty, card
    case transcript, message, plan, toolCard, permissionCard, composer
}

struct SDUINodeView: View {
    let node: SDUINode
    let context: SDUIContext

    init(node: SDUINode, context: SDUIContext) {
        self.node = node
        self.context = context
    }

    var body: some View {
        if let kind = SDUIKind(rawValue: node.type) {
            content(kind)
        } else if MacOnlyCaption.types.contains(node.type) {
            if let title = node.string("title"), ["runstrip", "lanes"].contains(node.type), !title.isEmpty {
                DrawnRunsSummary(node: node, title: title)
            } else if !MacOnlyCaption.ghostOnly(node) {
                MacOnlyCaption(type: node.type)
            }
        } else {
            SDUIPlaceholder(type: node.type)
        }
    }

    @ViewBuilder
    private func content(_ kind: SDUIKind) -> some View {
        switch kind {
        case .stack: StackNode(node: node, context: context)
        case .split: SplitNode(node: node, context: context)
        case .scroll: ScrollNode(node: node, context: context)
        case .spacer: Spacer(minLength: 0)
        case .card: CardNode(node: node, context: context)
        case .form: FormNode(node: node, context: context)
        case .tabs: TabsNode(node: node, context: context)
        case .list: ListNode(node: node, context: context)
        case .tree: TreeNode(node: node, context: context)
        case .table: TableNode(node: node, context: context)
        case .text: TextNode(node: node)
        case .badge: BadgeNode(node: node)
        case .icon: IconNode(node: node)
        case .markdown: MarkdownNode(node: node)
        case .diff: DiffNode(node: node)
        case .document: DocumentNode(node: node, context: context)
        case .sparkline: SparklineNode(node: node)
        case .progress: ProgressNode(node: node)
        case .header: HeaderNode(node: node, context: context)
        case .empty: EmptyNode(node: node, context: context)
        case .surface: SurfacePlaceholder()
        case .button: ButtonNode(node: node, context: context)
        case .textField: TextFieldNode(node: node, context: context)
        case .folderField: FolderFieldNode(node: node)
        case .toggle: ToggleNode(node: node, context: context)
        case .select: SelectNode(node: node, context: context)
        case .transcript: TranscriptNode(node: node, context: context)
        case .message: MessageNode(node: node, context: context)
        case .plan: PlanNode(node: node)
        case .toolCard: ToolCardNode(node: node, context: context)
        case .permissionCard: PermissionCardNode(node: node, context: context)
        case .composer: ComposerNode(node: node, context: context)
        }
    }
}

struct SDUIChildren: View {
    let node: SDUINode
    let context: SDUIContext

    var body: some View {
        ForEach(node.identifiedChildren) { child in
            SDUINodeView(node: child.node, context: context)
        }
    }
}

@MainActor
@Observable
final class SDUIFormState {
    static let typingDelay = Duration.milliseconds(600)

    var values: [String: JSONValue] = [:]
    private(set) var save = UIFormSave()
    @ObservationIgnored private(set) var autosave = false
    @ObservationIgnored private var node = SDUINode(type: "form")
    @ObservationIgnored private var context: SDUIContext?
    @ObservationIgnored private var pending: Task<Void, Never>?

    var saving: Bool { save.saving && autosave }

    func update(_ node: SDUINode, context: SDUIContext) {
        let ids = UIForm.fieldIDs(in: node)
        let old = UIForm.fieldIDs(in: self.node)
        if save.dirty, self.context != nil, !old.isSubset(of: ids) {
            let gone = JSONValue.object(values.filter { old.contains($0.key) })
            let previous = self.node
            context.run { await $0.fire(previous, .submit(gone)) }
        }
        values = values.filter { ids.contains($0.key) }
        self.node = node
        self.context = context
        autosave = node.bool("autosave") == true
    }

    func typed() {
        guard autosave else { return }
        save.edited()
        pending?.cancel()
        pending = Task { [weak self] in
            try? await Task.sleep(for: Self.typingDelay)
            guard !Task.isCancelled else { return }
            await self?.send()
        }
    }

    func submit() {
        Task { await send() }
    }

    func flush() {
        if save.dirty { submit() }
    }

    func send() async {
        guard let context else { return }
        pending?.cancel()
        pending = nil
        save.sent()
        await context.fire(node, .submit(UIForm.collect(values, in: node)))
        save.answered()
    }
}

extension EnvironmentValues {
    @Entry var sduiInScroll = false
    @Entry var sduiFramed = false
    @Entry var sduiInset: CGFloat = 16
    @Entry var sduiForm: SDUIFormState?
    @Entry var sduiFormRow = false
}
