import AppKit
import SwiftUI

import ClubmateKit

struct QuizCardView: View {
    let node: SDUINode

    var body: some View {
        if let quiz = node.quiz() {
            if quiz.asking {
                QuizAskingCard(node: node, quiz: quiz)
            } else {
                QuizRecordCard(node: node, quiz: quiz)
            }
        } else {
            UnsupportedNodeView(type: node.type)
        }
    }
}

@MainActor
private func quizEvent(_ dispatch: SDUIDispatch, _ node: SDUINode, _ name: String, _ fields: [String: JSONValue] = [:]) {
    var body = fields
    body["request"] = .string(node.string("request") ?? "")
    dispatch(node, .named(name, .object(body)))
}

private func shortcutLabel(_ shortcut: String?) -> String? {
    shortcut.flatMap { KeyCombo($0)?.label }
}

private extension Theme {
    var quizRow: CGFloat { (uiSize * 19 / 12).rounded() }
}

private struct QuizAskingCard: View {
    let node: SDUINode
    let quiz: SDUIQuiz

    @State private var fallback = QuizCardModel()
    @State private var paneHeight: CGFloat = 800
    @State private var holdStart: Date?
    @FocusState private var focused: Bool
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme
    @Environment(\.composerDrafts) private var drafts
    @Environment(\.composerFocusSlot) private var slot
    @Environment(\.focusRouter) private var router

    private static let returnKey: UInt16 = 36
    private static let downKey: UInt16 = 125
    private static let upKey: UInt16 = 126

