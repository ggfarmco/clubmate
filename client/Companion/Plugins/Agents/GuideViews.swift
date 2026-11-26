import SwiftUI

import CompanionKit

struct StepHeaderRow: View {
    let step: GuideStep

    var body: some View {
        HStack(spacing: 6) {
            Text(step.label)
                .foregroundStyle(Color.captionInk)
                .lineLimit(1)
                .truncationMode(.tail)
                .layoutPriority(1)
            if let node = step.node, !node.isEmpty {
                Text(node)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
            }
        }
        .font(.footnote)
        .padding(.top, 6)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("guide.step")
    }
}

struct StepControlsRow: View {
    let steps: StepControls
    let wide: Bool
    let blocked: Bool
    let busy: String?
    let act: (StepControls.Action) async -> Void

    var body: some View {
        FlowLayout(spacing: 8) {
            ForEach(steps.actions) { action in
                capsule(action)
            }
        }
        .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("guide.steps")
    }

    private func capsule(_ action: StepControls.Action) -> some View {
        let on = steps.on(action)
        let tappable = action.enabled && !blocked && busy == nil
        return Button {
            run(action)
        } label: {
            HStack(spacing: 6) {
                if busy == action.id {
                    ProgressView().controlSize(.mini)
                }
                Text(action.label)
                    .font(.subheadline.weight(action.primary ? .semibold : .regular))
                    .lineLimit(1)
                if wide, let key = action.key {
                    Text(key.label)
                        .font(.caption2.monospaced())
                        .foregroundStyle(action.primary ? Color.onAccent.opacity(0.8) : Color.captionInk)
                }
            }
            .foregroundStyle(action.primary ? Color.onAccent : on ? Color.accentColor : Color.primary)
            .padding(.horizontal, 14)
            .frame(minHeight: 44)
            .background(action.primary ? Color.accentColor : on ? Color.selectedRow : Color.surface2, in: .capsule)
            .overlay {
                if on, !action.primary {
                    Capsule().strokeBorder(Color.accentColor, lineWidth: 1)
                }
            }
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .disabled(!tappable)
        .opacity(action.enabled ? 1 : 0.42)
        .modifier(GuideShortcut(key: action.key.flatMap { $0.command && $0.key == "return" ? nil : $0 }))
        .accessibilityLabel(action.label)
        .accessibilityAddTraits(on ? .isSelected : [])
        .accessibilityIdentifier("guide.step." + action.id)
    }

    private func run(_ action: StepControls.Action) {
        Task { await act(action) }
    }
}

struct GuideShortcut: ViewModifier {
    let key: GuideKey?

    func body(content: Content) -> some View {
        if let key, let equivalent = Self.equivalent(key.key) {
            content.keyboardShortcut(equivalent, modifiers: Self.modifiers(key))
        } else {
            content
        }
    }

    private static func equivalent(_ key: String) -> KeyEquivalent? {
        switch key {
        case "return", "enter": .return
        case "escape", "esc": .escape
        case "space": .space
        default: key.count == 1 ? KeyEquivalent(Character(key)) : nil
        }
    }

    private static func modifiers(_ key: GuideKey) -> EventModifiers {
        var modifiers: EventModifiers = []
        if key.command { modifiers.insert(.command) }
        if key.option { modifiers.insert(.option) }
        if key.shift { modifiers.insert(.shift) }
        if key.control { modifiers.insert(.control) }
        return modifiers
    }
}

struct TourGlyph: View {
    let state: TourState
    var size: CGFloat = 16

