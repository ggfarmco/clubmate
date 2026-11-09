import AppKit
import Observation
import SwiftUI

import ClubmateKit

@MainActor @Observable final class GraphViews {
    static let shared = GraphViews()

    private(set) var state = GraphViewState()

    func zoom(_ id: String, steps: Int, from current: CGFloat) {
        state.zoom(id, steps: steps, from: current)
    }

    func setZoom(_ id: String, _ zoom: CGFloat) {
        state.setZoom(id, zoom)
    }

    func fit(_ id: String) {
        state.fit(id)
    }

    func toggleHistory(_ id: String, default initial: Bool = false) {
        Motion.disclose { state.toggleHistory(id, default: initial) }
    }
}

struct GraphNodeView: View {
    let node: SDUINode

    @State private var cursor: String?
    @State private var edgeCursor: Int?
    @State private var hovered: FlowHop?
    @State private var width: CGFloat = 0
    @State private var pinch: CGFloat?
    @FocusState private var focused: Bool
    @Environment(\.isFocused) private var paneFocused
    @Environment(\.paneFocusOwner) private var focusOwner
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    private var id: String { node.id ?? "" }
    private var views: GraphViews { .shared }

    var body: some View {
        let graph = node.graph() ?? SDUIGraph(nodes: [], edges: [])
        let flow = graph.flow
        let fit = GraphViewState.fit(width: width, content: FlowFrame(flow: flow, theme: theme).width)
        let zoom = scale(fit: fit)
        let frame = FlowFrame(flow: flow, theme: theme.scaled(theme.uiSize * zoom))
        let edges = frame.edgeOrder(flow)
        let open = views.state.historyOpen(id)
        let gutter = theme.gutter
        OwnScroll {
            VStack(alignment: .leading, spacing: theme.spacing.s) {
                zoomBar(zoom: zoom, fit: fit)
                ScrollViewReader { proxy in
                    ScrollView(.horizontal) {
                        FlowDiagram(flow: flow, frame: frame, cursor: focused ? cursor : nil, edgeCursor: focused ? edgeCursor : nil, hovered: hovered, opens: !graph.isStatic) { card in
                            focused = true
                            edgeCursor = nil
                            cursor = card
                            if !graph.isStatic { dispatch(node, .named("open", .string(card))) }
                        } openEdge: { edge in
                            focused = true
                            edgeCursor = edge
                            FlowKeys.openEdge(flow, edge) { dispatch(node, $0) }
                        }
                        .environment(\.theme, frame.theme)
                    }
                    .scrollIndicators(.automatic)
                    .clipped()
                    .background(WheelZoom { factor in views.setZoom(id, zoom * factor) })
                    .gesture(
                        MagnifyGesture()
                            .onChanged { pinch = $0.magnification }
                            .onEnded { value in
                                views.setZoom(id, (views.state.zoom(id) ?? fit) * value.magnification)
                                pinch = nil
                            }
                    )
                    .onChange(of: cursor) { _, card in
                        if let card { withAnimation(Motion.disclosure) { proxy.scrollTo(card) } }
                    }
                }
                let aside = !node.children.isEmpty
                let beside = aside && open && !graph.moves.isEmpty && width >= 800
                if aside && !beside {
                    GraphAside(node: node)
                }
                if !graph.moves.isEmpty {
                    let lanes = graph.lanes
                    LanesRow(title: lanes.title ?? "History", moves: graph.moves.count, open: open) { views.toggleHistory(id) }
                    if open {
                        HStack(alignment: .top, spacing: theme.spacing.l) {
                            LanesView(lanes: lanes, k: frame.k, headerWidth: frame.metrics.cardWidth * 0.75, hovered: $hovered, cursor: nil) { move in
                                dispatch(node, .named("openMove", .number(Double(move))))
                            }
                            .environment(\.theme, frame.theme)
                            if beside {
                                GraphAside(node: node)
                                    .frame(width: max(320, width * 0.4))
                            }
                        }
                    }
                }
            }
            .padding(.horizontal, theme.gutter)
            .frame(maxWidth: .infinity, alignment: .topLeading)
            .onGeometryChange(for: CGFloat.self) { $0.size.width - 2 * gutter } action: { width = $0 }
            .focusable()
            .focusEffectDisabled()
            .focused($focused)
            .keyMonitor(active: focused) { handle($0, flow: flow, history: !graph.moves.isEmpty, opens: !graph.isStatic, order: frame.order, edges: edges, zoom: zoom) }
            .reportsFocus(.graph(history: !graph.moves.isEmpty, edges: !edges.isEmpty, output: flow.cards.contains { $0.isStep && $0.id == cursor }, opens: !graph.isStatic), focused)
            .onChange(of: paneFocused) { _, value in
                if value, focusOwner == nil || focusOwner == node.id { focused = true }
            }
            .onChange(of: focused) { _, value in
                guard value, cursor.map(frame.order.contains) != true else { return }
                cursor = flow.cards.first { $0.attention && frame.order.contains($0.id) }?.id ?? frame.order.first
            }
        }
    }