    private var model: QuizCardModel { drafts?.quizCard(for: node.id ?? "") ?? fallback }
    private var session: String { ComposerDrafts.session(of: node.id ?? "") }
    private var excerpt: SDUIQuizExcerpt? { quiz.excerpt }
    private var ranging: Bool { excerpt?.pick == "range" }
    private var picks: Bool { excerpt.map { $0.pick == "line" || $0.pick == "range" } ?? false }
    private var drafting: Bool { excerpt.flatMap { model.span($0) } != nil }
    private var rates: Bool { quiz.confidence?.enabled == true && !drafting }
    private var peek: Bool {
        guard let excerpt, !model.bar else { return false }
        return model.peeks(excerpt, rowHeight: theme.quizRow, chrome: theme.header * 5, pane: paneHeight)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            if !model.bar {
                question
                if let excerpt {
                    QuizExcerptBox(request: node.string("request") ?? "", excerpt: excerpt, rows: peek ? model.window(excerpt) : excerpt.rows, cursor: picks ? model.at(excerpt) : nil, span: picks ? model.span(excerpt) : nil, focused: focused, keys: true, picks: picks, onPoint: point, onDrag: drag, onDragEnd: dragEnd, onOpen: open)
                        .padding(.horizontal, theme.gutter)
                        .padding(.bottom, theme.gutter)
                }
                if !quiz.options.isEmpty { options }
            }
            footer
        }
        .font(theme.ui)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.toneBg(theme.warning)))
        .clipShape(RoundedRectangle(cornerRadius: theme.radius.l))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.toneBorder(theme.warning), lineWidth: 1))
        .blockBar(node.id)
        .focusable()
        .focusEffectDisabled()
        .focused($focused)
        .reportsFocus(.quiz, focused)
        .onTapGesture { focused = true }
        .modifier(CardFocusTarget(focused: $focused))
        .padding(.horizontal, theme.gutter * 2)
        .padding(.bottom, theme.spacing.l)
        .onGeometryChange(for: CGFloat.self) { proxy in
            _ = proxy.frame(in: .global).minY
            return NSApp.promptWindow?.contentLayoutRect.height ?? 800
        } action: { paneHeight = $0 }
        .task(id: node.string("request")) {
            try? await Task.sleep(for: cardSettle)
            if cardMayTakeKeys(drafts, node: node) { focused = true }
        }
        .onChange(of: quiz.sending ? (quiz.confidence?.value ?? "") : nil, initial: true) { _, hold in
            holdStart = hold == nil ? nil : .now
        }
        .onChange(of: quiz.composerNeedsRating, initial: true) { _, unrated in
            drafts?.quiz(session: session, needsRating: unrated)
        }
        .onDisappear { drafts?.quiz(session: session, needsRating: false) }
        .keyMonitor(active: true, claims: claim, handle: handle)
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
            Image(systemName: "exclamationmark.circle")
                .foregroundStyle(theme.warning)
            Text(quiz.title)
                .font(theme.strong)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .fixedSize()
            if model.bar {
                if let concept = quiz.concept { QuizConceptChip(text: concept) }
                Text(quiz.confidence?.hint ?? quiz.hint ?? "")
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
            } else {
                let sub = [quiz.step, quiz.hint].compactMap { $0 }.joined(separator: " · ")
                Text(sub)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
                    .help(sub)
            }
            Spacer(minLength: theme.spacing.m)
            if model.bar {
                skip
            }
            Button { model.bar.toggle() } label: {
                HStack(spacing: theme.spacing.s) {
                    Image(systemName: model.bar ? "chevron.up" : "chevron.down")
                        .font(theme.glyph)
                    if !model.bar { Text(quiz.labels.fold) }
                    KeyCapLabel(label: "⌥Q")
                }
                .font(theme.uiSmall)
                .foregroundStyle(theme.text2)
            }
            .buttonStyle(.plain)
            .help(quiz.labels.fold)
        }
        .padding(theme.gutter)
    }

    private var question: some View {
        HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
            if let concept = quiz.concept { QuizConceptChip(text: concept) }
            Text(quiz.question)
                .font(theme.strong)
                .foregroundStyle(theme.foreground)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, theme.gutter)
        .padding(.bottom, theme.gutter)
    }

    private var options: some View {
        VStack(spacing: theme.spacing.s) {
            ForEach(quiz.options) { option in
                Button { choose(option) } label: {
                    HStack(spacing: theme.spacing.l) {
                        Image(systemName: option.on ? "largecircle.fill.circle" : "circle")
                            .foregroundStyle(option.on ? theme.accentInk : theme.muted)
                        Text(option.label)
                            .foregroundStyle(theme.foreground)
                            .fixedSize(horizontal: false, vertical: true)
                        Spacer(minLength: theme.spacing.m)
                        KeyCapLabel(label: option.key)
                    }
                    .padding(theme.gutter)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(option.on ? theme.selected : theme.surface2))
                    .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(option.on ? theme.accent : theme.border, lineWidth: 1))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .testRow("\(node.string("request") ?? "")#\(option.value)", label: option.label, selected: option.on)
            }
        }
        .padding(.horizontal, theme.gutter)
        .padding(.bottom, theme.gutter)
    }

    @ViewBuilder
    private var footer: some View {
        if let confidence = quiz.confidence {
            Group {
                if quiz.sending {
                    QuizHoldRow(confidence: confidence, quiz: quiz, start: holdStart ?? .now, enabled: rates, rate: rate) { undo() }
                } else if model.bar {
                    EmptyView()
                } else {
                    ViewThatFits(in: .horizontal) {
                        footerRow(confidence, hint: true)
                        footerRow(confidence, hint: false)
                        VStack(alignment: .leading, spacing: theme.spacing.m) {
                            HStack(spacing: theme.spacing.l) {
                                Text(confidence.label)
                                    .font(theme.uiSmall)
                                    .foregroundStyle(theme.muted)
                                    .fixedSize()
                                QuizChips(levels: confidence.levels, value: confidence.value, enabled: rates, press: rate)
                            }
                            HStack(spacing: theme.spacing.l) {
                                Spacer(minLength: 0)
                                allLines
                                skip
                            }
                        }
                    }
                }
            }
            .padding(theme.gutter)
            .frame(maxWidth: .infinity, alignment: .leading)
            .overlay(alignment: .top) {
                if !model.bar { theme.toneBorder(theme.warning).frame(height: 1) }
            }
        } else if !model.bar {
            HStack {
                Spacer(minLength: 0)
                skip
            }
            .padding(theme.gutter)
        }
    }

    private func footerRow(_ confidence: SDUIQuizConfidence, hint: Bool) -> some View {
        HStack(spacing: theme.spacing.l) {
            Text(confidence.label)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .fixedSize()
            QuizChips(levels: confidence.levels, value: confidence.value, enabled: rates, press: rate)
            if hint, let text = confidence.hint {
                Text(text)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
            }
            Spacer(minLength: theme.spacing.m)
            allLines
            skip
        }
    }

    @ViewBuilder
    private var allLines: some View {
        if peek, let excerpt {
            Button { model.full = true } label: {
                Text(quiz.labels.allLines.replacing("{n}", with: String(excerpt.lines.count)))
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.text2)
                    .fixedSize()
            }
            .buttonStyle(.plain)
        }
    }

    private var skip: some View {
        Button { quizEvent(dispatch, node, "quizSkip") } label: {
            Text(quiz.labels.skip)
                .font(theme.uiSmall)
                .foregroundStyle(theme.text2)
                .fixedSize()
        }
        .buttonStyle(.plain)
    }

    private func claim(_ event: NSEvent) -> KeyClaim? {
        let flags = event.modifierFlags.intersection([.command, .control, .shift, .option])
        let key = event.charactersIgnoringModifiers?.lowercased()
        if flags == .option, key == "q" || key == "g" { return .docked }
        if flags == .command, key == "z", quiz.sending { return .docked }
        return nil
    }

    private func handle(_ event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection([.command, .control, .shift, .option])
        let key = event.charactersIgnoringModifiers?.lowercased() ?? ""
        if flags == .option {
            switch key {
            case "q": if !event.isARepeat { model.bar.toggle() }
            case "g": if !event.isARepeat { open() }
            default: return false
            }
            return true
        }
        if flags == .command, key == "z", quiz.sending {
            if !event.isARepeat { undo() }
            return true
        }
        guard focused else { return false }
        if flags.isSubset(of: .shift), event.keyCode == Self.upKey || event.keyCode == Self.downKey, picks, let excerpt {
            model.move(event.keyCode == Self.upKey ? -1 : 1, extend: flags == .shift && ranging, in: excerpt)
            return true
        }
        if flags.isEmpty {
            if event.keyCode == Self.returnKey, picks, let excerpt {
                if !event.isARepeat { pick(excerpt) }
                return true
            }
            if let level = quiz.confidence?.levels.first(where: { $0.key == key }) {
                if !event.isARepeat, rates { rate(level) }
                return true
            }
            if let option = quiz.options.first(where: { $0.key == key }) {
                if !event.isARepeat { choose(option) }
                return true
            }
        }
        guard flags.isSubset(of: .shift) else { return false }
        guard let typed = event.characters, let scalar = typed.unicodeScalars.first, !CharacterSet.controlCharacters.contains(scalar), scalar.value < 0xF700 else { return false }
        guard drafts?.type(typed, session: session) == true, let slot else { return false }
        router?.request(slot)
        return true
    }

    private func pick(_ excerpt: SDUIQuizExcerpt) {
        guard let range = model.pick(excerpt) else { return }
        model.picked()
        quizEvent(dispatch, node, "quizPick", ["from": .number(Double(range.lowerBound)), "to": .number(Double(range.upperBound))])
    }

    private func point(_ line: Int) {
        focused = true
        model.point(at: line)
        quizEvent(dispatch, node, "quizPick", ["from": .number(Double(line)), "to": .number(Double(line))])
    }

    private func drag(_ first: Int, _ last: Int) {
        guard ranging else { return }
        focused = true
        model.drag(from: first, to: last)
    }

    private func dragEnd() {
        guard ranging, let excerpt else { return }
        if model.span(excerpt) == nil {
            model.picked()
            return
        }
        pick(excerpt)
    }

    private func choose(_ option: SDUIQuizOption) {
        focused = true
        quizEvent(dispatch, node, "quizPick", ["option": .string(option.value)])
    }

    private func rate(_ level: SDUIQuizLevel) {
        focused = true
        quizEvent(dispatch, node, "quizRate", ["confidence": .string(level.value)])
    }

    private func undo() {
        quizEvent(dispatch, node, "quizUndo")
    }

    private func open() {
        quizEvent(dispatch, node, "quizOpen")
    }
}

