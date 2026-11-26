import AppKit
import SwiftUI

import ClubmateKit

extension EnvironmentValues {
    @Entry var guidePaneShown = false
}

struct GuideStepRowView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        if let step = node.guideStep() {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                Image(systemName: "book")
                    .foregroundStyle(theme.success)
                Text(step.label)
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
                if let source = step.node {
                    Text(source)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
            }
            .font(theme.ui)
            .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            UnsupportedNodeView(type: node.type)
        }
    }
}

struct GuideStepsView: View {
    let node: SDUINode

    @State private var width: CGFloat = 640
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme
    @Environment(\.composerFocusSlot) private var slot

    private static let hintWidth: CGFloat = 640

    var body: some View {
        if let controls = node.stepControls() {
            FlowLayout(spacing: theme.spacing.m) {
                ForEach(controls.actions) { action in
                    StepButton(action: action, pressed: controls.pressed == action.id, hint: width >= Self.hintWidth) { press(action) }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
            .keyMonitor(active: controls.actions.contains { $0.enabled && $0.id != GuideTour.continueAction }) { event in handle(controls, event) }
        } else {
            UnsupportedNodeView(type: node.type)
        }
    }

    private func press(_ action: SDUIStepAction) {
        guard action.enabled else { return }
        dispatch(node, .named("stepAct", .object(["action": .string(action.id)])))
    }

    private func handle(_ controls: SDUIStepControls, _ event: NSEvent) -> Bool {
        let flags = event.modifierFlags
        guard inPane(event.window),
              let action = GuideTour.stepAction(in: controls.actions, keyCode: event.keyCode, command: flags.contains(.command), control: flags.contains(.control), option: flags.contains(.option), shift: flags.contains(.shift))
        else { return false }
        press(action)
        return true
    }

    private func inPane(_ window: NSWindow?) -> Bool {
        guard let window, let slot else { return false }
        let responder = window.firstResponder
        if let text = responder as? NSTextView, text.isEditable, !text.string.isEmpty || text.hasMarkedText() { return false }
        if responder == nil || responder === window || responder === window.contentView || responder === window.contentView?.superview { return true }
        return PaneMarker.holds(slot, responder)
    }
}

private struct StepButton: View {
    let action: SDUIStepAction
    let pressed: Bool
    let hint: Bool
    let press: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: press) {
            HStack(spacing: theme.spacing.m) {
                Text(action.label)
                if hint, let key = action.shortcut.flatMap({ KeyCombo($0)?.label }) {
                    KeyCapLabel(label: key, onAccent: action.primary)
                }
            }
            .font(theme.ui)
            .foregroundStyle(action.primary ? theme.onAccent : pressed ? theme.accentInk : theme.foreground)
            .padding(theme.spacing.capsule)
            .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(action.primary ? theme.accent : pressed ? theme.selected : theme.surface2))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(pressed ? theme.accent : action.primary ? .clear : theme.border, lineWidth: 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!action.enabled)
        .opacity(action.enabled ? 1 : 0.42)
        .testRow("step:" + action.id, label: action.label, selected: pressed)
    }
}

struct TourGlyph: View {
    let state: String
    var size: CGFloat = 12

    @Environment(\.theme) private var theme