    private func scale(fit: CGFloat) -> CGFloat {
        let base = views.state.zoom(id) ?? fit
        return pinch.map { GraphViewState.clamp(base * $0) } ?? base
    }

    private func zoomBar(zoom: CGFloat, fit: CGFloat) -> some View {
        HStack(spacing: theme.spacing.xs) {
            Spacer(minLength: 0)
            GraphZoomButton(label: "−", help: "Zoom out  -") { views.zoom(id, steps: -1, from: zoom) }
            Text("\(Int((zoom * 100).rounded()))%")
                .font(theme.uiSmall.monospacedDigit())
                .foregroundStyle(theme.muted)
                .padding(.horizontal, theme.spacing.s)
                .testNode("\(id)/zoom", role: "zoom", label: "\(Int((zoom * 100).rounded()))%")
            GraphZoomButton(label: "+", help: "Zoom in  +") { views.zoom(id, steps: 1, from: zoom) }
            GraphZoomButton(label: "Fit", help: "Fit to width  0") { views.fit(id) }
        }
    }

    private func handle(_ event: NSEvent, flow: SDUIFlow, history: Bool, opens: Bool, order: [String], edges: [Int], zoom: CGFloat) -> Bool {
        guard event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return false }
        if !opens, edgeCursor == nil, event.keyCode == 36 || event.keyCode == 76 {
            return true
        }
        if FlowKeys.handle(event, flow: flow, order: order, edges: edges, cursor: &cursor, edgeCursor: &edgeCursor, send: { dispatch(node, $0) }) {
            return true
        }
        switch event.characters {
        case "-": views.zoom(id, steps: -1, from: zoom)
        case "+", "=": views.zoom(id, steps: 1, from: zoom)
        case "0": views.fit(id)
        case "h" where history: views.toggleHistory(id)
        case "o":
            guard let cursor, flow.cards.contains(where: { $0.isStep && $0.id == cursor }) else { return false }
            dispatch(node, .named("output", .string(cursor)))
        default: return false
        }
        return true
    }
}

private struct GraphAside: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            SDUIChildren(node: node)
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
    }
}

struct FlowNodeView: View {
    let node: SDUINode

    @State private var cursor: String?
    @State private var edgeCursor: Int?
    @FocusState private var focused: Bool
    @Environment(\.isFocused) private var paneFocused
    @Environment(\.paneFocusOwner) private var focusOwner
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        let flow = node.flow() ?? SDUIFlow(cards: [], edges: [])
        let frame = FlowFrame(flow: flow, theme: theme)
        let edges = frame.edgeOrder(flow)
        ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                FlowDiagram(flow: flow, frame: frame, cursor: focused ? cursor : nil, edgeCursor: focused ? edgeCursor : nil, hovered: nil) { card in
                    focused = true
                    edgeCursor = nil
                    cursor = card
                    dispatch(node, .named("open", .string(card)))
                } openEdge: { edge in
                    focused = true
                    edgeCursor = edge
                    FlowKeys.openEdge(flow, edge) { dispatch(node, $0) }
                }
            }
            .scrollIndicators(.automatic)
            .clipped()
            .onChange(of: cursor) { _, card in
                if let card { withAnimation(Motion.disclosure) { proxy.scrollTo(card) } }
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .keyMonitor(active: focused) { event in
            guard event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return false }
            return FlowKeys.handle(event, flow: flow, order: frame.order, edges: edges, cursor: &cursor, edgeCursor: &edgeCursor) { dispatch(node, $0) }
        }
        .reportsFocus(.graph(history: false, edges: !edges.isEmpty), focused)
        .onChange(of: paneFocused) { _, value in
            if value, focusOwner == nil || focusOwner == node.id { focused = true }
        }
        .onChange(of: focused) { _, value in
            guard value, cursor.map(frame.order.contains) != true else { return }
            cursor = flow.cards.first { $0.attention && frame.order.contains($0.id) }?.id ?? frame.order.first
        }
    }
}