private struct QuizHoldRow: View {
    let confidence: SDUIQuizConfidence
    let quiz: SDUIQuiz
    let start: Date
    let enabled: Bool
    let rate: (SDUIQuizLevel) -> Void
    let undo: () -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        TimelineView(.animation(minimumInterval: 0.1)) { context in
            let total = Double(confidence.holdMs) / 1000
            let left = max(0, total - context.date.timeIntervalSince(start))
            HStack(spacing: theme.spacing.l) {
                Text(quiz.labels.sending)
                    .font(theme.uiSmall.weight(.semibold))
                    .foregroundStyle(theme.accentInk)
                    .fixedSize()
                QuizChips(levels: confidence.levels, value: confidence.value, enabled: enabled, press: rate)
                Text(summary(left))
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
                Spacer(minLength: theme.spacing.m)
                CardButton(label: quiz.labels.undo, shortcut: "⌘Z", action: undo)
            }
            .overlay(alignment: .top) {
                theme.accent
                    .frame(maxWidth: .infinity)
                    .frame(height: 2)
                    .scaleEffect(x: total > 0 ? left / total : 0, y: 1, anchor: .leading)
                    .offset(y: -theme.gutter - 1)
            }
        }
    }

    private func summary(_ left: Double) -> String {
        let what = confidence.hint.flatMap { $0.components(separatedBy: " · ").first }
        return [what, quiz.labels.secondsLeft.replacing("{n}", with: String(Int(left.rounded(.up))))].compactMap { $0 }.joined(separator: " · ")
    }
}

