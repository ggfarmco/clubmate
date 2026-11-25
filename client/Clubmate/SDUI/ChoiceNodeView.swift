import AppKit
import SwiftUI

import ClubmateKit

struct ChoiceNodeView: View {
    let node: SDUINode

    static let foldWidth: CGFloat = 560
    static let otherOption = "Other"

    private static let numberSize: CGFloat = 18
    private static let accentBar: CGFloat = 2
    private static let quoteBar: CGFloat = 2
    private static let otherLines = 6

    private static func oneLine(_ text: String) -> String {
        text.replacing(#/\R+/#, with: " ")
    }

    @State private var picked: [String] = []
    @State private var otherOn = false
    @State private var otherText = ""
    @State private var focusOther = false
    @FocusState private var otherFocused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(FormState.self) private var form: FormState?
    @Environment(\.formNarrow) private var narrow
    @Environment(\.formAccordion) private var accordion
    @Environment(\.cardRowInset) private var rowInset
    @Environment(\.keysFocused) private var keysFocused
    @Environment(\.keysRefocus) private var keysRefocus
    @Environment(\.transcriptReveal) private var transcriptReveal
    @Environment(\.scrollProxy) private var scroll
    @Environment(\.theme) private var theme

    private var radio: Bool { node.string("style") == "radio" }
    private var options: [SDUIChoiceOption] { node.choiceOptions() }
    private var other: SDUIChoiceOther? { node.choiceOther() }
    private var question: String? { node.string("text") }
    private var number: Int? { node.props["number"]?.numberValue.flatMap { Int(exactly: $0) } }
    private var picksAtOnce: Bool { node.string("action") != nil }
    private var serverOpens: Bool { node.string("openAction") != nil }

    private var current: Bool {
        guard !serverOpens, let openChoice = form?.openChoice else { return node.bool("open") == true }
        return openChoice == node.id
    }

    private var folded: Bool { accordion && form != nil && question != nil && !current && (form?.questions.count ?? 0) > 1 }

    private var answer: String? {
        var labels = options.filter { picked.contains($0.value) }.map(\.label)
        if otherOn, !otherText.isEmpty { labels.append(otherText) }
        return labels.isEmpty ? nil : labels.joined(separator: ", ")
    }

    private var asideAction: String? { node.string("asideAction") }

    private func ask(_ option: String) -> (() -> Void)? {
        guard let asideAction else { return nil }
        return { dispatch(node, .named(asideAction, .string(option))) }
    }

    var body: some View {
        Group {
            if node.bool("pinned") == true {
                PinnedStrip(node: node, options: options, picked: picked) { pick($0) }
            } else if folded { foldedRow } else { unfolded }
        }
        .id(node.id)
        .onChange(of: current) { _, now in
            guard now, serverOpens, let id = node.id else { return }
            scroll?.scrollTo(id)
            transcriptReveal?.show(id)
        }
        .onChange(of: options, initial: true) { restore() }
        .onChange(of: otherText) { _, text in
            if text.contains(where: \.isNewline) { otherText = Self.oneLine(text) }
        }
        .onChange(of: other, initial: true) { old, new in
            guard let new, !otherFocused else { return }
            if new.on { otherOn = true }
            otherText = new.value ?? otherText
            store()
            if new.focus > (old?.focus ?? 0) {
                focusOther = true
                otherFocused = otherOn
            }
        }
    }

    private var foldedRow: some View {
        ChoicePressable(radius: theme.radius.m, probe: "question", press: open) {
            HStack(spacing: theme.spacing.m) {
                badges
                Text(node.string("summary") ?? question ?? "")
                    .foregroundStyle(theme.text2)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Text(answer ?? (radio ? "not answered" : "none ticked"))
                    .font(theme.uiSmall)
                    .foregroundStyle(answer == nil ? theme.faint : theme.success)
                    .lineLimit(1)
                    .truncationMode(.label)
                Image(systemName: "chevron.down")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
            }
            .padding(.vertical, rowInset > 0 ? theme.spacing.m : theme.spacing.xs)
            .padding(.horizontal, rowInset > 0 ? rowInset : theme.spacing.m)
        }
        .help(question ?? "")
    }

    private var unfolded: some View {
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            if let question {
                HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                    badges
                    Text("\(Text(question).font(theme.strong))\(note)")
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            if let quote = node.string("quote") {
                HStack(spacing: theme.spacing.m) {
                    theme.borderStrong.frame(width: Self.quoteBar)
                    Text(quote)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .fixedSize(horizontal: false, vertical: true)
                .padding(.leading, Self.numberSize + theme.spacing.m)
            }
            VStack(alignment: .leading, spacing: 1) {
                ForEach(options) { option in
                    ChoiceRow(probe: option.value, label: option.label, description: option.description, shortcut: option.shortcut, hint: option.hint, radio: radio, on: picked.contains(option.value), inline: !narrow, cursor: keysFocused && node.string("cursor") == option.value, tabStop: !serverOpens, aside: option.aside, ask: ask(option.value), askKey: serverOpens ? "A" : nil) { pick(option.value) }
                }
                if let other {
                    ChoiceRow(probe: "other", label: other.label, description: nil, shortcut: other.shortcut, hint: other.hint, radio: radio, on: otherOn, inline: !narrow, tabStop: !serverOpens, aside: other.aside, ask: other.aside == nil ? nil : ask(Self.otherOption), press: toggleOther)
                    if otherOn { otherField(other) }
                }
            }
            .padding(.leading, number == nil ? 0 : Self.numberSize)
        }
        .padding(.leading, number == nil ? 0 : theme.spacing.m)
        .padding(.vertical, rowInset > 0 ? theme.spacing.l : 0)
        .padding(.leading, max(rowInset - theme.spacing.m, 0))
        .padding(.trailing, rowInset)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .leading) {
            if number != nil, current {
                Rectangle().fill(theme.accent).frame(width: Self.accentBar)
            }
        }
    }

    @ViewBuilder
    private var badges: some View {
        if let number {
            Text("\(number)")
                .font(theme.uiSmall)
                .foregroundStyle(answer == nil ? theme.text2 : theme.success)
                .frame(width: Self.numberSize, height: Self.numberSize)
                .background(Circle().fill(answer == nil ? Color.clear : theme.toneBg(theme.success)))
                .overlay(Circle().stroke(answer == nil ? theme.borderStrong : theme.toneBorder(theme.success), lineWidth: 1))
        }
        if let title = node.string("title"), !title.isEmpty {
            Text(title)
                .font(theme.uiSmall)
                .foregroundStyle(theme.text2)
                .lineLimit(1)
                .padding(.horizontal, theme.spacing.s)
                .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(theme.hover))
                .fixedSize()
        }
    }

