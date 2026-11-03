import AppKit
import SwiftUI

import ClubmateKit

struct FlowHop: Equatable {
    let node: String
    let out: String
}

struct FlowFrame {
    let theme: Theme
    let metrics: GraphMetrics
    let layout: GraphLayout

    init(flow: SDUIFlow, theme: Theme) {
        self.theme = theme
        let metrics = Self.metrics(theme)
        self.metrics = metrics
        let sizes = flow.markers.map { ($0.id, FlowMarkerView.size($0, theme: theme, metrics: metrics)) }
        layout = GraphLayout(flow: flow, metrics: metrics, markers: Dictionary(sizes, uniquingKeysWith: { a, _ in a }))
    }

    static func metrics(_ theme: Theme) -> GraphMetrics {
        let k = theme.uiSize / 12
        let char = ("M" as NSString).size(withAttributes: [.font: theme.nsFont(size: theme.uiSize, bold: true)]).width
        return GraphMetrics(cardWidth: max(210 * k, (20 * char + 44 * k).rounded()), headHeight: 52 * k, rowHeight: 26 * k, layerGap: 120 * k, nodeGap: 24 * k, trackGap: 12 * k, markerGap: 48 * k)
    }

    var k: CGFloat { theme.uiSize / 12 }
    var pad: CGFloat { 4 * k }
    var width: CGFloat { layout.size.width + 2 * pad }
    var height: CGFloat { layout.size.height + 2 * pad }
    var order: [String] { layout.layers.flatMap { $0 }.filter { layout.cards[$0] != nil } }

    func edgeOrder(_ flow: SDUIFlow) -> [Int] {
        layout.routes.filter { flow.edges.indices.contains($0.edge) && flow.edges[$0.edge].pointable }.sorted {
            let a = $0.points[0], b = $1.points[0]
            return (a.x, a.y, $0.edge) < (b.x, b.y, $1.edge)
        }.map(\.edge)
    }
}

struct FlowDiagram: View {
    let flow: SDUIFlow
    let frame: FlowFrame
    let cursor: String?
    let edgeCursor: Int?
    let hovered: FlowHop?
    let open: (String) -> Void
    let openEdge: (Int) -> Void

    @State private var pointer: (edge: Int, at: CGPoint)?
    @Environment(\.theme) private var theme

    private struct Slot {
        let card: SDUIFlow.Card
        let rect: CGRect
    }

    private var cards: [Slot] {
        var seen = Set<String>()
        return flow.cards.compactMap { card in
            guard seen.insert(card.id).inserted, let rect = frame.layout.cards[card.id] else { return nil }
            return Slot(card: card, rect: rect.offsetBy(dx: frame.pad, dy: frame.pad))
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
            .modifier(EdgePointer(enabled: flow.edges.contains(where: \.pointable), frame: frame, cards: cards.map(\.rect), pointer: $pointer) { edge in
                guard flow.edges.indices.contains(edge), flow.edges[edge].ref != nil else { return }
                openEdge(edge)
            })
            ForEach(flow.markers.filter { frame.layout.markers[$0.id] != nil }, id: \.id) { marker in
                if let rect = frame.layout.markers[marker.id]?.offsetBy(dx: frame.pad, dy: frame.pad) {
                    FlowMarkerView(marker: marker, metrics: frame.metrics, reached: flow.reached)
                        .frame(width: rect.width, height: rect.height)
                        .position(x: rect.midX, y: rect.midY)
                        .allowsHitTesting(false)
                }
            }
            ForEach(cards, id: \.card.id) { slot in
                FlowCardView(card: slot.card, metrics: frame.metrics, focused: edgeCursor == nil && cursor == slot.card.id, lit: hovered?.node == slot.card.id ? hovered?.out : nil) {
                    open(slot.card.id)
                }
                .frame(width: slot.rect.width, height: slot.rect.height)
                .id(slot.card.id)
                .position(x: slot.rect.midX, y: slot.rect.midY)
            }
            Canvas { context, _ in
                drawPorts(&context, cards: cards)
            }
            .allowsHitTesting(false)
            if let tip, let text = flow.edges[tip.edge].tooltip {
                FlowTooltip(text: text, opens: flow.edges[tip.edge].ref != nil, k: frame.k)
                    .offset(x: max(0, min(tip.at.x + 10 * frame.k, frame.width - 260 * frame.k)), y: tip.at.y > frame.height - 60 * frame.k ? tip.at.y - 48 * frame.k : tip.at.y + 14 * frame.k)
                    .allowsHitTesting(false)
            }
        }
        .frame(width: frame.width, height: frame.height, alignment: .topLeading)
    }