private struct QuizChips: View {
    let levels: [SDUIQuizLevel]
    let value: String?
    let enabled: Bool
    let press: (SDUIQuizLevel) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        HStack(spacing: theme.spacing.m) {
            ForEach(levels) { level in
                let on = level.value == value
                Button { press(level) } label: {
                    HStack(spacing: theme.spacing.s) {
                        Text(level.label)
                            .lineLimit(1)
                            .fixedSize()
                        KeyCapLabel(label: level.key, onAccent: on)
                    }
                    .font(theme.uiSmall)
                    .foregroundStyle(on ? theme.onAccent : theme.foreground)
                    .padding(theme.spacing.chip)
                    .background(Capsule().fill(on ? theme.accent : theme.surface2))
                    .overlay(Capsule().stroke(on ? Color.clear : theme.borderStrong, lineWidth: 1))
                    .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .disabled(!enabled)
                .opacity(enabled || on ? 1 : 0.45)
                .testRow("quiz.rate#\(level.value)", label: level.label, selected: on, keyCap: true)
            }
        }
    }
}

private struct QuizConceptChip: View {
    let text: String

    @Environment(\.theme) private var theme

    var body: some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(theme.text2)
            .lineLimit(1)
            .padding(theme.spacing.badge)
            .background(RoundedRectangle(cornerRadius: theme.radius.s).fill(theme.hover))
            .fixedSize()
    }
}

private struct QuizExcerptBox: View {
    let request: String
    let excerpt: SDUIQuizExcerpt
    let rows: [SDUIQuizRow]
    let cursor: Int?
    let span: ClosedRange<Int>?
    let focused: Bool
    let keys: Bool
    let picks: Bool
    var onPoint: (Int) -> Void = { _ in }
    var onDrag: (Int, Int) -> Void = { _, _ in }
    var onDragEnd: () -> Void = {}
    let onOpen: () -> Void

