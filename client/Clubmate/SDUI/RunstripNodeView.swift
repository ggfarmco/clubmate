import AppKit
import SwiftUI

import ClubmateKit

struct RunstripNodeView: View {
    let node: SDUINode

    @State private var width: CGFloat = 0
    @FocusState private var focused: Bool
    @Environment(\.isFocused) private var paneFocused
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        let strip = node.runstrip() ?? SDUIRunstrip(runs: [])
        let k = theme.uiSize / 12
        let inset = 16 * k
        ZStack(alignment: .topLeading) {
            Canvas { context, size in
                draw(&context, strip: strip, size: size, k: k, inset: inset)
            }
            ForEach(Array(strip.runs.enumerated()), id: \.offset) { _, run in
                RunDot(run: run, selected: run.ref != nil && run.ref == strip.selected, k: k) {
                    guard let ref = run.ref else { return }
                    focused = true
                    dispatch(node, .select(ref))
                }
                .position(x: strip.x(run.at, width: width, inset: inset), y: axisY(k))
            }
        }
        .frame(height: 62 * k)
        .frame(maxWidth: .infinity)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .padding(.horizontal, theme.gutter)
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .keyMonitor(active: focused) { event in
            guard event.modifierFlags.intersection([.command, .control, .option]).isEmpty, event.keyCode == 123 || event.keyCode == 124 else { return false }
            guard let ref = strip.step(from: strip.selected, by: event.keyCode == 124 ? 1 : -1) else { return false }
            if ref != strip.selected { dispatch(node, .select(ref)) }
            return true
        }
        .reportsFocus(.runstrip, focused)
        .onChange(of: paneFocused) { _, value in
            if value { focused = true }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Runs")
    }

    private func axisY(_ k: CGFloat) -> CGFloat {
        28 * k
    }

    private func draw(_ context: inout GraphicsContext, strip: SDUIRunstrip, size: CGSize, k: CGFloat, inset: CGFloat) {
        guard let range = strip.range else { return }
        let y = axisY(k)
        let x = { (date: Date) in strip.x(date, width: size.width, inset: inset) }
        var axis = Path()
        axis.move(to: CGPoint(x: inset, y: y))
        axis.addLine(to: CGPoint(x: size.width - inset, y: y))
        context.stroke(axis, with: .color(theme.border), lineWidth: 1)

        for span in strip.paused {
            let from = x(span.from), to = x(span.to ?? strip.now ?? range.upperBound)
            guard to > from else { continue }
            let rect = CGRect(x: from, y: y - 6 * k, width: to - from, height: 12 * k)
            context.fill(Path(rect), with: .color(theme.background))
            var hatch = Path()
            var hx = rect.minX - rect.height
            while hx < rect.maxX {
                hatch.move(to: CGPoint(x: hx, y: rect.maxY))
                hatch.addLine(to: CGPoint(x: hx + rect.height, y: rect.minY))
                hx += 4 * k
            }
            context.drawLayer { layer in
                layer.clip(to: Path(rect))
                layer.stroke(hatch, with: .color(theme.faint), lineWidth: 1)
            }
            context.stroke(Path(rect), with: .color(theme.faint), style: StrokeStyle(lineWidth: 1, dash: [2 * k, 2 * k]))
            context.draw(Text("paused").font(theme.uiSmall).foregroundStyle(theme.muted), at: CGPoint(x: rect.midX, y: y - 9 * k), anchor: .bottom)
        }

        let format = Self.format(range)
        var labels: [CGFloat] = []
        if let now = strip.now {
            let nx = x(now)
            var line = Path()
            line.move(to: CGPoint(x: nx, y: y - 9 * k))
            line.addLine(to: CGPoint(x: nx, y: y + 9 * k))
            context.stroke(line, with: .color(theme.foreground), lineWidth: 1.25 * k)
            context.draw(Text("now").font(theme.uiSmall).foregroundStyle(theme.text2), at: CGPoint(x: nx, y: y - 11 * k), anchor: .bottom)
            context.draw(Text(format.string(from: now)).font(theme.uiSmall.monospacedDigit()).foregroundStyle(theme.muted), at: CGPoint(x: nx, y: y + 13 * k), anchor: .top)
            labels.append(nx)
        }
        if let next = strip.next {
            let nx = x(next)
            let ring = Path(ellipseIn: CGRect(x: nx - 5 * k, y: y - 5 * k, width: 10 * k, height: 10 * k))
            context.fill(ring, with: .color(theme.background))
            context.stroke(ring, with: .color(theme.accent), style: StrokeStyle(lineWidth: 1.25 * k, dash: [2 * k, 2 * k]))
            if strip.now.map({ abs(x($0) - nx) > 28 * k }) ?? true {
                context.draw(Text("next").font(theme.uiSmall).foregroundStyle(theme.accentInk), at: CGPoint(x: nx, y: y - 11 * k), anchor: .bottom)
            }
        }
        let room = 44 * k
        let ends: [(Date, CGFloat, UnitPoint)] = [(range.lowerBound, inset, .topLeading), (range.upperBound, size.width - inset, .topTrailing)]
        for (date, ex, anchor) in ends where !labels.contains(where: { abs($0 - ex) < room }) {
            context.draw(Text(format.string(from: date)).font(theme.uiSmall.monospacedDigit()).foregroundStyle(theme.muted), at: CGPoint(x: ex, y: y + 13 * k), anchor: anchor)
            labels.append(ex)
        }
    }

    private static func format(_ range: ClosedRange<Date>) -> DateFormatter {
        let formatter = DateFormatter()
        formatter.dateFormat = range.upperBound.timeIntervalSince(range.lowerBound) > 20 * 3600 ? "MMM d HH:mm" : "HH:mm"
        return formatter
    }
}

private struct RunDot: View {
    let run: SDUIRunstrip.Run
    let selected: Bool
    let k: CGFloat
    let select: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: select) {
            glyph
                .frame(width: 16 * k, height: 16 * k)
                .overlay {
                    if selected {
                        Circle().stroke(theme.accent, lineWidth: theme.focusRing)
                    }
                }
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .focusable(false)
        .disabled(run.ref == nil)
        .help(run.tooltip ?? run.status.rawValue)
        .accessibilityLabel(run.tooltip ?? run.status.rawValue)
    }

    @ViewBuilder
    private var glyph: some View {
        switch run.status {
        case .ok:
            Circle().fill(theme.success).frame(width: 7 * k, height: 7 * k)
        case .failed:
            Image(systemName: "xmark.circle")
                .resizable()
                .scaledToFit()
                .fontWeight(.semibold)
                .foregroundStyle(theme.danger)
                .background(Circle().fill(theme.background))
                .frame(width: 12 * k, height: 12 * k)
        case .missed:
            Circle().stroke(theme.warning, lineWidth: 1.5 * k).frame(width: 9 * k, height: 9 * k)
        case .running:
            Image(systemName: "play.fill")
                .resizable()
                .scaledToFit()
                .foregroundStyle(theme.accentInk)
                .frame(width: 9 * k, height: 9 * k)
        case .ignored:
            Circle().fill(theme.faint).frame(width: 4 * k, height: 4 * k)
        }
    }
}
