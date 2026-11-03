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

    func toggleHistory(_ id: String) {
        Motion.disclose { state.toggleHistory(id) }
    }
}

private struct GraphHop: Equatable {
    let node: String
    let out: String
}

private struct GraphFrame {
    let theme: Theme
    let metrics: GraphMetrics
    let layout: GraphLayout

    init(graph: SDUIGraph, theme: Theme) {
        self.theme = theme
        let k = theme.uiSize / 12
        let char = ("M" as NSString).size(withAttributes: [.font: theme.nsFont(size: theme.uiSize, bold: true)]).width
        metrics = GraphMetrics(cardWidth: max(210 * k, (20 * char + 44 * k).rounded()), headHeight: 52 * k, rowHeight: 26 * k, layerGap: 120 * k, nodeGap: 24 * k, trackGap: 12 * k)
        layout = GraphLayout(graph: graph, metrics: metrics)
    }

    var k: CGFloat { theme.uiSize / 12 }
    var pad: CGFloat { 4 * k }
    var width: CGFloat { layout.size.width + 2 * pad }
    var height: CGFloat { layout.size.height + 2 * pad }
    var order: [String] { layout.layers.flatMap { $0 } }
}

struct GraphNodeView: View {
    let node: SDUINode

    @State private var cursor: String?
    @State private var hovered: GraphHop?
    @State private var width: CGFloat = 0
    @State private var pinch: CGFloat?
    @FocusState private var focused: Bool
    @Environment(\.isFocused) private var paneFocused
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    private var id: String { node.id ?? "" }
    private var views: GraphViews { .shared }