    @State private var dragFrom: Int?
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            theme.border.frame(height: 1)
            VStack(alignment: .leading, spacing: 0) {
                ForEach(rows) { row in
                    QuizLineRow(row: row, picked: row.n.map { excerpt.picked?.contains($0) == true } ?? false, drafting: row.n.map { span?.contains($0) == true } ?? false, cursor: picks && row.n != nil && row.n == cursor, focused: focused)
                        .frame(height: theme.quizRow)
                        .testRow("\(request)#\(row.n.map(String.init) ?? "fold")", label: row.text, selected: row.n.map { excerpt.picked?.contains($0) == true } ?? false, focused: row.n == cursor)
                        .onTapGesture {
                            if picks, let n = row.n { onPoint(n) }
                        }
                }
            }
            .gesture(picks ? dragGesture : nil)
            .padding(.vertical, theme.spacing.s)
        }
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.background))
        .clipShape(RoundedRectangle(cornerRadius: theme.radius.m))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.m).stroke(theme.border, lineWidth: 1))
    }

    private var header: some View {
        HStack(spacing: theme.spacing.m) {
            Image(systemName: "doc.text")
                .foregroundStyle(theme.muted)
            Text(excerpt.file)
                .font(theme.monoSmall)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .truncationMode(.path)
                .help(excerpt.file)
                .layoutPriority(1)
            ViewThatFits(in: .horizontal) {
                metaText(["@ " + excerpt.commit, excerpt.span].filter { !$0.isEmpty }.joined(separator: " · "))
                metaText("@ " + excerpt.commit)
            }
            Spacer(minLength: theme.spacing.m)
            if let open = excerpt.open {
                Button(action: onOpen) {
                    HStack(spacing: theme.spacing.s) {
                        Text(open.label)
                        if keys, let cap = shortcutLabel(open.shortcut) { KeyCapLabel(label: cap) }
                    }
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.text2)
                    .lineLimit(1)
                    .fixedSize()
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, theme.gutter)
        .padding(.vertical, theme.spacing.m)
    }

    private func metaText(_ text: String) -> some View {
        Text(text)
            .font(theme.uiSmall)
            .foregroundStyle(theme.faint)
            .lineLimit(1)
            .fixedSize()
    }

    private var dragGesture: some Gesture {
        DragGesture(minimumDistance: 4, coordinateSpace: .local)
            .onChanged { value in
                guard let first = dragFrom ?? line(at: value.startLocation.y), let last = line(at: value.location.y) else { return }
                dragFrom = first
                onDrag(first, last)
            }
            .onEnded { _ in
                dragFrom = nil
                onDragEnd()
            }
    }

    private func line(at y: CGFloat) -> Int? {
        guard !rows.isEmpty else { return nil }
        let index = min(max(Int(y / theme.quizRow), 0), rows.count - 1)
        return rows[index].n
    }
}

private struct QuizLineRow: View {
    let row: SDUIQuizRow
    let picked: Bool
    let drafting: Bool
    let cursor: Bool
    let focused: Bool

    @Environment(\.theme) private var theme

