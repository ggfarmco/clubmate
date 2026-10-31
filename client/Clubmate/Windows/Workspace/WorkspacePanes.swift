import AppKit
import SwiftUI

import ClubmateKit

struct SlotPane: View {
    let store: ProjectUIStore
    let slot: String

    var body: some View {
        if let view = store.slots[slot] {
            SDUIView(node: view.tree, slot: slot, projectID: store.projectID, drafts: store.drafts) { [store] event in
                await store.send(event)
            }
        } else {
            EmptyNodeView(text: "")
        }
    }
}

struct SidebarPane: View {
    let store: ProjectUIStore
    let slot: String
    let side: SidebarSide

    var body: some View {
        ResizablePane(side: side, saved: store.sidebarWidth.map { CGFloat($0) } ?? 220, range: 160...400) { [store] width in
            store.sidebarWidth = width
        } content: {
            SlotPane(store: store, slot: slot)
                .environment(\.sidebarSlot, slot)
                .environment(\.sidebarHasField, store.slots[slot]?.tree.hasTextField ?? false)
        }
    }
}

struct ChatPane: View {
    let store: ProjectUIStore
    let saved: Double?
    let limit: CGFloat
    let commit: (Double) -> Void

    private static let minWidth: CGFloat = 280

    var body: some View {
        ResizablePane(side: .right, saved: saved.map { CGFloat($0) } ?? 380, range: Self.minWidth...max(Self.minWidth, limit), commit: commit) {
            SlotPane(store: store, slot: CodeSection.chatSlot)
                .environment(\.composerFocusSlot, CodeSection.chatSlot)
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
    let slot: String
    let surface: String?

    @FocusState private var focused: Bool
    @Environment(\.focusRouter) private var router
    @Environment(\.surfaceDeck) private var deck

    func body(content: Content) -> some View {
        content
            .focusable(surface == nil)
            .focusEffectDisabled()
            .focused($focused)
            .onChange(of: router?.pending, initial: true) { claim() }
            .onChange(of: slot) { claim() }
            .onChange(of: surface) { claim() }
    }

    private func claim() {
        guard router?.take(slot) == true else { return }
        if let surface { deck?.focus(surface) } else { focused = true }
    }
}