    var body: some View {
        ZStack {
            switch state {
            case "known":
                Circle().fill(theme.success)
                Image(systemName: "checkmark")
                    .font(.system(size: size * 0.6, weight: .heavy))
                    .foregroundStyle(theme.background)
            case "shaky":
                Circle().strokeBorder(theme.warning, lineWidth: 1.5)
                Circle()
                    .fill(theme.warning)
                    .mask(alignment: .trailing) { Rectangle().frame(width: size / 2) }
            case "missed":
                Circle().strokeBorder(theme.danger, lineWidth: 1.5)
                Image(systemName: "xmark")
                    .font(.system(size: size * 0.5, weight: .heavy))
                    .foregroundStyle(theme.danger)
            case "current":
                Circle().strokeBorder(theme.accent, lineWidth: 1.5)
                Circle().fill(theme.accent).frame(width: size * 0.34, height: size * 0.34)
            default:
                Circle().strokeBorder(theme.muted, style: StrokeStyle(lineWidth: 1, dash: [2, 2]))
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

private struct TourTreeView: View {
    let node: SDUINode
    let tour: SDUITour
    var maxHeight: CGFloat = .infinity
    var done: (() -> Void)?

    @State private var selection = TourSelection()
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    private var ids: [String] { tour.concepts.map(\.id) }

    var body: some View {
        let selected = selection.at(ids, current: tour.current)
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(tour.concepts) { concept in
                        row(concept, selected: concept.id == selected)
                            .id(concept.id)
                        if selection.opened == concept.id, !concept.offers.isEmpty {
                            offers(concept)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .onChange(of: selection.selected) { _, id in
                guard let id else { return }
                proxy.scrollTo(id)
            }
        }
        .frame(maxHeight: maxHeight)
        .fixedSize(horizontal: false, vertical: maxHeight.isFinite)
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .onKeyPress(.upArrow) { move(-1) }
        .onKeyPress(.downArrow) { move(1) }
        .onKeyPress(.return) {
            guard let selected = selection.at(ids, current: tour.current), let concept = tour.concepts.first(where: { $0.id == selected }) else { return .ignored }
            activate(concept)
            return .handled
        }
        .onAppear { if done != nil { focused = true } }
    }

    private func move(_ delta: Int) -> KeyPress.Result {
        selection.move(delta, in: ids, current: tour.current)
        return .handled
    }

    private func activate(_ concept: SDUITourConcept) {
        if concept.offers.isEmpty {
            selection.select(concept.id)
        } else {
            selection.toggleOffers(for: concept.id)
        }
    }

    private func row(_ concept: SDUITourConcept, selected: Bool) -> some View {
        HStack(spacing: theme.spacing.m) {
            Color.clear.frame(width: CGFloat(concept.depth) * theme.indent, height: 1)
            TourGlyph(state: concept.state, size: theme.uiSize - 1)
            Text(concept.label)
                .font(concept.state == "current" ? theme.strong : theme.ui)
                .foregroundStyle(concept.state == "not_yet" ? theme.muted : theme.foreground)
                .lineLimit(1)
                .truncationMode(.tail)
            ForEach(concept.links) { link in
                Button { selection.select(link.id) } label: {
                    Text("↗ " + link.label)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                        .padding(theme.spacing.badge)
                        .overlay(RoundedRectangle(cornerRadius: theme.radius.s).strokeBorder(theme.borderStrong))
                }
                .buttonStyle(.plain)
            }
            Spacer(minLength: theme.spacing.m)
            Text(concept.stateLabel)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
        }
        .padding(.vertical, theme.spacing.s)
        .padding(.horizontal, theme.gutter)
        .background(selected ? theme.selected : .clear)
        .overlay(alignment: .leading) {
            if selected { theme.accent.frame(width: 2) }
        }
        .contentShape(Rectangle())
        .onTapGesture {
            focused = true
            activate(concept)
        }
        .testRow("\(tour.session)/tour#\(concept.id)", label: concept.label, selected: selected)
    }

    private func offers(_ concept: SDUITourConcept) -> some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                TourGlyph(state: concept.state, size: theme.uiSize - 1)
                Text(concept.label)
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
                if !concept.stepLabel.isEmpty {
                    Text(concept.stepLabel)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
            }
            if let note = concept.note {
                Text(note)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.text2)
                    .fixedSize(horizontal: false, vertical: true)
            }
            FlowLayout(spacing: theme.spacing.m) {
                ForEach(Array(concept.offers.enumerated()), id: \.element.id) { index, offer in
                    Button {
                        dispatch(node, .named("tourOffer", .object(["concept": .string(concept.id), "offer": .string(offer.id)])))
                        selection.closeOffers()
                        done?()
                    } label: {
                        Text(offer.label)
                            .font(theme.uiSmall)
                            .foregroundStyle(index == 0 ? theme.onAccent : theme.foreground)
                            .padding(theme.spacing.chip)
                            .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(index == 0 ? theme.accent : theme.surface3))
                            .overlay(RoundedRectangle(cornerRadius: theme.radius.s).strokeBorder(index == 0 ? .clear : theme.border))
                    }
                    .buttonStyle(.plain)
                    .testRow("\(tour.session)/tour#\(concept.id)/\(offer.id)", label: offer.label, selected: false)
                }
            }
        }
        .padding(theme.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.surface2))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).strokeBorder(theme.border))
        .padding(.horizontal, theme.gutter)
        .padding(.vertical, theme.spacing.s)
    }
}