    private var tip: (edge: Int, at: CGPoint)? {
        if let edgeCursor, let route = frame.layout.routes.first(where: { $0.edge == edgeCursor }) {
            let middle = route.middle
            return (edgeCursor, CGPoint(x: middle.x + frame.pad, y: middle.y + frame.pad))
        }
        guard let pointer, flow.edges.indices.contains(pointer.edge) else { return nil }
        return (pointer.edge, pointer.at)
    }

    private var k: CGFloat { frame.k }

    private func lit(_ index: Int, _ edge: SDUIFlow.Edge) -> Bool {
        index == edgeCursor || index == pointer?.edge || hovered.map { $0.node == edge.from && $0.out == edge.out } == true
    }

    private func drawEdges(_ context: inout GraphicsContext) {
        let rank: [SDUIFlow.EdgeState: Int] = [.never: 0, .ghost: 0, .dim: 1, .plain: 2, .taken: 2, .last: 3, .selected: 3]
        let routes = frame.layout.routes.filter { flow.edges.indices.contains($0.edge) }.sorted {
            let a = flow.edges[$0.edge], b = flow.edges[$1.edge]
            return (lit($0.edge, a) ? 4 : rank[a.state] ?? 0, $0.edge) < (lit($1.edge, b) ? 4 : rank[b.state] ?? 0, $1.edge)
        }
        for route in routes where route.points.count >= 4 {
            let edge = flow.edges[route.edge]
            let (color, style) = stroke(edge.state, lit: lit(route.edge, edge))
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
        for card in flow.cards where card.start {
            guard let rect = frame.layout.cards[card.id] else { continue }
            let y = frame.layout.input(node: card.id).y
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
        for card in flow.cards {
            guard let rect = frame.layout.cards[card.id] else { continue }
            for (index, out) in card.outs.enumerated() where out.end {
                let port = CGPoint(x: rect.maxX, y: portY(rect, index))
                let center = CGPoint(x: port.x + 46 * k, y: port.y)
                let (color, style) = stroke(out.state, lit: false)
                var line = Path()
                line.move(to: port)
                line.addLine(to: CGPoint(x: center.x - 8 * k, y: port.y))
                context.stroke(line, with: .color(color), style: style)
                FlowRing.end(&context, center: center, k: k, filled: flow.reached && out.state == .last, theme: theme)
                context.draw(Text("end").font(theme.uiSmall).foregroundStyle(theme.muted), at: CGPoint(x: center.x, y: center.y - 11 * k), anchor: .bottom)
            }
        }
    }

    private func drawPorts(_ context: inout GraphicsContext, cards: [Slot]) {
        for card in cards {
            let input = CGPoint(x: card.rect.minX, y: card.rect.minY + frame.metrics.headHeight / 2)
            context.fill(dot(input, 3.5 * k), with: .color(card.card.ghost ? theme.faint : theme.muted))
            for (index, out) in card.card.outs.enumerated() {
                let point = CGPoint(x: card.rect.maxX, y: portY(card.rect, index))
                if hovered == FlowHop(node: card.card.id, out: out.id) {
                    context.fill(dot(point, 5.5 * k), with: .color(theme.foreground))
                    continue
                }
                switch out.state {
                case .last, .selected: context.fill(dot(point, 4 * k), with: .color(theme.accent))
                case .taken, .plain: context.fill(dot(point, 4 * k), with: .color(theme.muted))
                case .dim: context.fill(dot(point, 4 * k), with: .color(theme.faint))
                case .never, .ghost:
                    context.fill(dot(point, 4 * k), with: .color(theme.background))
                    context.stroke(dot(point, 4 * k), with: .color(theme.borderStrong), lineWidth: 1.25 * k)
                }
            }
        }
        for marker in flow.markers where marker.kind == .inbox || marker.kind == .session {
            guard let rect = frame.layout.markers[marker.id], flow.edges.contains(where: { $0.to == marker.id }) else { continue }
            context.fill(dot(CGPoint(x: rect.minX + frame.pad, y: rect.midY + frame.pad), 3.5 * k), with: .color(theme.muted))
        }
    }

    private func portY(_ rect: CGRect, _ index: Int) -> CGFloat {
        rect.minY + frame.metrics.headHeight + frame.metrics.rowHeight / 3 + frame.metrics.rowHeight * (CGFloat(index) + 0.5)
    }

    private func dot(_ center: CGPoint, _ radius: CGFloat) -> Path {
        Path(ellipseIn: CGRect(x: center.x - radius, y: center.y - radius, width: 2 * radius, height: 2 * radius))
    }

    private func stroke(_ state: SDUIFlow.EdgeState, lit: Bool) -> (Color, StrokeStyle) {
        if lit { return (theme.foreground, StrokeStyle(lineWidth: 2.75 * k, lineCap: .round, lineJoin: .round)) }
        switch state {
        case .last, .selected: return (theme.accent, StrokeStyle(lineWidth: 2 * k, lineCap: .round, lineJoin: .round))
        case .taken, .plain: return (theme.muted, StrokeStyle(lineWidth: 1.25 * k, lineCap: .round, lineJoin: .round))
        case .dim: return (theme.faint, StrokeStyle(lineWidth: k, lineCap: .round, lineJoin: .round))
        case .never, .ghost: return (theme.faint, StrokeStyle(lineWidth: k, lineCap: .round, lineJoin: .round, dash: [3 * k, 4 * k]))
        }
    }

    private func arrow(_ context: inout GraphicsContext, tip: CGPoint, from: CGPoint, color: Color, width: CGFloat) {
        FlowArrow.draw(&context, tip: tip, from: from, size: 5 * k, color: color, width: width)
    }
}

private struct EdgePointer: ViewModifier {
    let enabled: Bool
    let frame: FlowFrame
    let cards: [CGRect]
    @Binding var pointer: (edge: Int, at: CGPoint)?
    let click: (Int) -> Void

    func body(content: Content) -> some View {
        if enabled {
            content
                .contentShape(Rectangle())
                .onContinuousHover { phase in
                    switch phase {
                    case .active(let point):
                        let edge = hit(point)
                        if edge != pointer?.edge || edge != nil { pointer = edge.map { (edge: $0, at: point) } }
                    case .ended:
                        if pointer != nil { pointer = nil }
                    }
                }
                .onTapGesture(coordinateSpace: .local) { point in
                    if let edge = hit(point) { click(edge) }
                }
        } else {
            content
        }
    }

    private func hit(_ point: CGPoint) -> Int? {
        guard !cards.contains(where: { $0.contains(point) }) else { return nil }
        return frame.layout.edge(at: CGPoint(x: point.x - frame.pad, y: point.y - frame.pad), tolerance: 7 * frame.k)
    }
}

private struct FlowTooltip: View {
    let text: String
    let opens: Bool
    let k: CGFloat

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 3 * k) {
            Text(text)
                .font(theme.uiSmall)
                .foregroundStyle(theme.foreground)
                .lineLimit(3)
            if opens {
                Text("↩ or click opens it")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
        }
        .padding(.horizontal, 9 * k)
        .padding(.vertical, 6 * k)
        .frame(maxWidth: 250 * k, alignment: .leading)
        .fixedSize(horizontal: false, vertical: true)
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.surface3))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).strokeBorder(theme.borderStrong, lineWidth: 1))
    }
}

