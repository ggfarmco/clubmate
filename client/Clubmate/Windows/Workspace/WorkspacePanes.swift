import AppKit
import SwiftUI

import ClubmateKit

enum SlotPart {
    case whole
    case without(String)
    case only(String)

    func pick(_ tree: SDUINode) -> SDUINode? {
        switch self {
        case .whole: tree
        case .without(let id): tree.removing(id: id)
        case .only(let id): tree.node(id: id)
        }
    }
}

struct SlotPane: View {
    let store: WorkspaceUIStore
    let slot: String
    var part: SlotPart = .whole

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        if let view = store.slot(slot) {
            if let node = part.pick(view.tree) {
                SDUIView(node: node, slot: slot, workspaceID: store.workspaceID, rev: view.rev, drafts: store.drafts) { [store] event in
                    await store.send(event)
                }
                .equatable()
                .environment(\.theme, theme.interface(family: app.settings.paneFontFamily(fontKey), size: app.settings.paneFontSize(fontKey)))
            }
        } else {
            EmptyNodeView(text: "")
        }
    }

    private var fontKey: String {
        PaneFont.key(slot: slot, composer: store.hasComposer(slot))
    }
}

struct SidebarPane: View {
    let store: WorkspaceUIStore
    let slot: String
    let side: SidebarSide

    @Environment(\.theme) private var theme

    var body: some View {
        ResizablePane(side: side, saved: store.sidebarWidth.map { CGFloat($0) } ?? 248, range: 200...320) { [store] width in
            store.sidebarWidth = width
        } content: {
            SlotPane(store: store, slot: slot)
                .environment(\.sidebarSlot, slot)
                .environment(\.sidebarHasField, store.hasTextField(slot))
                .environment(\.sidebarTabs, store.firstTabs(slot))
                .background(theme.surface1, ignoresSafeAreaEdges: [])
        }
    }
}

struct CodeColumn: View {
    let store: WorkspaceUIStore
    let slot: String
    let stacked: Bool
    let width: CGFloat
    let limit: CGFloat
    let commit: (Double) -> Void

    @Environment(\.theme) private var theme

    private static let minWidth = CGFloat(CodeSection.columnMinWidth)

    var body: some View {
        if stacked {
            VStack(spacing: 0) {
                theme.border.frame(height: 1)
                CodeColumnContent(store: store, slot: slot)
            }
            .frame(maxHeight: .infinity)
        } else {
            ResizablePane(side: .right, saved: width, range: Self.minWidth...max(Self.minWidth, limit), commit: commit) {
                CodeColumnContent(store: store, slot: slot)
            }
        }
    }
}

struct CodeColumnContent: View {
    let store: WorkspaceUIStore
    let slot: String

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(spacing: 0) {
            if store.hasCodeTabs {
                CodeTabBar(store: store)
                theme.border.frame(height: 1)
            }
            SlotPane(store: store, slot: slot, part: slot == CodeSection.reviewSlot ? .without(CodeSection.reviewStrip) : .whole)
                .environment(\.composerFocusSlot, slot)
                .environment(\.sidebarSlot, slot == CodeSection.reviewSlot ? slot : nil)
        }
    }
}

struct CodeTabBar: View {
    let store: WorkspaceUIStore

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.s) {
            tab(.review, "Review")
            tab(.chat, "Chat")
            Spacer(minLength: theme.spacing.s)
            Text(AppShortcuts.switchCodeTab.labels.first ?? "")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .help(AppShortcuts.switchCodeTab.action)
        }
        .frame(minWidth: 0, maxWidth: .infinity)
        .padding(.horizontal, theme.gutter)
        .padding(.vertical, theme.spacing.s)
    }

    @ViewBuilder private func tab(_ tab: CodeTab, _ label: String) -> some View {
        if let item = SDUIItem(.object(["id": .string(tab.rawValue), "label": .string(label)])) {
            TabButton(item: item, compact: false, selected: store.codeTab == tab) {
                if store.codeTab != tab { store.switchCodeTab() }
            }
        }
    }
}