enum FlowKeys {
    static func handle(_ event: NSEvent, flow: SDUIFlow, order: [String], edges: [Int], cursor: inout String?, edgeCursor: inout Int?, send: (UIAction.Trigger) -> Void) -> Bool {
        if event.keyCode == 36 || event.keyCode == 76 {
            if let edgeCursor {
                return openEdge(flow, edgeCursor, send: send)
            }
            guard let cursor else { return false }
            send(.named("open", .string(cursor)))
            return true
        }
        if event.keyCode == 48 {
            guard !edges.isEmpty else { return false }
            let step = event.modifierFlags.contains(.shift) ? -1 : 1
            let index = edgeCursor.flatMap(edges.firstIndex) ?? (step > 0 ? -1 : edges.count)
            edgeCursor = edges[(index + step + edges.count) % edges.count]
            return true
        }
        switch event.characters {
        case "j", "k":
            guard !order.isEmpty else { return true }
            let step = event.characters == "j" ? 1 : -1
            let index = cursor.flatMap(order.firstIndex) ?? (step > 0 ? -1 : order.count)
            cursor = order[min(max(index + step, 0), order.count - 1)]
            edgeCursor = nil
            return true
        default:
            return false
        }
    }

    @discardableResult
    static func openEdge(_ flow: SDUIFlow, _ edge: Int, send: (UIAction.Trigger) -> Void) -> Bool {
        guard flow.edges.indices.contains(edge), let ref = flow.edges[edge].ref else { return false }
        send(.named("openEdge", .string(ref)))
        return true
    }
}

private struct GraphZoomButton: View {
    let label: String
    let help: String
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(theme.uiSmall)
                .foregroundStyle(hovering ? theme.foreground : theme.text2)
                .padding(.horizontal, theme.spacing.m)
                .frame(minWidth: theme.rowHeight, minHeight: theme.rowHeight)
                .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(hovering ? theme.hover : .clear))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .focusable(false)
        .onHover { hovering = $0 }
        .help(help)
        .accessibilityLabel(help)
    }
}

private struct WheelZoom: NSViewRepresentable {
    let zoom: @MainActor (CGFloat) -> Void

    func makeNSView(context: Context) -> WheelZoomView {
        WheelZoomView()
    }

    func updateNSView(_ view: WheelZoomView, context: Context) {
        view.zoom = zoom
    }
}

final class WheelZoomView: NSView {
    var zoom: (@MainActor (CGFloat) -> Void)?

    private var monitor: Any?

    override func hitTest(_ point: NSPoint) -> NSView? {
        nil
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil { return remove() }
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { [weak self] event in
            let consumed = MainActor.assumeIsolated { self?.deliver(event) == true }
            return consumed ? nil : event
        }
    }

    override func viewWillMove(toWindow newWindow: NSWindow?) {
        super.viewWillMove(toWindow: newWindow)
        if newWindow == nil { remove() }
    }

    private func deliver(_ event: NSEvent) -> Bool {
        guard let window, event.window === window, !event.modifierFlags.intersection([.command, .control]).isEmpty, bounds.contains(convert(event.locationInWindow, from: nil)) else { return false }
        let delta = event.scrollingDeltaY
        if delta != 0 {
            zoom?(event.hasPreciseScrollingDeltas ? exp(delta / 200) : delta > 0 ? GraphViewState.step : 1 / GraphViewState.step)
        }
        return true
    }

    private func remove() {
        guard let monitor else { return }
        NSEvent.removeMonitor(monitor)
        self.monitor = nil
    }
}