    var body: some View {
        ZStack {
            switch state {
            case .known:
                Circle().fill(Color.success)
                Image(systemName: "checkmark")
                    .font(.system(size: size * 0.55, weight: .bold))
                    .foregroundStyle(Color.onAccent)
            case .shaky:
                Circle().strokeBorder(Color.warning, lineWidth: 1.5)
                HalfDisc().fill(Color.warning)
            case .missed:
                Circle().strokeBorder(Color.danger, lineWidth: 1.5)
                Image(systemName: "xmark")
                    .font(.system(size: size * 0.5, weight: .bold))
                    .foregroundStyle(Color.danger)
            case .current:
                Circle().strokeBorder(Color.accentColor, lineWidth: 1.5)
                Circle().fill(Color.accentColor).frame(width: size * 0.4, height: size * 0.4)
            case .notYet, .other:
                Circle().strokeBorder(Color.captionInk, style: StrokeStyle(lineWidth: 1.2, dash: [2, 2]))
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

private struct LinkChip: View {
    let label: String

    var body: some View {
        Text("↗ " + label)
            .font(.caption)
            .foregroundStyle(Color.captionInk)
            .lineLimit(1)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(Color.surface2, in: .capsule)
    }
}

private struct HalfDisc: Shape {
    func path(in rect: CGRect) -> Path {
        var path = Path()
        path.addArc(center: CGPoint(x: rect.midX, y: rect.midY), radius: rect.width / 2, startAngle: .degrees(90), endAngle: .degrees(270), clockwise: false)
        path.closeSubpath()
        return path
    }
}

struct TourTree: View {
    let tour: TourMap
    var titled = true
    let blocked: Bool
    let offer: (TourMap.Concept, TourMap.Chip) async -> Void
    @State private var opened: String?
    @State private var busy: String?

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    heading
                        .padding(.bottom, 10)
                    ForEach(tour.concepts) { concept in
                        row(concept, proxy: proxy)
                            .id(concept.id)
                    }
                    legend
                        .padding(.top, 14)
                }
                .padding(16)
            }
            .onAppear {
                if let current = tour.current { proxy.scrollTo(current, anchor: .center) }
            }
        }
        .accessibilityIdentifier("guide.tour")
    }

    private var heading: some View {
        VStack(alignment: .leading, spacing: 2) {
            if titled {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(tour.title)
                        .font(.headline)
                        .lineLimit(1)
                    Text(tour.stepLabel)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
            }
            if !tour.summary.isEmpty {
                Text(tour.summary)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
            }
        }
    }