struct ReviewStrip: View {
    let store: WorkspaceUIStore

    @Environment(\.theme) private var theme

    var body: some View {
        if store.slot(CodeSection.reviewSlot)?.tree.node(id: CodeSection.reviewStrip) != nil {
            VStack(spacing: 0) {
                SlotPane(store: store, slot: CodeSection.reviewSlot, part: .only(CodeSection.reviewStrip))
                    .fixedSize(horizontal: false, vertical: true)
                theme.border.frame(height: 1)
            }
            .background(theme.surface1, ignoresSafeAreaEdges: [])
        }
    }
}

struct ReviewDrawer: View {
    let store: WorkspaceUIStore
    let slot: String
    let width: CGFloat

    @Environment(\.theme) private var theme

    var body: some View {
        ZStack(alignment: .leading) {
            Color.black.opacity(theme.isDark ? 0.45 : 0.25)
                .contentShape(Rectangle())
                .onTapGesture { store.toggleSidebar() }
            HStack(spacing: 0) {
                CodeColumnContent(store: store, slot: slot)
                    .frame(width: width)
                    .background(theme.background, ignoresSafeAreaEdges: [])
                theme.border.frame(width: 1)
            }
            .shadow(color: .black.opacity(0.35), radius: 16, y: 6)
        }
    }
}

private struct ResizablePane<Content: View>: View {
    let side: SidebarSide
    let saved: CGFloat
    let range: ClosedRange<CGFloat>
    let commit: (Double) -> Void
    @ViewBuilder let content: Content

    @Environment(\.theme) private var theme
    @State private var dragStart: CGFloat?
    @State private var dragWidth: CGFloat?
    @State private var hovering = false

    private var width: CGFloat {
        min(max(dragWidth ?? saved, range.lowerBound), range.upperBound)
    }

    var body: some View {
        HStack(spacing: 0) {
            if side == .right { handle }
            content
                .frame(width: width)
            if side == .left { handle }
        }
    }

    private var handle: some View {
        theme.border
            .frame(width: 1)
            .overlay {
                Color.clear
                    .frame(width: 7)
                    .contentShape(Rectangle())
                    .onHover(perform: hover)
                    .gesture(resize)
            }
            .zIndex(1)
    }

    private var resize: some Gesture {
        DragGesture(minimumDistance: 1, coordinateSpace: .global)
            .onChanged { drag in
                let start = dragStart ?? width
                dragStart = start
                let delta = side == .left ? drag.translation.width : -drag.translation.width
                dragWidth = min(max(start + delta, range.lowerBound), range.upperBound)
            }
            .onEnded { _ in
                if let dragWidth { commit(Double(dragWidth)) }
                dragStart = nil
                dragWidth = nil
            }
    }

    private func hover(_ inside: Bool) {
        guard inside != hovering else { return }
        hovering = inside
        if inside { NSCursor.resizeLeftRight.push() } else { NSCursor.pop() }
    }
}

struct MainFocus: ViewModifier {
    let store: WorkspaceUIStore
    let slot: String

    @FocusState private var focused: Bool
    @Environment(\.focusRouter) private var router
    @Environment(\.surfaceDeck) private var deck

    private var surface: String? {
        store.surfaceSessionID(slot)
    }

    func body(content: Content) -> some View {
        let surface = surface
        let composer = store.hasComposer(slot)
        return content
            .focusable(surface == nil)
            .focusEffectDisabled()
            .focused($focused)
            .environment(\.composerFocusSlot, slot)
            .onChange(of: router?.pending, initial: true) { claim() }
            .onChange(of: slot) { claim() }
            .onChange(of: surface) { claim() }
            .onChange(of: composer) { claim() }
    }

    private func claim() {
        guard !store.hasComposer(slot), router?.take(slot) == true else { return }
        if let surface { deck?.focus(surface) } else { focused = true }
    }
}