struct TourPaneView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme

    var body: some View {
        if let tour = node.tour() {
            VStack(alignment: .leading, spacing: 0) {
                VStack(alignment: .leading, spacing: theme.spacing.s) {
                    HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                        Text(tour.title)
                            .font(theme.strong)
                            .foregroundStyle(theme.foreground)
                        Text(tour.stepLabel)
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.muted)
                    }
                    Text(tour.summary)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
                .padding(theme.gutter)
                TourTreeView(node: node, tour: tour)
                legend(tour)
            }
            .font(theme.ui)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        } else {
            UnsupportedNodeView(type: node.type)
        }
    }

    private func legend(_ tour: SDUITour) -> some View {
        FlowLayout(spacing: theme.spacing.xl) {
            ForEach(tour.legend) { entry in
                HStack(spacing: theme.spacing.s) {
                    TourGlyph(state: entry.state, size: theme.uiSize - 2)
                    Text(entry.label)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
            }
        }
        .padding(theme.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) { theme.border.frame(height: 1) }
    }
}

struct GuideStripView: View {
    let node: SDUINode

    @State private var open = false
    @Environment(\.guidePaneShown) private var pane
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    private static let treeHeight: CGFloat = 360
    private static let popoverWidth: CGFloat = 320
    private static let labelWidth: CGFloat = 140

    var body: some View {
        if let tour = node.tour() {
            Group {
                if pane { chip(tour) } else { strip(tour) }
            }
            .keyMonitor(active: true) { event in handle(tour, event) }
        } else {
            UnsupportedNodeView(type: node.type)
        }
    }

    private func chip(_ tour: SDUITour) -> some View {
        Text(tour.stepLabel)
            .font(theme.uiSmall)
            .foregroundStyle(theme.success)
            .padding(theme.spacing.badge)
            .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(theme.toneBg(theme.success)))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.s).strokeBorder(theme.toneBorder(theme.success)))
            .fixedSize()
            .help(tour.summary)
    }

    private func strip(_ tour: SDUITour) -> some View {
        Button { open.toggle() } label: {
            HStack(spacing: theme.spacing.l) {
                HStack(spacing: theme.spacing.s) {
                    ForEach(tour.concepts) { concept in
                        TourGlyph(state: concept.state, size: theme.uiSize - 3)
                    }
                }
                if let current = tour.currentConcept {
                    Text("\(tour.step) \(current.label)")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.text2)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .frame(maxWidth: Self.labelWidth, alignment: .leading)
                        .fixedSize(horizontal: true, vertical: false)
                } else {
                    Text(tour.stepLabel)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.text2)
                }
            }
            .padding(theme.spacing.capsule)
            .background(Capsule().fill(theme.surface2))
            .overlay(Capsule().strokeBorder(theme.border))
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .fixedSize()
        .help(tour.summary)
        .popover(isPresented: $open, arrowEdge: .bottom) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                    Text(tour.title)
                        .font(theme.strong)
                        .foregroundStyle(theme.foreground)
                    Text(tour.stepLabel)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
                .padding(theme.gutter)
                TourTreeView(node: node, tour: tour, maxHeight: Self.treeHeight) { open = false }
            }
            .font(theme.ui)
            .frame(width: Self.popoverWidth)
            .padding(.bottom, theme.gutter)
        }
        .testNode("\(tour.session)/strip", role: "button", label: tour.stepLabel)
    }

    private func handle(_ tour: SDUITour, _ event: NSEvent) -> Bool {
        guard let combo = KeyCombo(tour.shortcut ?? ""), combo.fires(on: event) else { return false }
        dispatch(node, .named("tourPanel", .null))
        return true
    }
}