    private var note: Text {
        guard let note = node.string("note") else { return Text("") }
        return Text("  \(note)").font(theme.uiSmall).foregroundStyle(theme.muted)
    }

    private func otherField(_ other: SDUIChoiceOther) -> some View {
        let prompt = Text(other.placeholder ?? "").foregroundStyle(theme.muted)
        return TextField("", text: Binding(get: { otherText }, set: { typed($0, in: other) }), prompt: prompt, axis: .vertical)
            .lineLimit(1...Self.otherLines)
            .focused($otherFocused)
            .textFieldStyle(.plain)
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .clipped()
            .padding(.horizontal, theme.spacing.m)
            .padding(.vertical, theme.spacing.xs)
            .frame(minWidth: 0, maxWidth: .infinity, minHeight: theme.control, alignment: .leading)
            .fieldChrome(focused: otherFocused)
            .padding(.top, theme.spacing.xs)
            .id(other.id)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { old, new in
                guard otherFocused, new > old else { return }
                Task { @MainActor in scroll?.scrollTo(other.id) }
            }
            .onSubmit {
                form?.submit()
                keysRefocus?.take()
            }
            .modifier(ExitToKeys(refocus: keysRefocus, focused: otherFocused))
            .onAppear {
                guard focusOther else { return }
                focusOther = false
                otherFocused = true
                Task { @MainActor in scroll?.scrollTo(other.id, anchor: .bottom) }
            }
    }

