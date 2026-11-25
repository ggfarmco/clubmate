import AppKit
import SwiftUI

import ClubmateKit

@MainActor
struct AgentsFocus {
    let store: WorkspaceUIStore
    let panes: PaneFocus
    let router: FocusRouter
    let side: SidebarSide

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

    func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        guard let chord = AgentsChord.resolve(keyCode: event.keyCode, command: flags.contains(.command), control: flags.contains(.control), option: flags.contains(.option), shift: flags.contains(.shift)), let window = event.window else { return false }
        let responder = window.firstResponder
        if (responder as? NSTextInputClient)?.hasMarkedText() == true { return false }
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
