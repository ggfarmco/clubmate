import AppKit
import SwiftUI

import ClubmateKit

struct SlotPane: View {
    let store: WorkspaceUIStore
    let slot: String
    var paneTheme: Theme?
    var hold: TreeHold?

    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        if let view = store.slot(slot) {
            SDUIView(node: view.tree, slot: slot, workspaceID: store.workspaceID, rev: view.rev, drafts: store.drafts) { [store, hold] event in
                await hold?.act()
                let result = await store.send(event)
                if let hold { Task { await hold.acted() } }
                return result
            }
            .equatable()
            .environment(\.theme, paneTheme ?? theme.interface(family: app.settings.paneFontFamily(fontKey), size: app.settings.paneFontSize(fontKey)))
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

    @State private var hold = TreeHold()
    @Environment(\.theme) private var theme

    var body: some View {
        ResizablePane(id: slot + "/resize", side: side, saved: store.sidebarWidth.map { CGFloat($0) } ?? 248, range: 200...320) { [store] width in
            store.sidebarWidth = width
        } content: {
            SlotPane(store: store, slot: slot, hold: hold)
                .environment(\.sidebarSlot, slot)
                .environment(\.sidebarField, store.textFieldID(slot))
                .environment(\.sidebarTabs, store.firstTabs(slot))
                .environment(\.treeHold, hold)
                .background { PointerInside { hold.hover($0) } }
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
                pane
            }
            .frame(maxHeight: .infinity)
        } else {
            ResizablePane(id: slot + "/resize", side: .right, saved: width, range: Self.minWidth...max(Self.minWidth, limit), commit: commit) {
                pane
            }
        }
    }

    private var pane: some View {
        SlotPane(store: store, slot: slot)
            .environment(\.composerFocusSlot, slot)
    }
}

struct AsideColumn: View {
    let store: WorkspaceUIStore
    let stacked: Bool
    let width: CGFloat
    let limit: CGFloat
    let commit: (Double) -> Void

    static let minWidth: CGFloat = 320

    @Environment(\.theme) private var theme
    @State private var dragStart: CGFloat?
    @State private var dragWidth: CGFloat?
    @State private var hovering = false

    private var current: CGFloat {
        min(max(dragWidth ?? width, Self.minWidth), max(Self.minWidth, limit))
    }

    var body: some View {
        let layout = stacked ? AnyLayout(VStackLayout(spacing: 0)) : AnyLayout(HStackLayout(spacing: 0))
        layout {
            edge
            SlotPane(store: store, slot: AgentsSection.asideSlot)
                .environment(\.composerFocusSlot, AgentsSection.asideSlot)
                .frame(width: stacked ? nil : current)
                .frame(maxHeight: .infinity)
        }
    }

    private var edge: some View {
        theme.border
            .frame(width: stacked ? nil : 1, height: stacked ? 1 : nil)
            .overlay {
                if !stacked {
                    Color.clear
                        .frame(width: 7)
                        .contentShape(Rectangle())
                        .onHover(perform: hover)
                        .onDisappear { hover(false) }
                        .gesture(resize)
                        .testNode(AgentsSection.asideSlot + "/resize", role: "divider")
                }
            }
            .zIndex(1)
    }

    private var resize: some Gesture {
        DragGesture(minimumDistance: 1, coordinateSpace: .global)
            .onChanged { drag in
                let start = dragStart ?? current
                dragStart = start
                dragWidth = min(max(start - drag.translation.width, Self.minWidth), max(Self.minWidth, limit))
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

private struct ResizablePane<Content: View>: View {
    let id: String
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
                    .testNode(id, role: "divider")
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
        let loaded = store.hasSlot(slot)
        return content
            .focusable(surface == nil)
            .focusEffectDisabled()
            .focused($focused)
            .environment(\.composerFocusSlot, slot)
            .onChange(of: router?.pending, initial: true) { claim() }
            .onChange(of: slot) { claim() }
            .onChange(of: surface) { claim() }
            .onChange(of: composer) { claim() }
            .onChange(of: loaded) { claim() }
    }

    private func claim() {
        guard store.hasSlot(slot), !store.hasComposer(slot), router?.take(slot) == true else { return }
        if let surface { deck?.focus(surface) } else { focused = true }
    }
}