    private var serverSeed: ChoiceSeed {
        ChoiceSeed(picks: options.filter(\.on).map(\.value), other: other.flatMap { $0.on ? $0.value ?? "" : nil })
    }

    private func restore() {
        guard let id = node.id else { return }
        let server = serverSeed
        var draftPicks: [String]?
        switch form?.draft(id) {
        case .string(let value)?: draftPicks = [value]
        case .array(let values)?: draftPicks = values.compactMap(\.stringValue)
        default: break
        }
        let draftOther = other.flatMap { form?.draft($0.id)?.stringValue }
        let local = draftPicks == nil && draftOther == nil ? nil : ChoiceSeed(picks: draftPicks ?? server.picks, other: draftOther ?? server.other)
        let seen = form?.seeds[id]
        let chosen = ChoiceSeed.resolve(draft: local, server: server, seen: seen)
        let serverWon = local != nil && chosen == server && seen != server
        form?.seeds[id] = server
        picked = chosen.picks
        if other != nil, let text = chosen.other {
            otherOn = true
            otherText = text
        } else if serverWon {
            otherOn = false
        }
        store()
    }

    private var value: JSONValue? {
        if radio { return picked.first.map(JSONValue.string) }
        return .array(picked.map(JSONValue.string))
    }

    private func store() {
        guard let form, let id = node.id else { return }
        form.values[id] = value
        if let other { form.values[other.id] = otherOn ? .string(Self.oneLine(otherText)) : nil }
    }

    private func commit() {
        store()
        guard let form, let id = node.id else { return }
        form.touch(id)
        if let other { form.touch(other.id) }
        if !serverOpens { form.openChoice = id }
        form.edited()
    }

    private func pick(_ option: String) {
        keysRefocus?.take()
        if radio {
            picked = [option]
            if !picksAtOnce { otherOn = false }
        } else if picked.contains(option) {
            picked.removeAll { $0 == option }
        } else {
            picked.append(option)
        }
        commit()
        if let form {
            if radio, accordion, !picksAtOnce, !serverOpens, let id = node.id, let next = form.question(after: id) { form.openChoice = next }
            if picksAtOnce { dispatch(node, .submit(.object(form.values))) }
        } else {
            dispatch(node, .submit(value ?? .null))
        }
    }

    private func toggleOther() {
        otherOn = radio || !otherOn
        if radio { picked = [] }
        focusOther = otherOn
        otherFocused = otherOn
        commit()
    }

    private func typed(_ text: String, in other: SDUIChoiceOther) {
        otherText = text
        guard otherOn else { return }
        store()
        form?.touch(other.id)
        form?.edited(after: FormState.typingDelay)
    }

    private func open() {
        keysRefocus?.take()
        if serverOpens, let id = node.id, let action = node.string("openAction") {
            dispatch(node, .named(action, .string(id)))
        } else {
            form?.openChoice = node.id
        }
    }
}

private struct ChoiceRow: View {
    let probe: String
    let label: String
    let description: String?
    let shortcut: String?
    var hint: String?
    let radio: Bool
    let on: Bool
    let inline: Bool
    var cursor = false
    var tabStop = true
    var aside: SDUIChoiceAside?
    var ask: (() -> Void)?
    var askKey: String?
    let press: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    private static let inlineGap = "   "

    private var combo: KeyCombo? { shortcut.flatMap(KeyCombo.init) }

    private var showsAsk: Bool { ask != nil && aside == nil && (hovering || cursor) }

