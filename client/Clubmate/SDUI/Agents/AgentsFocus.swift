import AppKit
import SwiftUI

import ClubmateKit

@MainActor
struct AgentsFocus {
    let store: WorkspaceUIStore
    let panes: PaneFocus
    let router: FocusRouter
    let side: SidebarSide
    let commands: [Command]

    var layout: AgentsLayout {
        AgentsLayout(main: store.slot(AgentsSection.mainSlot)?.tree, aside: store.asideShown ? store.slot(AgentsSection.asideSlot)?.tree : nil, lastComposer: panes.typedIn, side: side)
    }

    func apply(_ move: AgentsMove) {
        switch move {
        case .focus(.tree): router.request(AgentsSection.sidebarSlot)
        case .focus(.composer(let column)): router.request(column.slot)
        case .focus(.transcript(let column)): router.request(column.slot + AgentsKeys.transcriptSuffix)
        case .note(let text): panes.flash(text)
        case .stay: break
        }
    }

    func go(_ chord: TreeChord, from column: AgentsColumn) {
        let invocation: CommandInvocation?
        switch chord {
        case .jump(let n): invocation = AgentsSection.jump(n, section: AgentsSection.id, commands: commands)
        case .needs(let by): invocation = AgentsSection.needs(by, section: AgentsSection.id, commands: commands)
        case .last: invocation = AgentsSection.last(section: AgentsSection.id, commands: commands)
        case .tree: return apply(.focus(.tree))
        case .composer: return apply(AgentsKeys.move(.composer, from: .transcript(column), in: layout))
        case .focusField: return router.request(AgentsSection.sidebarSlot + SidebarField.suffix)
        }
        guard let invocation else { return }
        Task { [store, panes] in
            if case .failure(let error) = await store.run(invocation) { panes.flash(error.message) }
        }
    }

    func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        guard let chord = AgentsChord.resolve(keyCode: event.keyCode, command: flags.contains(.command), control: flags.contains(.control), option: flags.contains(.option), shift: flags.contains(.shift)), let window = event.window else { return false }
        let responder = window.firstResponder
        if (responder as? NSTextInputClient)?.hasMarkedText() == true { return false }
        if AgentsChord.control(keyCode: event.keyCode, command: flags.contains(.command), control: flags.contains(.control), option: flags.contains(.option), shift: flags.contains(.shift)),
           (responder as? NSTextView)?.isFieldEditor == true, panes.pane != .field || panes.slot != AgentsSection.sidebarSlot + SidebarField.suffix || chord == .column(-1) || chord == .column(1) { return false }
        if chord == .keys {
            panes.keysShown.toggle()
            return true
        }
        if chord == .stepOut, panes.keysShown || panes.whichKey != nil {
            panes.keysShown = false
            panes.whichKey = nil
            return true
        }
        let text = responder as? NSTextView
        let layout = layout
        var place: AgentsPlace?
        switch panes.pane {
        case .tree:
            place = .tree
        case .composer:
            place = AgentsColumn(slot: panes.slot).map(AgentsPlace.composer)
        case .transcript:
            place = AgentsColumn(slot: panes.slot).map(AgentsPlace.transcript)
        case .field:
            if panes.slot == AgentsSection.sidebarSlot + SidebarField.suffix {
                if chord == .stepOut, let text, !text.string.isEmpty {
                    text.selectAll(nil)
                    text.deleteBackward(nil)
                    return true
                }
                place = .tree
                if chord == .stepOut {
                    apply(.focus(.tree))
                    return true
                }
            } else if let column = column(of: responder) {
                place = .transcript(column)
                if chord == .stepOut {
                    apply(.focus(layout.transcripts.contains(column) ? .transcript(column) : .tree))
                    return true
                }
            }
        case nil:
            if text?.isEditable == true { return false }
        default:
            break
        }
        var running = false
        if case .composer(running: true, queues: _, steps: _) = panes.pane { running = chord == .stepOut }
        apply(AgentsKeys.move(chord, from: place, in: layout))
        if running, StopHint().show() { panes.flash(StopHint.text) }
        return true
    }

    private func column(of responder: NSResponder?) -> AgentsColumn? {
        [AgentsColumn.main, .aside].first { PaneMarker.holds($0.slot, responder) }
    }
}

extension EnvironmentValues {
    @Entry var agentsFocus: AgentsFocus?
}

struct AgentsKeysSink: ViewModifier {
    let focus: AgentsFocus?

    func body(content: Content) -> some View {
        content
            .environment(\.agentsFocus, focus)
            .keyMonitor(active: focus != nil, fallback: true) { event in focus?.handle(event) ?? false }
            .overlay(alignment: .bottomTrailing) {
                if let panes = focus?.panes, panes.keysShown {
                    PaneKeysCard(pane: panes.mode ?? "AGENTS", groups: PaneKeysHelp.groups(panes.pane)) { panes.keysShown = false }
                }
            }
    }
}

private struct PaneKeysCard: View {
    let pane: String
    let groups: [ShortcutGroup]
    let close: () -> Void

    @Environment(\.theme) private var theme

    private static let width: CGFloat = 420

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: theme.spacing.m) {
                Text("Keys · \(pane)")
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
                Spacer(minLength: theme.gutter)
                KeyCapLabel(label: "⎋")
                Button(action: close) {
                    Image(systemName: "xmark")
                        .font(theme.glyph)
                        .foregroundStyle(theme.muted)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Close keys")
            }
            .padding(theme.gutter)
            theme.border.frame(height: 1)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: theme.spacing.s) {
                    ForEach(groups) { group in
                        Text(group.title)
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.muted)
                            .padding(.top, theme.spacing.m)
                        ForEach(group.shortcuts) { shortcut in
                            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                                Text(shortcut.labels.joined(separator: " "))
                                    .font(theme.mono)
                                    .foregroundStyle(theme.accentInk)
                                    .lineLimit(1)
                                    .frame(width: theme.indent * 7, alignment: .leading)
                                Text(shortcut.action)
                                    .font(theme.ui)
                                    .foregroundStyle(theme.text2)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                        }
                    }
                }
                .padding(.horizontal, theme.gutter)
                .padding(.bottom, theme.gutter)
            }
            .frame(maxHeight: theme.rowHeight * 14)
            .clipped()
        }
        .frame(width: Self.width, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.surface3))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.l).strokeBorder(theme.borderStrong))
        .padding(theme.spacing.xl)
        .padding(.bottom, theme.rowHeight)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("agents.keys")
    }
}

struct ResponderInside: NSViewRepresentable {
    let changed: (Bool) -> Void

    func makeNSView(context: Context) -> ResponderInsideView {
        ResponderInsideView()
    }

    func updateNSView(_ view: ResponderInsideView, context: Context) {
        view.changed = changed
    }
}

final class ResponderInsideView: NSView {
    var changed: ((Bool) -> Void)?
    private var observation: NSKeyValueObservation?
    private var inside = false

    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        observation = window?.observe(\.firstResponder, options: [.new]) { [weak self] _, _ in
            MainActor.assumeIsolated { self?.check() }
        }
        check()
    }

    private func check() {
        let now = holds(window?.firstResponder)
        guard now != inside else { return }
        inside = now
        changed?(now)
    }

    private func holds(_ responder: NSResponder?) -> Bool {
        guard let text = responder as? NSTextView, !text.isEditable, let window, text.window === window, !isHiddenOrHasHiddenAncestor else { return false }
        let frame = text.convert(text.visibleRect, to: nil)
        return convert(bounds, to: nil).contains(NSPoint(x: frame.midX, y: frame.midY))
    }
}
