import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class TreeHold {
    private(set) var gate = TreeGate()
    @ObservationIgnored private var hovering = false
    @ObservationIgnored private var acting = 0
    @ObservationIgnored private var leave: Task<Void, Never>?

    static let leaveDelay = Duration.milliseconds(500)
    static let replyGrace = Duration.seconds(1)

    var pending: Int { gate.pending }

    func display(_ node: SDUINode) -> SDUINode {
        gate.display(node)
    }

    func receive(_ node: SDUINode) {
        if gate.receive(node) { land() }
    }

    func gone() {
        gate.reset()
    }

    func hover(_ inside: Bool) {
        hovering = inside
        leave?.cancel()
        if inside {
            if !gate.holding, acting == 0 { gate.arm() }
            return
        }
        leave = Task { [weak self] in
            try? await Task.sleep(for: Self.leaveDelay)
            guard !Task.isCancelled, let self, !self.hovering else { return }
            self.gate.release()
        }
    }

    func land() {
        gate.release()
        if hovering, acting == 0 { gate.arm() }
    }

    func act() {
        acting += 1
        gate.release()
    }

    func acted() async {
        try? await Task.sleep(for: Self.replyGrace)
        acting -= 1
        if acting == 0, hovering, !gate.holding { gate.arm() }
    }
}

private struct TreeHoldKey: EnvironmentKey {
    static let defaultValue: TreeHold? = nil
}

extension EnvironmentValues {
    var treeHold: TreeHold? {
        get { self[TreeHoldKey.self] }
        set { self[TreeHoldKey.self] = newValue }
    }
}

struct HeldTree<Content: View>: View {
    let node: SDUINode
    let hold: TreeHold
    @ViewBuilder let content: (SDUINode) -> Content

    var body: some View {
        content(hold.display(node))
            .onChange(of: node, initial: true) { _, value in
                hold.receive(value)
            }
            .onDisappear { hold.gone() }
    }
}

struct PointerInside: NSViewRepresentable {
    let changed: (Bool) -> Void

    func makeNSView(context: Context) -> TrackingView {
        let view = TrackingView()
        view.changed = changed
        return view
    }

    func updateNSView(_ view: TrackingView, context: Context) {
        view.changed = changed
    }

    final class TrackingView: NSView {
        var changed: (Bool) -> Void = { _ in }
        private var area: NSTrackingArea?

        override func updateTrackingAreas() {
            super.updateTrackingAreas()
            track()
        }

        override func setFrameSize(_ size: NSSize) {
            super.setFrameSize(size)
            track()
        }

        private func track() {
            if let area, area.rect == bounds { return }
            if let area { removeTrackingArea(area) }
            let fresh = NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeAlways], owner: self, userInfo: nil)
            addTrackingArea(fresh)
            area = fresh
        }

        override func hitTest(_ point: NSPoint) -> NSView? { nil }

        override func mouseEntered(with event: NSEvent) { changed(true) }

        override func mouseExited(with event: NSEvent) { changed(false) }
    }
}

struct HoldPill: View {
    let hold: TreeHold

    @State private var hovering = false
    @Environment(\.theme) private var theme

    static let nodeID = "sessions.hold"

    var body: some View {
        if hold.pending > 0 {
            let label = "\(hold.pending) to show"
            Button { hold.land() } label: {
                Text(label)
                    .font(theme.uiSmall)
                    .monospacedDigit()
                    .lineLimit(1)
                    .foregroundStyle(theme.accentInk)
                    .padding(theme.spacing.badge)
                    .background(Capsule().fill(hovering ? theme.toneBorder(theme.accent) : theme.toneBg(theme.accent)))
                    .fixedSize()
                    .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .onHover { hovering = $0 }
            .help("Show the changes held while the pointer is on the list")
            .accessibilityLabel(label)
            .testNode(Self.nodeID, role: "button", label: label)
        }
    }
}