    var body: some View {
        ChoicePressable(radius: theme.radius.s, fill: on ? theme.selected : nil, combo: combo, tabStop: tabStop, probe: probe, press: press) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                ChoiceMark(radio: radio, on: on)
                    .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 2 }
                text
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if aside != nil || showsAsk {
                    asideControl.hidden()
                }
                if let key = combo?.label ?? hint {
                    Text(key)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.faint)
                }
            }
            .padding(.vertical, inline ? theme.spacing.xs : theme.spacing.s)
            .padding(.horizontal, theme.spacing.m)
            .overlay {
                if cursor {
                    RoundedRectangle(cornerRadius: theme.radius.s).stroke(theme.accent, lineWidth: theme.focusRing)
                }
            }
        }
        .overlay(alignment: .trailing) {
            if aside != nil || showsAsk {
                asideControl
                    .padding(.trailing, theme.spacing.m + keyWidth)
            }
        }
        .onHover { hovering = $0 }
        .contextMenu {
            if let ask {
                Button(aside == nil ? "Ask aside" : "Open aside", action: ask)
            }
        }
        .accessibilityLabel(label)
        .accessibilityAddTraits(on ? .isSelected : [])
    }

    private var keyWidth: CGFloat {
        guard let key = combo?.label ?? hint else { return 0 }
        return CGFloat(key.count) * 7 + theme.spacing.m
    }

    @ViewBuilder
    private var asideControl: some View {
        if let aside {
            AsideMark(state: aside.state, press: ask ?? {})
                .testNode("\(probe)/aside", role: "button")
        } else {
            AsideAskButton(key: askKey, press: ask ?? {})
                .testNode("\(probe)/askAside", role: "button")
        }
    }

    @ViewBuilder
    private var text: some View {
        if let description, !description.isEmpty {
            if inline {
                Text("\(Text(label).foregroundStyle(theme.foreground))\(Self.inlineGap)\(Text(description).font(theme.uiSmall).foregroundStyle(theme.muted))")
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    Text(label).foregroundStyle(theme.foreground)
                    Text(description)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
            }
        } else {
            Text(label).foregroundStyle(theme.foreground)
        }
    }
}

private struct ChoiceMark: View {
    let radio: Bool
    let on: Bool

    @Environment(\.theme) private var theme

    private static let size: CGFloat = 13
    private static let line: CGFloat = 1.5
    private static let dotInset: CGFloat = 3.5
    private static let tickSize: CGFloat = 8

    var body: some View {
        Group {
            if radio {
                ZStack {
                    Circle().strokeBorder(on ? theme.accent : theme.borderStrong, lineWidth: Self.line)
                    if on {
                        Circle().fill(theme.accent).padding(Self.dotInset)
                    }
                }
            } else {
                ZStack {
                    if on {
                        RoundedRectangle(cornerRadius: theme.radius.s).fill(theme.accent)
                        Image(systemName: "checkmark")
                            .resizable()
                            .scaledToFit()
                            .frame(width: Self.tickSize, height: Self.tickSize)
                            .foregroundStyle(theme.onAccent)
                    } else {
                        RoundedRectangle(cornerRadius: theme.radius.s).strokeBorder(theme.borderStrong, lineWidth: Self.line)
                    }
                }
            }
        }
        .frame(width: Self.size, height: Self.size)
    }
}

private struct ChoicePressable<Label: View>: View {
    let radius: CGFloat
    var fill: Color?
    var combo: KeyCombo?
    var tabStop = true
    var probe = ""
    let press: () -> Void
    @ViewBuilder let label: () -> Label

    @State private var hovering = false
    @State private var keyboardFocus = false
    @FocusState private var focused: Bool
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: press) {
            label()
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RoundedRectangle(cornerRadius: radius).fill(fill ?? (hovering ? theme.hover : .clear)))
                .overlay {
                    if keyboardFocus {
                        RoundedRectangle(cornerRadius: radius)
                            .stroke(theme.accent, lineWidth: theme.focusRing)
                    }
                }
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .focusable(tabStop, interactions: .activate)
        .focusEffectDisabled()
        .focused($focused)
        .onChange(of: focused) { _, now in
            keyboardFocus = now && !ButtonNodeView.mouseEvents.contains(NSApp.currentEvent?.type ?? .keyDown)
        }
        .onKeyPress(keys: [.space, .return]) { _ in
            press()
            return .handled
        }
        .onHover { hovering = $0 }
        .keyMonitor(active: combo != nil, overTerminals: combo?.passesTerminals == true) { event in
            guard combo?.fires(on: event) == true else { return false }
            press()
            return true
        }
        .testRow(probe, label: probe, selected: fill != nil, focused: keyboardFocus, hovered: hovering)
    }
}