enum FlowArrow {
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

enum FlowRing {
    static func end(_ context: inout GraphicsContext, center: CGPoint, k: CGFloat, filled: Bool, theme: Theme) {
        let outer = Path(ellipseIn: CGRect(x: center.x - 8 * k, y: center.y - 8 * k, width: 16 * k, height: 16 * k))
        let inner = Path(ellipseIn: CGRect(x: center.x - 4.5 * k, y: center.y - 4.5 * k, width: 9 * k, height: 9 * k))
        context.fill(outer, with: .color(theme.background))
        context.stroke(outer, with: .color(theme.muted), lineWidth: 1.25 * k)
        if filled {
            context.fill(inner, with: .color(theme.success))
        } else {
            context.stroke(inner, with: .color(theme.muted), lineWidth: 1.25 * k)
        }
    }
}

struct FlowMarkerView: View {
    let marker: SDUIFlow.Marker
    let metrics: GraphMetrics
    let reached: Bool

    @Environment(\.theme) private var theme

    static func size(_ marker: SDUIFlow.Marker, theme: Theme, metrics: GraphMetrics) -> CGSize {
        let k = theme.uiSize / 12
        switch marker.kind {
        case .start: return CGSize(width: 10 * k, height: metrics.headHeight)
        case .end: return CGSize(width: 16 * k, height: metrics.headHeight)
        case .schedule: return CGSize(width: 28 * k, height: metrics.headHeight)
        case .inbox, .session:
            let text = (chipLabel(marker) as NSString).size(withAttributes: [.font: theme.nsFont(size: theme.uiSize, bold: true)]).width
            return CGSize(width: min(metrics.cardWidth, (text + 42 * k).rounded(.up)), height: metrics.headHeight)
        }
    }