    var body: some View {
        if let fold = row.fold {
            HStack(spacing: theme.spacing.l) {
                Text("–")
                Text(fold)
            }
            .font(theme.monoSmall)
            .foregroundStyle(theme.faint)
            .padding(.leading, theme.gutter + theme.spacing.xl)
            .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            let tone = theme.tone(row.tone)
            HStack(spacing: 0) {
                mark(tone)
                    .frame(width: theme.gutter + theme.spacing.xl, alignment: .center)
                Text(row.n.map(String.init) ?? "")
                    .font(theme.monoSmall)
                    .foregroundStyle(theme.faint)
                    .frame(width: theme.spacing.section + theme.spacing.xs, alignment: .trailing)
                Text(row.text.replacing("\t", with: "    "))
                    .font(theme.mono)
                    .foregroundStyle(theme.foreground)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .padding(.leading, theme.spacing.xl)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .help(row.text)
                if let label = row.label {
                    Text(label)
                        .font(theme.monoSmall)
                        .foregroundStyle(tone ?? theme.muted)
                        .lineLimit(1)
                        .fixedSize()
                        .padding(.horizontal, theme.gutter)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(fill)
            .overlay {
                if cursor { Rectangle().strokeBorder(focused ? theme.accent : theme.borderStrong, lineWidth: theme.focusRing) }
            }
            .contentShape(Rectangle())
        }
    }

    @ViewBuilder
    private func mark(_ tone: Color?) -> some View {
        switch row.tone {
        case "success": Image(systemName: "checkmark").font(theme.glyph).foregroundStyle(tone ?? theme.success)
        case "danger": Image(systemName: "xmark").font(theme.glyph).foregroundStyle(tone ?? theme.danger)
        default: Color.clear
        }
    }

    private var fill: Color {
        switch row.tone {
        case "success": theme.toneBg(theme.success)
        case "danger": theme.toneBg(theme.danger)
        default: picked ? theme.selected : drafting ? theme.selectedIdle : .clear
        }
    }
}

private struct QuizRecordCard: View {
    let node: SDUINode
    let quiz: SDUIQuiz

    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.gutter) {
            header
            HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                if let concept = quiz.concept { QuizConceptChip(text: concept) }
                Text(quiz.question)
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let excerpt = quiz.excerpt {
                QuizExcerptBox(request: node.string("request") ?? "", excerpt: excerpt, rows: excerpt.rows, cursor: nil, span: nil, focused: false, keys: false, picks: false) {
                    quizEvent(dispatch, node, "quizOpen")
                }
            }
            if let reveal = quiz.reveal { revealView(reveal) }
        }
        .font(theme.ui)
        .padding(theme.gutter + theme.spacing.s)
        .frame(maxWidth: .infinity, alignment: .leading)
        .agentCard()
        .blockBar(node.id)
        .padding(.horizontal, theme.gutter * 2)
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
            switch quiz.state {
            case "checking": ProgressView().controlSize(.mini)
            case "skipped": Image(systemName: "forward.end").foregroundStyle(theme.muted)
            case "cancelled": Image(systemName: "xmark.circle").foregroundStyle(theme.muted)
            default: Image(systemName: "checkmark.circle").foregroundStyle(theme.success)
            }
            Text(quiz.title)
                .font(theme.strong)
                .foregroundStyle(theme.foreground)
                .lineLimit(1)
                .fixedSize()
            if let step = quiz.step {
                Text(step)
                    .font(theme.uiSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.label)
            }
        }
    }

    private func revealView(_ reveal: SDUIQuizReveal) -> some View {
        VStack(alignment: .leading, spacing: theme.gutter) {
            FlowLayout(spacing: theme.spacing.l) {
                verdict(reveal)
                Text("\(quiz.labels.you) \(reveal.yours)")
                    .foregroundStyle(theme.text2)
                if let confidence = reveal.confidence { rating(confidence, flagged: reveal.flag == "sure_wrong") }
                if let answer = reveal.answer {
                    Text("\(quiz.labels.answer) \(answer)")
                        .foregroundStyle(theme.text2)
                }
            }
            .font(theme.uiSmall)
            if let note = reveal.note {
                HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                    Image(systemName: "exclamationmark.circle")
                    Text(note)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .font(theme.uiSmall)
                .foregroundStyle(theme.danger)
            }
            if let why = reveal.why {
                HStack(alignment: .top, spacing: theme.spacing.l) {
                    theme.borderStrong.frame(width: 2)
                    InlineCardText(text: why, color: theme.text2, font: theme.uiSmall)
                        .lineLimit(2)
                        .help(why)
                }
                .fixedSize(horizontal: false, vertical: true)
            }
            if let ref = reveal.ref {
                Text([ref, quiz.excerpt.map { "@ " + $0.commit }].compactMap { $0 }.joined(separator: " "))
                    .font(theme.monoSmall)
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.path)
                    .help(ref)
            }
        }
    }

    private func verdict(_ reveal: SDUIQuizReveal) -> some View {
        let tone = Self.tone(reveal.verdict, theme)
        return HStack(spacing: theme.spacing.s) {
            switch reveal.verdict {
            case "correct": Image(systemName: "checkmark")
            case "close": Image(systemName: "circle.lefthalf.filled")
            case "wrong": Image(systemName: "xmark")
            default: ProgressView().controlSize(.mini)
            }
            Text(reveal.label)
        }
        .font(theme.uiSmall.weight(.semibold))
        .foregroundStyle(tone)
        .padding(theme.spacing.chip)
        .background(Capsule().fill(theme.toneBg(tone)))
        .overlay(Capsule().stroke(theme.toneBorder(tone), lineWidth: 1))
    }

    private func rating(_ text: String, flagged: Bool) -> some View {
        Text(text)
            .foregroundStyle(flagged ? theme.danger : theme.text2)
            .padding(theme.spacing.badge)
            .background(Capsule().fill(flagged ? theme.toneBg(theme.danger) : theme.hover))
            .overlay(Capsule().stroke(flagged ? theme.toneBorder(theme.danger) : theme.border, lineWidth: 1))
    }

    private static func tone(_ verdict: String, _ theme: Theme) -> Color {
        switch verdict {
        case "correct": theme.success
        case "close": theme.warning
        case "wrong": theme.danger
        default: theme.muted
        }
    }
}
