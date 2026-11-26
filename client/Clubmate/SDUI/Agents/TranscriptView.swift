import AppKit
import os
import SwiftUI

import ClubmateKit

struct TranscriptNodeView: View {
    let node: SDUINode

    @State private var position = ScrollPosition(edge: .bottom)
    @State private var pinned = true
    @State private var watchUntil = ContinuousClock.now + .seconds(3)
    @Environment(\.theme) private var theme
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.composerDrafts) private var drafts
    @Environment(\.composerFocusSlot) private var focusSlot
    @Environment(\.focusRouter) private var router
    @Environment(\.agentsFocus) private var agentsFocus
    @FocusState private var focused: Bool
    @State private var inside = false
    @State private var page = TranscriptPage()
    @State private var cursor: String?
    @Environment(\.paneFocus) private var panes

    private var column: AgentsColumn? { agentsFocus == nil ? nil : AgentsColumn(slot: focusSlot) }
    private var reading: Bool { column != nil && (focused || inside) }

    private var ownsFocus: Bool {
        guard let focused = drafts?.focused, let id = node.id else { return true }
        return ComposerDrafts.session(of: focused) == ComposerDrafts.session(of: id)
    }

    private var asideQuote: AsideQuote? {
        guard let action = node.string("asideAction") else { return nil }
        return AsideQuote { [dispatch] message, quote in dispatch(message, .named(action, .object(["quote": .string(quote)]))) }
    }

    private var docked: SDUINode? {
        guard let agentsFocus, let column else { return nil }
        return TranscriptBlocks.docked(in: agentsFocus.store.slot(column.slot)?.tree)
    }

    private var blocks: [TranscriptBlock] {
        TranscriptBlocks.blocks(node, folds: TranscriptFolding.folds, docked: docked)
    }

    var body: some View {
        let slack = theme.rowHeight
        let items = node.transcriptItems()
        let bar = TranscriptBar.shared.block
        let fullDiff = node.fullDiffCard(focused: bar)
        let echoes = drafts?.echoes.echoes(session: ComposerDrafts.session(of: node.id ?? "")) ?? []
        ScrollView {
            VStack(alignment: .leading, spacing: theme.gutter + theme.spacing.s) {
                ForEach(items) { item in
                    TranscriptItemView(item: item).equatable()
                        .overlay(alignment: .leading) {
                            if bar == item.id { BlockBar(id: item.id) }
                        }
                        .simultaneousGesture(TapGesture().onEnded { pick(item.id) })
                }
                ForEach(echoes) { echo in
                    SDUINodeView(node: SDUINode(type: "message", id: echo.id, props: ["role": .string("user"), "text": .string(echo.text), "sending": .bool(true)])).equatable()
                }
            }
            .padding(.horizontal, theme.gutter * 2)
            .padding(.vertical, theme.gutter + theme.spacing.s)
            .environment(\.inTranscript, true)
            .environment(\.transcriptReveal, TranscriptReveal { id in Motion.disclose { position.scrollTo(id: id, anchor: .center) } })
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .defaultScrollAnchor(node.bool("center") == true ? .center : .bottom, for: .alignment)
        .onScrollGeometryChange(for: TranscriptScroll.self) { geometry in
            TranscriptScroll(offset: geometry.contentOffset.y, pinned: geometry.contentOffset.y + geometry.containerSize.height >= geometry.contentSize.height - slack)
        } action: { _, scroll in
            if pinned != scroll.pinned { pinned = scroll.pinned }
            if let id = node.id { TranscriptScroll.saved[id] = scroll }
        }
        .onScrollGeometryChange(for: TranscriptFit.self) { geometry in
            TranscriptFit(container: geometry.containerSize.height, pinned: geometry.contentOffset.y + geometry.containerSize.height >= geometry.contentSize.height - slack)
        } action: { old, new in
            guard new.container < old.container, old.pinned else { return }
            pinned = true
            position.scrollTo(edge: .bottom)
        }
        .onScrollGeometryChange(for: TranscriptGeometry.self) { geometry in
            TranscriptGeometry(offset: geometry.contentOffset.y, content: geometry.contentSize.height, container: geometry.containerSize.height)
        } action: { _, geometry in
            page.geometry = geometry
            guard watching || geometry.pastEnd else { return }
            transcriptLog.notice("geometry \(node.id ?? "", privacy: .public) offset=\(geometry.offset, format: .fixed(precision: 0)) content=\(geometry.content, format: .fixed(precision: 0)) container=\(geometry.container, format: .fixed(precision: 0)) pastEnd=\(geometry.pastEnd)")
        }
        .onChange(of: node.children) { old, _ in
            if watching { transcriptLog.notice("children \(node.id ?? "", privacy: .public) items=\(items.count) pinned=\(pinned)") }
            ToolExpansion.shared.prune(node)
            MessageExpansion.shared.prune(node)
            guard follow(from: old) else { return }
            if pinned { position.scrollTo(edge: .bottom) }
        }
        .onChange(of: echoes.count) { old, new in
            guard new > old else { return }
            pinned = true
            position.scrollTo(edge: .bottom)
        }
        .onChange(of: ToolExpansion.shared.opened) { _, opened in
            guard let opened, let last = items.last, last.contains(opened) else { return }
            Task { Motion.disclose { position.scrollTo(edge: .bottom) } }
        }
        .onChange(of: TranscriptAnchor(node)) {
            scrollToAnchor()
        }
        .onAppear {
            watchUntil = .now + .seconds(3)
            let saved = node.id.flatMap { TranscriptScroll.saved[$0] }
            let savedOffset = saved?.offset ?? 0
            transcriptLog.notice("appear \(node.id ?? "", privacy: .public) items=\(items.count) savedPinned=\(saved?.pinned ?? true) savedOffset=\(savedOffset, format: .fixed(precision: 0))")
            if scrollToAnchor() { return }
            guard let saved, !saved.pinned else { return }
            pinned = false
            position.scrollTo(y: saved.offset)
        }
        .onDisappear {
            transcriptLog.notice("disappear \(node.id ?? "", privacy: .public)")
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .clipped()
        .environment(\.fullDiffCard, fullDiff?.id)
        .environment(\.asideQuote, asideQuote)
        .keyMonitor(active: asideQuote != nil) { event in
            guard let asideQuote, AsideKey.matches(event) else { return false }
            if let prose = NSApp.keyWindow?.firstResponder as? ProseTextView, prose.askAside == nil { return false }
            guard ownsFocus else { return false }
            if let prose = NSApp.keyWindow?.firstResponder as? ProseTextView, let ask = prose.askAside, let quote = prose.selectedQuote {
                ask(quote)
                return true
            }
            if reading, let cursor, let block = node.transcriptChild(cursor), block.type == "message", block.messageRole == .agent, let text = block.string("text") {
                asideQuote.ask(block, text)
                return true
            }
            guard let last = node.lastAgentMessage, let text = last.string("text") else { return false }
            asideQuote.ask(last, text)
            return true
        }
        .keyMonitor(active: fullDiff != nil) { event in
            guard let fullDiff, let review = fullDiff.cardActions().first(where: { $0.action == "review" }), FullDiffKey.matches(event) else { return false }
            dispatch(fullDiff, .named(review.action, review.value))
            return true
        }
        .focusable(column != nil)
        .focusEffectDisabled()
        .focused($focused)
        .overlay(alignment: .leading) {
            if reading { theme.accent.frame(width: 2).allowsHitTesting(false) }
        }
        .background {
            if column != nil { ResponderInside { inside = $0 } }
        }
        .reportsFocus(.transcript, reading, slot: focusSlot)
        .keyMonitor(active: reading, fallback: true, handle: read)
        .keyMonitor(active: reading, handle: copy)
        .onChange(of: router?.pending, initial: true) {
            guard let column, router?.take(column.slot + AgentsKeys.transcriptSuffix) == true else { return }
            focused = true
        }
        .onChange(of: reading, initial: true) { _, reading in
            if reading, cursor == nil || !blocks.contains(where: { $0.id == cursor }) { cursor = TranscriptCursor.landing(blocks) }
            if !reading { panes?.whichKey = nil }
            showBar()
        }
        .onChange(of: cursor) { showBar() }
        .onDisappear {
            if TranscriptBar.shared.block == cursor { TranscriptBar.shared.block = nil }
        }
    }

    private func showBar() {
        if reading {
            TranscriptBar.shared.block = cursor
        } else if TranscriptBar.shared.block == cursor {
            TranscriptBar.shared.block = nil
        }
    }

    private func pick(_ id: String) {
        guard column != nil, blocks.contains(where: { $0.id == id }) else { return }
        cursor = id
        if !reading { focused = true }
    }

    private func follow(from old: [SDUINode]) -> Bool {
        guard reading else { return true }
        let folds = TranscriptFolding.folds
        let docked = docked
        let before = TranscriptBlocks.blocks(SDUINode(type: node.type, id: node.id, children: old), folds: folds, docked: docked)
        let after = TranscriptBlocks.blocks(node, folds: folds, docked: docked)
        let follow = TranscriptCursor.follow(cursor, from: before, to: after)
        cursor = follow.cursor
        if follow.newer > 0 { panes?.flash(TranscriptNote.newer(follow.newer)) }
        return follow.follows
    }

    private func copy(_ event: NSEvent) -> Bool {
        guard CopyKey.matches(event), let cursor else { return false }
        if let text = event.window?.firstResponder as? NSTextView, text.selectedRange().length > 0 { return false }
        guard let markdown = TranscriptBlocks.markdown(cursor, in: node, docked: docked) else { return false }
        Clipboard.copy(markdown)
        panes?.flash(CopyKey.note, for: .seconds(2))
        return true
    }

    private func step(_ step: TranscriptStep, column: AgentsColumn) {
        let blocks = blocks
        guard let command = TranscriptCursor.resolve(step, cursor: cursor, in: blocks) else { return }
        switch command {
        case .cursor(let id):
            reveal(id)
        case .expand(let id):
            if let block = blocks.first(where: { $0.id == id }) { TranscriptFolding.set(block, expanded: true, in: node) }
        case .collapse(let id, let to):
            if let block = blocks.first(where: { $0.id == id }) { TranscriptFolding.set(block, expanded: false, in: node) }
            reveal(to)
        case .press(let id):
            if id == docked?.id {
                router?.request(column.slot + AgentsKeys.cardSuffix)
            } else if let button = node.transcriptChild(id), button.type == "button" {
                dispatch(button, .click)
            }
        case .top(let id, let earlier):
            if let earlier, let button = node.transcriptChild(earlier) { dispatch(button, .click) }
            pinned = false
            cursor = id
            position.scrollTo(id: scrollTarget(id), anchor: .top)
        case .bottom(let id):
            cursor = id
            pinned = true
            position.scrollTo(edge: .bottom)
        }
    }

    private func reveal(_ id: String) {
        cursor = id
        guard id != docked?.id else { return }
        position.scrollTo(id: scrollTarget(id))
    }

    private func scrollTarget(_ id: String) -> String {
        node.transcriptItems().first { $0.contains(id) }?.id ?? id
    }

    private func chord(_ chord: TranscriptChord?, column: AgentsColumn, focus: AgentsFocus) {
        switch chord {
        case .first: step(.first, column: column)
        case .file: openFile()
        case .go(let go): focus.go(go, from: column)
        case nil: break
        }
    }

    private func openFile() {
        guard let cursor, let block = node.transcriptChild(cursor), let action = TranscriptBlocks.file(block) else {
            panes?.flash(TranscriptNote.noFile, for: .seconds(2))
            return
        }
        dispatch(block, .named(action.action, action.value))
    }

    private func read(_ event: NSEvent) -> Bool {
        if (event.window?.firstResponder as? NSTextView)?.isEditable == true { return false }
        let flags = event.modifierFlags
        guard let column, let agentsFocus else { return false }
        if panes?.whichKey != nil {
            panes?.whichKey = nil
            guard flags.intersection([.command, .control, .option]).isEmpty else { return false }
            chord(TranscriptChord.resolve(event.keyCode, shift: flags.contains(.shift)), column: column, focus: agentsFocus)
            return true
        }
        guard let key = TranscriptKey.resolve(keyCode: event.keyCode, characters: event.charactersIgnoringModifiers, command: flags.contains(.command), control: flags.contains(.control), option: flags.contains(.option), shift: flags.contains(.shift)) else { return false }
        switch key {
        case .type: agentsFocus.apply(.focus(.composer(column)))
        case .column(let delta): agentsFocus.apply(AgentsKeys.column(delta, from: .transcript(column), in: agentsFocus.layout))
        case .page(let delta):
            let geometry = page.geometry
            let end = max(geometry.content - geometry.container, 0)
            position.scrollTo(y: min(max(geometry.offset + CGFloat(delta) * geometry.container * 0.9, 0), end))
        case .step(let move):
            step(move, column: column)
        case .leader:
            panes?.whichKey = TranscriptChord.whichKey(file: blocks.first { $0.id == cursor }?.file == true)
        case .help:
            panes?.keysShown.toggle()
        }
        return true
    }

    private var watching: Bool {
        ContinuousClock.now < watchUntil
    }

    @discardableResult
    private func scrollToAnchor() -> Bool {
        guard let id = node.id else { return false }
        guard let anchor = TranscriptAnchor(node) else {
            TranscriptScroll.anchors[id] = nil
            return false
        }
        guard TranscriptScroll.anchors[id] != anchor else { return false }
        TranscriptScroll.anchors[id] = anchor
        pinned = false
        let target = node.transcriptItems().first { $0.contains(anchor.id) }?.id ?? anchor.id
        Task { position.scrollTo(id: target, anchor: .top) }
        return true
    }
}

struct TranscriptReveal {
    let show: @MainActor (String) -> Void
}

extension EnvironmentValues {
    @Entry var transcriptReveal: TranscriptReveal?
}

private struct TranscriptItemView: View, Equatable {
    let item: TranscriptItem

    nonisolated static func == (lhs: TranscriptItemView, rhs: TranscriptItemView) -> Bool {
        lhs.item == rhs.item
    }

    var body: some View {
        switch item {
        case let .node(child): SDUINodeView(node: child.node).equatable()
        case let .action(child): ToolActionLine(id: child.id, node: child.node, expanded: ToolExpansion.shared.state.isExpanded(card: child.id)).equatable()
        case let .actions(_, cards): ToolActionsView(cards: cards)
        }
    }
}

extension TranscriptItem {
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

private final class TranscriptPage {
    var geometry = TranscriptGeometry(offset: 0, content: 0, container: 0)
}

private struct TranscriptFit: Equatable {
    let container: CGFloat
    let pinned: Bool
}

private struct TranscriptScroll: Equatable {
    let offset: CGFloat
    let pinned: Bool

    @MainActor static var saved = RecentCache<String, TranscriptScroll>(limit: 32)
    @MainActor static var anchors = RecentCache<String, TranscriptAnchor>(limit: 32)
}

private struct TranscriptAnchor: Equatable {
    let id: String
    let rev: Int

    init?(_ node: SDUINode) {
        guard let id = node.string("anchor") else { return nil }
        self.id = id
        rev = node.props["anchorRev"]?.numberValue.flatMap { Int(exactly: $0) } ?? 0
    }
}

struct AsideQuote {
    let ask: (SDUINode, String) -> Void
}

enum AsideKey {
    static let character = "\\"

    static func matches(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection([.command, .control, .option, .shift])
        return flags == .command && event.charactersIgnoringModifiers == character
    }
}

extension SDUINode {
    var lastAgentMessage: SDUINode? {
        if type == "message", string("role") == "agent" { return self }
        return children.reversed().lazy.compactMap(\.lastAgentMessage).first
    }
}

extension EnvironmentValues {
    @Entry var asideQuote: AsideQuote?
    @Entry var askAside: ((String?) -> Void)?
}

enum CopyKey {
    static let note = "Copied as Markdown"

    static func matches(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection([.command, .control, .option, .shift])
        return flags == .command && event.charactersIgnoringModifiers?.lowercased() == "c"
    }
}

@MainActor
enum FullDiffKey {
    static let label = "⌘D"

    static func matches(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection([.command, .control, .option, .shift])
        guard flags == .command, event.charactersIgnoringModifiers?.lowercased() == "d" else { return false }
        return NSApp.mainMenu.map { !MenuKeys.claims($0, event) } ?? true
    }
}
