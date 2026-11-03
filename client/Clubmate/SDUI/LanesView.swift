import AppKit
import SwiftUI

import ClubmateKit

struct LanesNodeView: View {
    let node: SDUINode

    @State private var cursor: Int?
    @State private var hovered: FlowHop?
    @FocusState private var focused: Bool
    @Environment(\.isFocused) private var paneFocused
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    private var id: String { node.id ?? "" }
    private var views: GraphViews { .shared }

    var body: some View {
        let lanes = node.lanes() ?? SDUILanes(actors: [], moves: [])
        let open = lanes.title == nil || views.state.historyOpen(id, default: !lanes.collapsed)
        let refs = lanes.moves.indices.filter { lanes.moves[$0].ref != nil }
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            if let title = lanes.title {
                LanesRow(title: title, moves: lanes.moves.count, open: open) { views.toggleHistory(id, default: !lanes.collapsed) }
            }
            if open {
                LanesView(lanes: lanes, k: theme.uiSize / 12, headerWidth: FlowFrame.metrics(theme).cardWidth * 0.75, hovered: $hovered, cursor: focused ? cursor : nil) { index in
                    send(lanes, index)
                }
            }
        }
        .padding(.horizontal, theme.gutter)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .keyMonitor(active: focused) { handle($0, lanes: lanes, refs: refs, open: open) }
        .reportsFocus(.lanes(title: lanes.title != nil), focused)
        .onChange(of: paneFocused) { _, value in
            if value { focused = true }
        }
    }

    private func send(_ lanes: SDUILanes, _ index: Int) {
        guard lanes.moves.indices.contains(index), let ref = lanes.moves[index].ref else { return }
        focused = true
        cursor = index
        dispatch(node, .named("openMove", .string(ref)))
    }

    private func handle(_ event: NSEvent, lanes: SDUILanes, refs: [Int], open: Bool) -> Bool {
        guard event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return false }
        if event.keyCode == 36 || event.keyCode == 76 {
            guard let cursor, open else { return false }
            send(lanes, cursor)
            return true
        }
        switch event.keyCode {
        case 123, 124:
            guard open, !refs.isEmpty else { return false }
            let step = event.keyCode == 124 ? 1 : -1
            let index = cursor.flatMap(refs.firstIndex) ?? (step > 0 ? refs.count - 2 : refs.count)
            cursor = refs[min(max(index + step, 0), refs.count - 1)]
            return true
        default:
            break
        }
        switch event.characters {
        case "h" where lanes.title != nil:
            views.toggleHistory(id, default: !lanes.collapsed)
            return true
        default:
            return false
        }
    }
}

struct LanesRow: View {
    let title: String
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
                Text(title)
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
        .accessibilityLabel("\(title), \(moves) moves")
    }
}

struct LanesView: View {
    let lanes: SDUILanes
    let k: CGFloat
    let headerWidth: CGFloat
    @Binding var hovered: FlowHop?
    let cursor: Int?
    let openMove: (Int) -> Void

    @State private var viewport: CGFloat = 0
    @State private var position = ScrollPosition(edge: .trailing)
    @Environment(\.theme) private var theme

    private struct Hop {
        let from: CGPoint
        let to: CGPoint
        let label: String
        let last: Bool
        let hop: FlowHop?
    }

    private var step: CGFloat { 82 * k }

    private func laneY(_ index: Int) -> CGFloat {
        30 * k + 50 * k * CGFloat(index)
    }

    private func moveX(_ index: Int) -> CGFloat {
        30 * k + step * CGFloat(index)
    }

