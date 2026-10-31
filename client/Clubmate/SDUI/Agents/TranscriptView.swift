import os
import SwiftUI

import ClubmateKit

struct TranscriptNodeView: View {
    let node: SDUINode

    @State private var position = ScrollPosition(edge: .bottom)
    @State private var pinned = true
    @State private var watchUntil = ContinuousClock.now + .seconds(3)
    @Environment(\.theme) private var theme

    var body: some View {
        let slack = theme.rowHeight
        let items = node.transcriptItems()
        ScrollView {
            LazyVStack(alignment: .leading, spacing: theme.gutter + theme.spacing.s) {
                ForEach(items) { item in
                    TranscriptItemView(item: item)
                }
            }
            .padding(.horizontal, theme.gutter * 2)
            .padding(.vertical, theme.gutter + theme.spacing.s)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .onScrollGeometryChange(for: TranscriptScroll.self) { geometry in
            TranscriptScroll(offset: geometry.contentOffset.y, pinned: geometry.contentOffset.y + geometry.containerSize.height >= geometry.contentSize.height - slack)
        } action: { _, scroll in
            pinned = scroll.pinned
            if let id = node.id { TranscriptScroll.saved[id] = scroll }
        }
        .onScrollGeometryChange(for: TranscriptGeometry.self) { geometry in
            TranscriptGeometry(offset: geometry.contentOffset.y, content: geometry.contentSize.height, container: geometry.containerSize.height)
        } action: { _, geometry in
            guard watching || geometry.pastEnd else { return }
            transcriptLog.notice("geometry \(node.id ?? "", privacy: .public) offset=\(geometry.offset, format: .fixed(precision: 0)) content=\(geometry.content, format: .fixed(precision: 0)) container=\(geometry.container, format: .fixed(precision: 0)) pastEnd=\(geometry.pastEnd)")
        }
        .onChange(of: node.children) {
            if watching { transcriptLog.notice("children \(node.id ?? "", privacy: .public) items=\(items.count) pinned=\(pinned)") }
            if pinned { position.scrollTo(edge: .bottom) }
            ToolExpansion.shared.prune(node)
        }
        .onChange(of: ToolExpansion.shared.opened) { _, opened in
            guard let opened, let last = items.last, last.contains(opened) else { return }
            Task { position.scrollTo(edge: .bottom) }
        }
        .onAppear {
            watchUntil = .now + .seconds(3)
            let saved = node.id.flatMap { TranscriptScroll.saved[$0] }
            let savedOffset = saved?.offset ?? 0
            transcriptLog.notice("appear \(node.id ?? "", privacy: .public) items=\(items.count) savedPinned=\(saved?.pinned ?? true) savedOffset=\(savedOffset, format: .fixed(precision: 0))")
            guard let saved, !saved.pinned else { return }
            pinned = false
            position.scrollTo(y: saved.offset)
        }
        .onDisappear {
            transcriptLog.notice("disappear \(node.id ?? "", privacy: .public)")
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .clipped()
    }

    private var watching: Bool {
        ContinuousClock.now < watchUntil
    }
}

private struct TranscriptItemView: View {
    let item: TranscriptItem

    var body: some View {
        switch item {
        case let .node(child): SDUINodeView(node: child.node)
        case let .action(child): ToolActionLine(id: child.id, node: child.node, expanded: ToolExpansion.shared.state.isExpanded(card: child.id)).equatable()
        case let .actions(_, cards): ToolActionsView(cards: cards)
        }
    }
}

private extension TranscriptItem {
    func contains(_ id: String) -> Bool {
        if case let .actions(_, cards) = self, cards.contains(where: { $0.id == id }) { return true }
        return self.id == id
    }
}

private let transcriptLog = Logger(subsystem: Diagnostics.logSubsystem, category: "transcript")

private struct TranscriptGeometry: Equatable {
    let offset: CGFloat
    let content: CGFloat
    let container: CGFloat

    var pastEnd: Bool {
        content > container && offset > content - container + 1
    }
}

private struct TranscriptScroll: Equatable {
    let offset: CGFloat
    let pinned: Bool

    @MainActor static var saved: [String: TranscriptScroll] = [:]
}