    var body: some View {
        let graph = node.graph() ?? SDUIGraph(nodes: [], edges: [])
        let fit = GraphViewState.fit(width: width, content: GraphFrame(graph: graph, theme: theme).width)
        let zoom = scale(fit: fit)
        let frame = GraphFrame(graph: graph, theme: theme.scaled(theme.uiSize * zoom))
        let open = views.state.historyOpen(id)
        let gutter = theme.gutter
        OwnScroll {
            VStack(alignment: .leading, spacing: theme.spacing.s) {
                zoomBar(zoom: zoom, fit: fit)
                ScrollViewReader { proxy in
                    ScrollView(.horizontal) {
                        GraphDiagram(graph: graph, frame: frame, cursor: focused ? cursor : nil, hovered: hovered) { card in
                            focused = true
                            cursor = card
                            dispatch(node, .named("open", .string(card)))
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
                if !graph.moves.isEmpty {
                    GraphHistoryRow(moves: graph.moves.count, open: open) { views.toggleHistory(id) }
                    if open {
                        GraphHistory(graph: graph, frame: frame, hovered: $hovered) { move in
                            dispatch(node, .named("openMove", .number(Double(move))))
                        }
                        .environment(\.theme, frame.theme)
                    }
                }
            }
            .padding(.horizontal, theme.gutter)
            .frame(maxWidth: .infinity, alignment: .topLeading)
            .onGeometryChange(for: CGFloat.self) { $0.size.width - 2 * gutter } action: { width = $0 }
            .focusable()
            .focusEffectDisabled()
            .focused($focused)
            .keyMonitor(active: focused) { handle($0, graph: graph, order: frame.order, zoom: zoom) }
            .reportsFocus(.graph(history: !graph.moves.isEmpty), focused)
            .onChange(of: paneFocused) { _, value in
                if value { focused = true }
            }
            .onChange(of: focused) { _, value in
                guard value, cursor.map(frame.order.contains) != true else { return }
                cursor = graph.nodes.first { $0.attention && frame.order.contains($0.id) }?.id ?? frame.order.first
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
            GraphZoomButton(label: "+", help: "Zoom in  +") { views.zoom(id, steps: 1, from: zoom) }
            GraphZoomButton(label: "Fit", help: "Fit to width  0") { views.fit(id) }
        }
    }

    private func handle(_ event: NSEvent, graph: SDUIGraph, order: [String], zoom: CGFloat) -> Bool {
        guard event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return false }
        if event.keyCode == 36 || event.keyCode == 76 {
            guard let cursor else { return false }
            dispatch(node, .named("open", .string(cursor)))
            return true
        }
        switch event.characters {
        case "j", "k":
            guard !order.isEmpty else { return true }
            let step = event.characters == "j" ? 1 : -1
            let index = cursor.flatMap(order.firstIndex) ?? (step > 0 ? -1 : order.count)
            cursor = order[min(max(index + step, 0), order.count - 1)]
        case "-": views.zoom(id, steps: -1, from: zoom)
        case "+", "=": views.zoom(id, steps: 1, from: zoom)
        case "0": views.fit(id)
        case "h" where !graph.moves.isEmpty: views.toggleHistory(id)
        default: return false
        }
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

private struct GraphDiagram: View {
    let graph: SDUIGraph
    let frame: GraphFrame
    let cursor: String?
    let hovered: GraphHop?
    let open: (String) -> Void

    @Environment(\.theme) private var theme

    private struct Slot {
        let node: SDUIGraph.Node
        let rect: CGRect
    }

    private var cards: [Slot] {
        var seen = Set<String>()
        return graph.nodes.compactMap { node in
            guard seen.insert(node.id).inserted, let rect = frame.layout.cards[node.id] else { return nil }
            return Slot(node: node, rect: rect.offsetBy(dx: frame.pad, dy: frame.pad))
        }
    }

    var body: some View {
        let cards = cards
        ZStack(alignment: .topLeading) {
            Canvas { context, _ in
                context.translateBy(x: frame.pad, y: frame.pad)
                drawEdges(&context)
                drawMarkers(&context)
            }
            ForEach(cards, id: \.node.id) { card in
                GraphCard(node: card.node, metrics: frame.metrics, focused: cursor == card.node.id, lit: hovered?.node == card.node.id ? hovered?.out : nil) {
                    open(card.node.id)
                }
                .frame(width: card.rect.width, height: card.rect.height)
                .id(card.node.id)
                .position(x: card.rect.midX, y: card.rect.midY)
            }
            Canvas { context, _ in
                drawPorts(&context, cards: cards)
            }
            .allowsHitTesting(false)
        }
        .frame(width: frame.width, height: frame.height, alignment: .topLeading)
    }

    private var k: CGFloat { frame.k }

    private func lit(_ edge: SDUIGraph.Edge) -> Bool {
        hovered.map { $0.node == edge.from && $0.out == edge.out } == true
    }

    private func drawEdges(_ context: inout GraphicsContext) {
        let rank: [SDUIGraph.EdgeState: Int] = [.never: 0, .plain: 1, .taken: 1, .last: 2]
        let routes = frame.layout.routes.filter { graph.edges.indices.contains($0.edge) }.sorted {
            let a = graph.edges[$0.edge], b = graph.edges[$1.edge]
            return (lit(a) ? 3 : rank[a.state] ?? 0, $0.edge) < (lit(b) ? 3 : rank[b.state] ?? 0, $1.edge)
        }
        for route in routes where route.points.count >= 4 {
            let edge = graph.edges[route.edge]
            let (color, style) = stroke(edge.state, lit: lit(edge))
            var points = route.points
            let end = points[points.count - 1]
            let tip = CGPoint(x: end.x - 4.5 * k, y: end.y)
            points[points.count - 1] = tip
            var path = Path()
            path.move(to: points[0])
            for i in stride(from: 1, to: points.count - 2, by: 3) {
                path.addCurve(to: points[i + 2], control1: points[i], control2: points[i + 1])
            }
            context.stroke(path, with: .color(color), style: style)
            arrow(&context, tip: tip, from: points[points.count - 2], color: color, width: style.lineWidth)
        }
    }

    private func drawMarkers(_ context: inout GraphicsContext) {
        for node in graph.nodes where node.start {
            guard let rect = frame.layout.cards[node.id] else { continue }
            let y = frame.layout.input(node: node.id).y
            let ring = CGPoint(x: rect.minX - 40 * k, y: y)
            let (color, style) = stroke(.taken, lit: false)
            var line = Path()
            line.move(to: CGPoint(x: ring.x + 5 * k, y: y))
            line.addLine(to: CGPoint(x: rect.minX - 4.5 * k, y: y))
            context.stroke(line, with: .color(color), style: style)
            arrow(&context, tip: CGPoint(x: rect.minX - 4.5 * k, y: y), from: ring, color: color, width: style.lineWidth)
            let circle = Path(ellipseIn: CGRect(x: ring.x - 5 * k, y: y - 5 * k, width: 10 * k, height: 10 * k))
            context.fill(circle, with: .color(theme.background))
            context.stroke(circle, with: .color(theme.muted), lineWidth: 1.25 * k)
            context.draw(Text("start").font(theme.uiSmall).foregroundStyle(theme.muted), at: CGPoint(x: ring.x, y: y + 9 * k), anchor: .top)
        }
        for node in graph.nodes {
            guard let rect = frame.layout.cards[node.id] else { continue }
            for (index, out) in node.outs.enumerated() where out.end {
                let port = CGPoint(x: rect.maxX, y: portY(rect, index))
                let center = CGPoint(x: port.x + 46 * k, y: port.y)
                let (color, style) = stroke(out.state, lit: false)
                var line = Path()
                line.move(to: port)
                line.addLine(to: CGPoint(x: center.x - 8 * k, y: port.y))
                context.stroke(line, with: .color(color), style: style)
                let outer = Path(ellipseIn: CGRect(x: center.x - 8 * k, y: center.y - 8 * k, width: 16 * k, height: 16 * k))
                let inner = Path(ellipseIn: CGRect(x: center.x - 4.5 * k, y: center.y - 4.5 * k, width: 9 * k, height: 9 * k))
                context.fill(outer, with: .color(theme.background))
                context.stroke(outer, with: .color(theme.muted), lineWidth: 1.25 * k)
                if graph.reached && out.state == .last {
                    context.fill(inner, with: .color(theme.success))
                } else {
                    context.stroke(inner, with: .color(theme.muted), lineWidth: 1.25 * k)
                }
                context.draw(Text("end").font(theme.uiSmall).foregroundStyle(theme.muted), at: CGPoint(x: center.x, y: center.y - 11 * k), anchor: .bottom)
            }
        }
    }

    private func drawPorts(_ context: inout GraphicsContext, cards: [Slot]) {
        for card in cards {
            let input = CGPoint(x: card.rect.minX, y: card.rect.minY + frame.metrics.headHeight / 2)
            context.fill(dot(input, 3.5 * k), with: .color(theme.muted))
            for (index, out) in card.node.outs.enumerated() {
                let point = CGPoint(x: card.rect.maxX, y: portY(card.rect, index))
                if hovered == GraphHop(node: card.node.id, out: out.id) {
                    context.fill(dot(point, 5.5 * k), with: .color(theme.foreground))
                    continue
                }
                switch out.state {
                case .last: context.fill(dot(point, 4 * k), with: .color(theme.accent))
                case .taken, .plain: context.fill(dot(point, 4 * k), with: .color(theme.muted))
                case .never:
                    context.fill(dot(point, 4 * k), with: .color(theme.background))
                    context.stroke(dot(point, 4 * k), with: .color(theme.borderStrong), lineWidth: 1.25 * k)
                }
            }
        }
    }

    private func portY(_ rect: CGRect, _ index: Int) -> CGFloat {
        rect.minY + frame.metrics.headHeight + frame.metrics.rowHeight / 3 + frame.metrics.rowHeight * (CGFloat(index) + 0.5)
    }

    private func dot(_ center: CGPoint, _ radius: CGFloat) -> Path {
        Path(ellipseIn: CGRect(x: center.x - radius, y: center.y - radius, width: 2 * radius, height: 2 * radius))
    }

    private func stroke(_ state: SDUIGraph.EdgeState, lit: Bool) -> (Color, StrokeStyle) {
        if lit { return (theme.foreground, StrokeStyle(lineWidth: 2.75 * k, lineCap: .round, lineJoin: .round)) }
        switch state {
        case .last: return (theme.accent, StrokeStyle(lineWidth: 2 * k, lineCap: .round, lineJoin: .round))
        case .taken, .plain: return (theme.muted, StrokeStyle(lineWidth: 1.25 * k, lineCap: .round, lineJoin: .round))
        case .never: return (theme.faint, StrokeStyle(lineWidth: k, lineCap: .round, lineJoin: .round, dash: [3 * k, 4 * k]))
        }
    }

    private func arrow(_ context: inout GraphicsContext, tip: CGPoint, from: CGPoint, color: Color, width: CGFloat) {
        GraphArrow.draw(&context, tip: tip, from: from, size: 5 * k, color: color, width: width)
    }
}

private enum GraphArrow {
    static func draw(_ context: inout GraphicsContext, tip: CGPoint, from: CGPoint, size: CGFloat, color: Color, width: CGFloat) {
        let dx = tip.x - from.x, dy = tip.y - from.y
        let length = max(hypot(dx, dy), 0.001)
        let ux = dx / length, uy = dy / length
        var path = Path()
        path.move(to: CGPoint(x: tip.x - size * ux - size * 0.8 * uy, y: tip.y - size * uy + size * 0.8 * ux))
        path.addLine(to: tip)
        path.addLine(to: CGPoint(x: tip.x - size * ux + size * 0.8 * uy, y: tip.y - size * uy - size * 0.8 * ux))
        context.stroke(path, with: .color(color), style: StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round))
    }
}

private struct GraphCard: View {
    let node: SDUIGraph.Node
    let metrics: GraphMetrics
    let focused: Bool
    let lit: String?
    let open: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        let k = theme.uiSize / 12
        let shape = RoundedRectangle(cornerRadius: theme.radius.l)
        Button(action: open) {
            VStack(alignment: .leading, spacing: 0) {
                VStack(alignment: .leading, spacing: 3 * k) {
                    HStack(spacing: 7 * k) {
                        if !node.state.isEmpty {
                            GraphGlyph(node: node, size: 14 * k)
                        }
                        Text(node.label)
                            .font(theme.strong)
                            .foregroundStyle(node.pending ? theme.muted : theme.foreground)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                    if !node.state.isEmpty {
                        Text(node.state)
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.tone(node.tone) ?? (node.pending ? theme.muted : theme.text2))
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                }
                .padding(.horizontal, 11 * k)
                .frame(maxWidth: .infinity, minHeight: metrics.headHeight, maxHeight: metrics.headHeight, alignment: .leading)
                .overlay(alignment: .bottom) { theme.border.frame(height: 1) }
                VStack(spacing: 0) {
                    ForEach(Array(node.outs.enumerated()), id: \.offset) { _, out in
                        HStack(spacing: theme.spacing.m) {
                            Text(out.label)
                                .font(lit == out.id ? theme.uiSmall.weight(.semibold) : theme.uiSmall)
                                .foregroundStyle(lit == out.id ? theme.foreground : out.state == .never ? theme.faint : theme.text2)
                                .lineLimit(1)
                                .truncationMode(.tail)
                                .modifier(CutHelp(text: out.label, cut: cut(out, k: k)))
                            Spacer(minLength: 0)
                            if out.count > 0 {
                                Text("×\(out.count)")
                                    .font(theme.uiSmall.monospacedDigit())
                                    .foregroundStyle(theme.muted)
                                    .fixedSize()
                            }
                        }
                        .padding(.leading, 12 * k)
                        .padding(.trailing, 14 * k)
                        .frame(height: metrics.rowHeight)
                    }
                }
                .padding(.vertical, metrics.rowHeight / 3)
                Spacer(minLength: 0)
            }
            .background(shape.fill(fill))
            .background(shape.fill(hovering ? theme.hover : .clear))
            .overlay(border(shape, k: k))
            .overlay {
                if focused {
                    RoundedRectangle(cornerRadius: theme.radius.l + 2)
                        .stroke(theme.accent, lineWidth: theme.focusRing)
                        .padding(-2 - theme.focusRing / 2)
                }
            }
            .contentShape(shape)
        }
        .buttonStyle(.plain)
        .focusable(false)
        .onHover { hovering = $0 }
        .help(node.tooltip ?? node.label)
        .accessibilityLabel(node.state.isEmpty ? node.label : "\(node.label), \(node.state)")
    }

    private func cut(_ out: SDUIGraph.Out, k: CGFloat) -> Bool {
        let width = { (text: String) in (text as NSString).size(withAttributes: [.font: theme.nsFont(size: theme.uiSize - 1, bold: lit == out.id)]).width }
        let count = out.count > 0 ? width("×\(out.count)") + theme.spacing.m : 0
        return width(out.label) > metrics.cardWidth - 26 * k - count
    }

    private var fill: Color {
        node.attention ? theme.toneBg(theme.warningFill) : node.pending ? .clear : theme.surface1
    }

    @ViewBuilder
    private func border(_ shape: RoundedRectangle, k: CGFloat) -> some View {
        if node.attention {
            shape.strokeBorder(theme.warningFill, lineWidth: 1.5)
        } else if node.pending {
            shape.strokeBorder(theme.borderStrong, style: StrokeStyle(lineWidth: 1, dash: [4 * k, 3 * k]))
        } else {
            shape.strokeBorder(theme.borderStrong, lineWidth: 1)
        }
    }
}

private struct CutHelp: ViewModifier {
    let text: String
    let cut: Bool

    func body(content: Content) -> some View {
        if cut {
            content.help(text)
        } else {
            content
        }
    }
}

private struct GraphGlyph: View {
    let node: SDUIGraph.Node
    let size: CGFloat

    @State private var pulse = false
    @Environment(\.theme) private var theme

    var body: some View {
        if node.attention {
            symbol(SymbolName.resolve((node.icon ?? "exclamationmark.circle") + ".fill", fallback: "exclamationmark.circle.fill"))
                .symbolRenderingMode(.palette)
                .foregroundStyle(theme.onWarning, theme.warningFill)
                .background {
                    Circle()
                        .stroke(theme.warningFill, lineWidth: 1.5)
                        .frame(width: size * 9 / 7, height: size * 9 / 7)
                        .scaleEffect(pulse ? 16 / 9 : 1)
                        .opacity(pulse ? 0 : 0.9)
                }
                .onAppear {
                    guard !NSWorkspace.shared.accessibilityDisplayShouldReduceMotion else { return }
                    withAnimation(.easeOut(duration: 1.6).repeatForever(autoreverses: false)) { pulse = true }
                }
        } else if node.pending {
            symbol(SymbolName.resolve(node.icon, fallback: "circle.dashed"))
                .foregroundStyle(theme.faint)
        } else {
            symbol(SymbolName.resolve(node.icon, fallback: "circle"))
                .foregroundStyle(theme.tone(node.tone) ?? theme.muted)
        }
    }

    private func symbol(_ name: String) -> some View {
        Image(systemName: name)
            .resizable()
            .scaledToFit()
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

private struct GraphHistoryRow: View {
    let moves: Int
    let open: Bool
    let toggle: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: toggle) {
            HStack(spacing: theme.spacing.m) {
                DisclosureChevron(expanded: open)
                    .foregroundStyle(theme.muted)
                Text("History")
                    .foregroundStyle(hovering ? theme.foreground : theme.text2)
                Text("· \(moves) \(moves == 1 ? "move" : "moves")")
                    .foregroundStyle(theme.muted)
                Spacer(minLength: theme.gutter)
                Text("h")
                    .foregroundStyle(theme.faint)
            }
            .font(theme.ui)
            .frame(height: theme.rowHeight2)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .focusable(false)
        .onHover { hovering = $0 }
        .overlay(alignment: .top) { theme.border.frame(height: 1) }
        .accessibilityLabel("History, \(moves) moves")
    }
}

private struct GraphHistory: View {
    let graph: SDUIGraph
    let frame: GraphFrame
    @Binding var hovered: GraphHop?
    let openMove: (Int) -> Void

    @State private var viewport: CGFloat = 0
    @State private var position = ScrollPosition(edge: .trailing)
    @Environment(\.theme) private var theme

    private struct Hop {
        let from: CGPoint
        let to: CGPoint
        let label: String
        let last: Bool
        let hop: GraphHop?
    }

    private var k: CGFloat { frame.k }
    private var step: CGFloat { 82 * k }

    private var lanes: [SDUIGraph.Node] {
        var seen = Set<String>()
        return graph.nodes.filter { seen.insert($0.id).inserted }
    }

    private func laneY(_ index: Int) -> CGFloat {
        30 * k + 50 * k * CGFloat(index)
    }

    private func moveX(_ index: Int) -> CGFloat {
        30 * k + step * CGFloat(index)
    }

    var body: some View {
        let lanes = lanes
        let lane = Dictionary(lanes.enumerated().map { ($0.element.id, $0.offset) }, uniquingKeysWith: { a, _ in a })
        let hops = hops(lane: lane)
        let end = endMove(lane: lane)
        let width = max(viewport, moveX(max(graph.moves.count - 1, 0)) + (end == nil ? 0 : 84 * k) + 30 * k)
        let height = 10 * k + 50 * k * CGFloat(lanes.count)
        HStack(alignment: .top, spacing: 0) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(lanes, id: \.id) { node in
                    HStack(spacing: 7 * k) {
                        GraphGlyph(node: node, size: 14 * k)
                        Text(node.label)
                            .font(theme.strong)
                            .foregroundStyle(node.pending ? theme.muted : theme.foreground)
                            .lineLimit(1)
                            .truncationMode(.tail)
                            .help(node.label)
                    }
                    .padding(.leading, 11 * k)
                    .frame(height: 50 * k)
                }
            }
            .padding(.vertical, 5 * k)
            .frame(width: frame.metrics.cardWidth * 0.75, alignment: .leading)
            ScrollView(.horizontal) {
                ZStack(alignment: .topLeading) {
                    Canvas { context, _ in
                        for index in lanes.indices {
                            var line = Path()
                            line.move(to: CGPoint(x: 0, y: laneY(index)))
                            line.addLine(to: CGPoint(x: width, y: laneY(index)))
                            context.stroke(line, with: .color(theme.border), lineWidth: 1)
                        }
                        for hop in hops {
                            draw(&context, hop)
                        }
                        for (index, move) in graph.moves.enumerated() where move.cause < 0 {
                            guard let label = move.label, let row = lane[move.node] else { continue }
                            context.draw(Text(label).font(theme.uiSmall).foregroundStyle(theme.muted), at: CGPoint(x: moveX(index), y: laneY(row) - 11 * k), anchor: .bottom)
                        }
                        if let end {
                            let y = laneY(end.lane), x = moveX(end.move) + 74 * k
                            draw(&context, Hop(from: CGPoint(x: moveX(end.move), y: y), to: CGPoint(x: x - 1 * k, y: y), label: end.label, last: false, hop: nil))
                            let outer = Path(ellipseIn: CGRect(x: x - 8 * k, y: y - 8 * k, width: 16 * k, height: 16 * k))
                            context.fill(outer, with: .color(theme.background))
                            context.stroke(outer, with: .color(theme.muted), lineWidth: 1.25 * k)
                            context.fill(Path(ellipseIn: CGRect(x: x - 4.5 * k, y: y - 4.5 * k, width: 9 * k, height: 9 * k)), with: .color(theme.success))
                        }
                    }
                    ForEach(Array(graph.moves.enumerated()), id: \.offset) { index, move in
                        if let row = lane[move.node] {
                            GraphMoveDot(node: lanes[row], move: move, size: 14 * k) { openMove(index) }
                                .position(x: moveX(index), y: laneY(row))
                        }
                    }
                }
                .frame(width: width, height: height, alignment: .topLeading)
                .onContinuousHover { phase in
                    let next: GraphHop? = switch phase {
                    case .active(let point): hops.first { near(point, $0) }?.hop
                    case .ended: nil
                    }
                    if next != hovered { hovered = next }
                }
            }
            .scrollPosition($position)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { viewport = $0 }
            .onChange(of: graph.moves.count, initial: true) { position.scrollTo(edge: .trailing) }
            .onChange(of: viewport) { position.scrollTo(edge: .trailing) }
            .scrollIndicators(.automatic)
            .clipped()
        }
        .padding(.bottom, theme.spacing.s)
    }

    private func hops(lane: [String: Int]) -> [Hop] {
        graph.moves.enumerated().compactMap { index, move in
            guard move.cause >= 0, move.cause < index, let to = lane[move.node] else { return nil }
            let cause = graph.moves[move.cause]
            guard let from = lane[cause.node] else { return nil }
            let hop = move.out.map { GraphHop(node: cause.node, out: $0) }
            return Hop(from: CGPoint(x: moveX(move.cause), y: laneY(from)), to: CGPoint(x: moveX(index), y: laneY(to)), label: move.label ?? move.out ?? "", last: move.last, hop: hop)
        }
    }

    private func endMove(lane: [String: Int]) -> (move: Int, lane: Int, label: String)? {
        for (index, move) in graph.moves.enumerated() {
            guard let end = move.end, let row = lane[move.node] else { continue }
            let label = graph.nodes.first { $0.id == move.node }?.outs.first { $0.id == end }?.label ?? end
            return (index, row, label)
        }
        return nil
    }

    private func controls(_ hop: Hop) -> (CGPoint, CGPoint, CGPoint, CGPoint) {
        let start = CGPoint(x: hop.from.x + 7 * k, y: hop.from.y), end = CGPoint(x: hop.to.x - 9 * k, y: hop.to.y)
        return (start, CGPoint(x: hop.from.x + step / 2, y: hop.from.y), CGPoint(x: hop.to.x - step / 2, y: hop.to.y), end)
    }

    private func draw(_ context: inout GraphicsContext, _ hop: Hop) {
        let (start, c1, c2, end) = controls(hop)
        let color = hop.last ? theme.accent : theme.muted
        let width = (hop.last ? 2 : 1.25) * k
        var path = Path()
        path.move(to: start)
        path.addCurve(to: end, control1: c1, control2: c2)
        context.stroke(path, with: .color(color), style: StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round))
        GraphArrow.draw(&context, tip: end, from: c2, size: 4 * k, color: color, width: width)
        guard !hop.label.isEmpty else { return }
        let mid = CGPoint(x: (hop.from.x + hop.to.x) / 2, y: (hop.from.y + hop.to.y) / 2)
        let text = Text(hop.label).font(theme.uiSmall).foregroundStyle(hop.last ? theme.accentInk : theme.muted)
        if hop.to.y > hop.from.y {
            context.draw(text, at: CGPoint(x: mid.x + 5 * k, y: mid.y - 2 * k), anchor: .bottomLeading)
        } else if hop.to.y < hop.from.y {
            context.draw(text, at: CGPoint(x: mid.x + 5 * k, y: mid.y + 2 * k), anchor: .topLeading)
        } else {
            context.draw(text, at: CGPoint(x: mid.x, y: mid.y - 4 * k), anchor: .bottom)
        }
    }

    private func near(_ point: CGPoint, _ hop: Hop) -> Bool {
        let (p0, p1, p2, p3) = controls(hop)
        return (0...20).contains { i in
            let t = CGFloat(i) / 20, u = 1 - t
            let x = u * u * u * p0.x + 3 * u * u * t * p1.x + 3 * u * t * t * p2.x + t * t * t * p3.x
            let y = u * u * u * p0.y + 3 * u * u * t * p1.y + 3 * u * t * t * p2.y + t * t * t * p3.y
            return hypot(point.x - x, point.y - y) < 7 * k
        }
    }
}

private struct GraphMoveDot: View {
    let node: SDUIGraph.Node
    let move: SDUIGraph.Move
    let size: CGFloat
    let open: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: open) {
            Group {
                if move.current {
                    GraphGlyph(node: node, size: size)
                } else {
                    Circle()
                        .fill(theme.success)
                        .frame(width: size * 5 / 14, height: size * 5 / 14)
                        .frame(width: size * 6 / 7, height: size * 6 / 7)
                        .background(Circle().fill(theme.background))
                        .overlay(Circle().stroke(theme.success, lineWidth: 1.5))
                }
            }
            .frame(width: size + 4, height: size + 4)
            .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .focusable(false)
        .help(move.tooltip)
        .accessibilityLabel(move.tooltip)
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