private struct AsideMark: View {
    let state: String
    let press: () -> Void

    @Environment(\.theme) private var theme

    private var ink: Color {
        switch state {
        case "thinking": theme.accentInk
        case "asks": theme.warning
        case "sent": theme.success
        default: theme.muted
        }
    }

    var body: some View {
        Button(action: press) {
            HStack(spacing: theme.spacing.xs) {
                Image(systemName: "bubble.left.and.bubble.right")
                Text("aside")
            }
            .font(theme.uiSmall)
            .foregroundStyle(ink)
            .padding(.horizontal, theme.spacing.s)
            .background(Capsule().fill(theme.surface2))
            .overlay(Capsule().stroke(theme.border, lineWidth: 1))
        }
        .buttonStyle(.plain)
        .help("Open the aside about this option")
    }
}

private struct AsideAskButton: View {
    let key: String?
    let press: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: press) {
            HStack(spacing: theme.spacing.xs) {
                Image(systemName: "bubble.left.and.bubble.right")
                Text("Ask aside")
                if let key {
                    Text(key)
                        .foregroundStyle(theme.faint)
                        .padding(.horizontal, theme.spacing.xs)
                        .overlay(RoundedRectangle(cornerRadius: theme.radius.s).stroke(theme.border, lineWidth: 1))
                }
            }
            .font(theme.uiSmall)
            .foregroundStyle(theme.text2)
            .padding(.horizontal, theme.spacing.s)
            .background(Capsule().fill(theme.surface2))
            .overlay(Capsule().stroke(theme.border, lineWidth: 1))
        }
        .buttonStyle(.plain)
        .help("Ask aside: a read-only side conversation about this option")
    }
}

private struct PinnedStrip: View {
    let node: SDUINode
    let options: [SDUIChoiceOption]
    let picked: [String]
    let pick: (String) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.m) {
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                Image(systemName: "exclamationmark.circle")
                    .foregroundStyle(theme.warning)
                if let note = node.string("note") {
                    Text(note)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.warning)
                        .lineLimit(1)
                        .truncationMode(.label)
                        .help(note)
                }
                if let title = node.string("title"), !title.isEmpty {
                    Text(title)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.text2)
                        .lineLimit(1)
                        .padding(.horizontal, theme.spacing.s)
                        .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(theme.hover))
                        .fixedSize()
                }
                Text(node.string("text") ?? "")
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
                    .fixedSize(horizontal: false, vertical: true)
                    .layoutPriority(1)
            }
            FlowLayout(spacing: theme.spacing.s) {
                ForEach(Array(options.enumerated()), id: \.element.id) { index, option in
                    PinnedChip(number: index + 1, label: option.label, on: picked.contains(option.value), marked: option.aside != nil) { pick(option.value) }
                        .testRow(option.value, label: option.value, selected: picked.contains(option.value), focused: false, hovered: false)
                }
            }
            if let quote = node.string("quote") {
                Text(quote)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(theme.spacing.l)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.toneBg(theme.warning)))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(theme.toneBorder(theme.warning), lineWidth: 1))
    }
}

private struct PinnedChip: View {
    let number: Int
    let label: String
    let on: Bool
    let marked: Bool
    let press: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: press) {
            HStack(spacing: theme.spacing.s) {
                Text("\(number)")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.faint)
                Text(label)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .truncationMode(.label)
                if marked {
                    Image(systemName: "bubble.left.and.bubble.right")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.accentInk)
                }
            }
            .padding(.horizontal, theme.spacing.m)
            .padding(.vertical, theme.spacing.xs)
            .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(on ? theme.selected : hovering ? theme.hover : theme.surface2))
            .overlay(RoundedRectangle(cornerRadius: theme.radius.s).stroke(on || marked ? theme.accent : theme.border, lineWidth: 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(label)
    }
}