    static func chipLabel(_ marker: SDUIFlow.Marker) -> String {
        marker.label ?? (marker.kind == .inbox ? "Inbox" : "session")
    }

    var body: some View {
        let k = theme.uiSize / 12
        switch marker.kind {
        case .start, .end, .schedule:
            Canvas { context, size in
                let center = CGPoint(x: size.width / 2, y: size.height / 2)
                switch marker.kind {
                case .start:
                    let circle = Path(ellipseIn: CGRect(x: center.x - 5 * k, y: center.y - 5 * k, width: 10 * k, height: 10 * k))
                    context.fill(circle, with: .color(theme.background))
                    context.stroke(circle, with: .color(theme.muted), lineWidth: 1.25 * k)
                case .end:
                    FlowRing.end(&context, center: center, k: k, filled: reached || marker.reached, theme: theme)
                default:
                    let circle = Path(ellipseIn: CGRect(x: center.x - 14 * k, y: center.y - 14 * k, width: 28 * k, height: 28 * k))
                    context.fill(circle, with: .color(theme.surface2))
                    context.stroke(circle, with: .color(theme.borderStrong), lineWidth: 1.25 * k)
                    var clock = context.resolve(Image(systemName: "clock"))
                    clock.shading = .color(theme.muted)
                    context.draw(clock, in: CGRect(x: center.x - 7 * k, y: center.y - 7 * k, width: 14 * k, height: 14 * k))
                }
            }
            .overlay(alignment: marker.kind == .end ? .bottom : .top) {
                Text(label)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .fixedSize()
                    .offset(y: labelOffset(k))
            }
            .help(marker.label ?? label)
            .accessibilityElement()
            .accessibilityLabel(label)
        case .inbox, .session:
            HStack(spacing: 6 * k) {
                Image(systemName: marker.kind == .inbox ? "tray" : "sparkle")
                    .resizable()
                    .scaledToFit()
                    .frame(width: 12 * k, height: 12 * k)
                    .foregroundStyle(theme.muted)
                Text(Self.chipLabel(marker))
                    .font(theme.strong)
                    .foregroundStyle(theme.text2)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
            .padding(.horizontal, 10 * k)
            .frame(maxWidth: .infinity, minHeight: 28 * k, maxHeight: 28 * k, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.surface1))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.m).strokeBorder(theme.borderStrong, lineWidth: 1))
            .help(Self.chipLabel(marker))
            .accessibilityElement(children: .combine)
        }
    }

    private var label: String {
        marker.label ?? marker.kind.rawValue
    }

    private func labelOffset(_ k: CGFloat) -> CGFloat {
        switch marker.kind {
        case .end: -(metrics.headHeight / 2 + 11 * k)
        case .schedule: metrics.headHeight / 2 + 17 * k
        default: metrics.headHeight / 2 + 9 * k
        }
    }
}