    private func row(_ concept: TourMap.Concept, proxy: ScrollViewProxy) -> some View {
        let open = opened == concept.id
        let current = concept.state == .current
        return VStack(alignment: .leading, spacing: 8) {
            Button {
                guard concept.opens else { return }
                withAnimation(.snappy) { opened = open ? nil : concept.id }
            } label: {
                label(concept, current: current)
                    .padding(.leading, CGFloat(concept.depth) * 16)
                    .frame(minHeight: 40)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(concept.label + ", " + concept.stateLabel + ", " + concept.stepLabel)
            .accessibilityAddTraits(current ? .isSelected : [])
            .accessibilityIdentifier("guide.concept." + concept.id)
            if open {
                detail(concept, proxy: proxy)
                    .padding(.leading, CGFloat(concept.depth) * 16 + 26)
                    .padding(.bottom, 8)
            }
        }
        .padding(.horizontal, 10)
        .background(current ? Color.selectedRow : .clear, in: .rect(cornerRadius: 10))
        .overlay(alignment: .leading) {
            if current {
                RoundedRectangle(cornerRadius: 1).fill(Color.accentColor).frame(width: 2).padding(.vertical, 6)
            }
        }
    }

    private func label(_ concept: TourMap.Concept, current: Bool) -> some View {
        HStack(spacing: 10) {
            TourGlyph(state: concept.state)
            Text(concept.label)
                .font(.subheadline.weight(current ? .semibold : .regular))
                .foregroundStyle(concept.state == .notYet ? Color.captionInk : Color.primary)
                .lineLimit(1)
                .truncationMode(.tail)
                .layoutPriority(1)
            ForEach(concept.links) { link in
                LinkChip(label: link.label)
            }
            Spacer(minLength: 8)
            Text(concept.stateLabel)
                .font(.caption)
                .foregroundStyle(Color.captionInk)
                .lineLimit(1)
        }
    }

    private func detail(_ concept: TourMap.Concept, proxy: ScrollViewProxy) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(concept.stepLabel)
                .font(.caption)
                .foregroundStyle(Color.captionInk)
            if let note = concept.note {
                Text(note)
                    .font(.footnote)
                    .lineLimit(2)
            }
            if !concept.links.isEmpty {
                FlowLayout(spacing: 6) {
                    ForEach(concept.links) { link in
                        Button("↗ " + link.label) {
                            withAnimation(.snappy) {
                                opened = link.id
                                proxy.scrollTo(link.id, anchor: .center)
                            }
                        }
                        .font(.footnote)
                        .buttonStyle(.plain)
                        .foregroundStyle(Color.accentColor)
                        .frame(minHeight: 32)
                    }
                }
            }
            if !concept.offers.isEmpty {
                FlowLayout(spacing: 8) {
                    ForEach(Array(concept.offers.enumerated()), id: \.element.id) { index, chip in
                        Button {
                            busy = chip.id
                            Task {
                                defer { busy = nil }
                                await offer(concept, chip)
                            }
                        } label: {
                            Text(chip.label)
                                .font(.subheadline.weight(index == 0 ? .semibold : .regular))
                                .foregroundStyle(index == 0 ? Color.onAccent : Color.primary)
                                .padding(.horizontal, 14)
                                .frame(minHeight: 44)
                                .background(index == 0 ? Color.accentColor : Color.surface2, in: .capsule)
                                .contentShape(.capsule)
                        }
                        .buttonStyle(.plain)
                        .disabled(blocked || busy != nil)
                        .accessibilityIdentifier("guide.offer." + chip.id)
                    }
                }
            }
        }
    }

    private var legend: some View {
        FlowLayout(spacing: 12) {
            ForEach(tour.legend) { item in
                HStack(spacing: 5) {
                    TourGlyph(state: item.state, size: 12)
                    Text(item.label)
                        .font(.caption)
                        .foregroundStyle(Color.captionInk)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }
}

struct TourSheet: View {
    let tour: TourMap
    let blocked: Bool
    let offer: (TourMap.Concept, TourMap.Chip) async -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            TourTree(tour: tour, titled: false, blocked: blocked, offer: offer)
                .navigationTitle(tour.title)
                .navigationSubtitle(tour.stepLabel)
                .toolbarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }
}

struct TourStrip: View {
    let tour: TourMap
    let blocked: Bool
    let offer: (TourMap.Concept, TourMap.Chip) async -> Void
    @State private var showing = false

    var body: some View {
        Button {
            showing = true
        } label: {
            HStack(spacing: 4) {
                ForEach(tour.concepts) { concept in
                    TourGlyph(state: concept.state, size: 12)
                }
                Text(tour.stripLabel)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
                    .padding(.leading, 4)
            }
            .padding(.horizontal, 10)
            .frame(minHeight: 32)
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .modifier(GuideShortcut(key: GuideKey(tour.shortcut)))
        .accessibilityLabel(tour.title + ", " + tour.stepLabel + ", " + tour.summary)
        .accessibilityIdentifier("guide.strip")
        .popover(isPresented: $showing) {
            TourTree(tour: tour, blocked: blocked) { concept, chip in
                await offer(concept, chip)
                showing = false
            }
            .frame(idealWidth: 360, idealHeight: 460)
            .presentationCompactAdaptation(.sheet)
        }
    }
}

struct TourPane: View {
    let tour: TourMap
    let blocked: Bool
    let offer: (TourMap.Concept, TourMap.Chip) async -> Void

    var body: some View {
        TourTree(tour: tour, blocked: blocked, offer: offer)
            .frame(width: 320)
            .background(Color.surface1)
    }
}