    var body: some View {
        let actors = lanes.actors
        let lane = Dictionary(actors.enumerated().map { ($0.element.id, $0.offset) }, uniquingKeysWith: { a, _ in a })
        let hops = hops(lane: lane)
        let end = endMove(lane: lane)
        let width = max(viewport, moveX(max(lanes.moves.count - 1, 0)) + (end == nil ? 0 : 84 * k) + 30 * k)
        let height = 10 * k + 50 * k * CGFloat(actors.count)
        HStack(alignment: .top, spacing: 0) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(actors, id: \.id) { actor in
                    HStack(spacing: 7 * k) {
                        FlowGlyph(actor: actor, size: 14 * k)
                        Text(actor.label)
                            .font(theme.strong)
                            .foregroundStyle(actor.pending ? theme.muted : theme.foreground)
                            .lineLimit(1)
                            .truncationMode(.tail)
                            .help(actor.label)
                    }
                    .padding(.leading, 11 * k)
                    .frame(height: 50 * k)
                }
            }
            .padding(.vertical, 5 * k)
            .frame(width: headerWidth, alignment: .leading)
            ScrollView(.horizontal) {
                ZStack(alignment: .topLeading) {
                    Canvas { context, _ in
                        for index in actors.indices {
                            var line = Path()
                            line.move(to: CGPoint(x: 0, y: laneY(index)))
                            line.addLine(to: CGPoint(x: width, y: laneY(index)))
                            context.stroke(line, with: .color(theme.border), lineWidth: 1)
                        }
                        for hop in hops {
                            draw(&context, hop)
                        }
                        for (index, move) in lanes.moves.enumerated() where move.cause < 0 {
                            guard let label = move.label, let row = lane[move.actor] else { continue }
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
                    ForEach(Array(lanes.moves.enumerated()), id: \.offset) { index, move in
                        if let row = lane[move.actor] {
                            LaneMoveDot(actor: actors[row], move: move, size: 14 * k, focused: cursor == index) { openMove(index) }
                                .id(index)
                                .position(x: moveX(index), y: laneY(row))
                        }
                    }
                }
                .frame(width: width, height: height, alignment: .topLeading)
                .onContinuousHover { phase in
                    let next: FlowHop? = switch phase {
                    case .active(let point): hops.first { near(point, $0) }?.hop
                    case .ended: nil
                    }
                    if next != hovered { hovered = next }
                }
            }
            .scrollPosition($position)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { viewport = $0 }
            .onChange(of: lanes.moves.count, initial: true) { position.scrollTo(edge: .trailing) }
            .onChange(of: viewport) { position.scrollTo(edge: .trailing) }
            .onChange(of: cursor) { _, index in
                if let index { withAnimation(Motion.disclosure) { position.scrollTo(id: index) } }
            }
            .scrollIndicators(.automatic)
            .clipped()
        }
        .padding(.bottom, theme.spacing.s)
    }

    private func hops(lane: [String: Int]) -> [Hop] {
        lanes.moves.enumerated().compactMap { index, move in
            guard move.cause >= 0, move.cause < index, let to = lane[move.actor] else { return nil }
            let cause = lanes.moves[move.cause]
            guard let from = lane[cause.actor] else { return nil }
            let hop = move.out.map { FlowHop(node: cause.actor, out: $0) }
            return Hop(from: CGPoint(x: moveX(move.cause), y: laneY(from)), to: CGPoint(x: moveX(index), y: laneY(to)), label: move.label ?? move.out ?? "", last: move.last, hop: hop)
        }
    }

    private func endMove(lane: [String: Int]) -> (move: Int, lane: Int, label: String)? {
        for (index, move) in lanes.moves.enumerated() {
            guard let end = move.end, let row = lane[move.actor] else { continue }
            return (index, row, end)
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
        FlowArrow.draw(&context, tip: end, from: c2, size: 4 * k, color: color, width: width)
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

private struct LaneMoveDot: View {
    let actor: SDUILanes.Actor
    let move: SDUILanes.Move
    let size: CGFloat
    let focused: Bool
    let open: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        if move.ref != nil {
            Button(action: open) { dot }
                .buttonStyle(.plain)
                .focusable(false)
                .help(move.tooltip)
                .accessibilityLabel(move.tooltip)
        } else {
            dot
                .help(move.tooltip)
                .accessibilityElement()
                .accessibilityLabel(move.tooltip)
        }
    }

    private var dot: some View {
        Group {
            if move.current {
                FlowGlyph(actor: actor, size: size)
            } else {
                let ink = theme.tone(move.tone) ?? theme.success
                Circle()
                    .fill(ink)
                    .frame(width: size * 5 / 14, height: size * 5 / 14)
                    .frame(width: size * 6 / 7, height: size * 6 / 7)
                    .background(Circle().fill(theme.background))
                    .overlay(Circle().stroke(ink, lineWidth: 1.5))
            }
        }
        .frame(width: size + 4, height: size + 4)
        .overlay {
            if focused {
                Circle().stroke(theme.accent, lineWidth: theme.focusRing)
            }
        }
        .contentShape(Circle())
    }
}