struct FlowCardView: View {
    let card: SDUIFlow.Card
    let metrics: GraphMetrics
    let focused: Bool
    let lit: String?
    let open: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        let k = theme.uiSize / 12
        let shape = RoundedRectangle(cornerRadius: theme.radius.l)
        let quiet = card.pending || card.ghost
        Button(action: open) {
            VStack(alignment: .leading, spacing: 0) {
                VStack(alignment: .leading, spacing: 3 * k) {
                    HStack(spacing: 7 * k) {
                        if !card.state.isEmpty {
                            FlowGlyph(card: card, size: 14 * k)
                        }
                        Text(card.label)
                            .font(theme.strong)
                            .foregroundStyle(quiet ? theme.muted : theme.foreground)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                    if !card.state.isEmpty {
                        Text(card.state)
                            .font(theme.uiSmall)
                            .foregroundStyle(card.ghost ? theme.faint : theme.tone(card.tone) ?? (card.pending ? theme.muted : theme.text2))
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                }
                .padding(.horizontal, 11 * k)
                .frame(maxWidth: .infinity, minHeight: metrics.headHeight, maxHeight: metrics.headHeight, alignment: .leading)
                .overlay(alignment: .bottom) { (card.ghost ? theme.border.opacity(0.6) : theme.border).frame(height: 1) }
                VStack(spacing: 0) {
                    ForEach(Array(card.outs.enumerated()), id: \.offset) { _, out in
                        HStack(spacing: theme.spacing.m) {
                            Text(out.label)
                                .font(lit == out.id ? theme.uiSmall.weight(.semibold) : theme.uiSmall)
                                .foregroundStyle(lit == out.id ? theme.foreground : out.state == .never || out.state == .ghost || card.ghost ? theme.faint : theme.text2)
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
        .help(card.tooltip ?? card.label)
        .accessibilityLabel(card.state.isEmpty ? card.label : "\(card.label), \(card.state)")
    }

    private func cut(_ out: SDUIFlow.Out, k: CGFloat) -> Bool {
        let width = { (text: String) in (text as NSString).size(withAttributes: [.font: theme.nsFont(size: theme.uiSize - 1, bold: lit == out.id)]).width }
        let count = out.count > 0 ? width("×\(out.count)") + theme.spacing.m : 0
        return width(out.label) > metrics.cardWidth - 26 * k - count
    }

    private var fill: Color {
        card.attention ? theme.toneBg(theme.warningFill) : card.pending || card.ghost ? .clear : theme.surface1
    }

    @ViewBuilder
    private func border(_ shape: RoundedRectangle, k: CGFloat) -> some View {
        if card.attention {
            shape.strokeBorder(theme.warningFill, lineWidth: 1.5)
        } else if card.ghost {
            shape.strokeBorder(theme.border, style: StrokeStyle(lineWidth: 1, dash: [4 * k, 3 * k]))
        } else if card.pending {
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

struct FlowGlyph: View {
    let icon: String?
    let tone: String?
    let attention: Bool
    let pending: Bool
    let size: CGFloat

    @State private var pulse = false
    @Environment(\.theme) private var theme

    init(card: SDUIFlow.Card, size: CGFloat) {
        self.init(icon: card.icon, tone: card.tone, attention: card.attention, pending: card.pending || card.ghost, size: size)
    }

    init(actor: SDUILanes.Actor, size: CGFloat) {
        self.init(icon: actor.icon, tone: actor.tone, attention: actor.attention, pending: actor.pending, size: size)
    }

    init(icon: String?, tone: String?, attention: Bool, pending: Bool, size: CGFloat) {
        self.icon = icon
        self.tone = tone
        self.attention = attention
        self.pending = pending
        self.size = size
    }

    var body: some View {
        if attention {
            symbol(SymbolName.resolve((icon ?? "exclamationmark.circle") + ".fill", fallback: "exclamationmark.circle.fill"))
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
        } else if pending {
            symbol(SymbolName.resolve(icon, fallback: "circle.dashed"))
                .foregroundStyle(theme.faint)
        } else {
            symbol(SymbolName.resolve(icon, fallback: "circle"))
                .foregroundStyle(theme.tone(tone) ?? theme.muted)
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
