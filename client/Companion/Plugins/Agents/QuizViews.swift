import SwiftUI

import ClubmateProtocol
import CompanionKit

extension EnvironmentValues {
    @Entry var quizAction: ((String, QuizAction) -> Void)?
}

private enum QuizMetrics {
    static let row: CGFloat = 34
    static let peekLines = 3
    static let phoneLines = 7
    static let padLines = 8
}

struct QuizCard: View {
    let quiz: Quiz
    @Binding var draft: QuizDraft
    let wide: Bool
    var blocked: String?
    var composerEmpty = true
    let act: @MainActor (QuizAction) async throws -> Void
    var words: () -> Void = {}
    var type: (String) -> Void = { _ in }
    @State private var failure: String?
    @State private var busy = false
    @FocusState private var focused: Bool

    private var fold: QuizFold {
        draft.fold ?? (wide ? .full : .peek)
    }

    private var range: Bool {
        draft.range ?? (quiz.excerpt?.pick == "range")
    }

    var body: some View {
        Group {
            if fold == .bar {
                bar
            } else {
                card
            }
        }
        .overlay(alignment: .leading) {
            if wide && focused {
                PaneBar(label: "Question has keyboard focus", id: "quiz.focus")
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("quiz-card")
        .focusable(wide, interactions: .edit)
        .focused($focused)
        .focusEffectDisabled()
        .onKeyPress(phases: .down) { press in key(press) }
        .task(id: quiz.request) {
            guard wide && composerEmpty else { return }
            try? await Task.sleep(for: .milliseconds(300))
            focused = true
        }
    }

    private var bar: some View {
        Button {
            withAnimation(.snappy) { draft.fold = .peek }
        } label: {
            HStack(spacing: 12) {
                DockBadge(symbol: "exclamationmark")
                VStack(alignment: .leading, spacing: 2) {
                    Text([quiz.title, quiz.concept].compactMap { $0 }.joined(separator: " · "))
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Color.warningInk)
                        .lineLimit(1)
                    if let hint = quiz.confidence?.hint ?? quiz.hint {
                        Text(hint)
                            .font(.caption)
                            .foregroundStyle(Color.captionInk)
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 0)
                Image(systemName: "chevron.up")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.captionInk)
                    .frame(width: 44, height: 44)
            }
            .padding(.leading, 14)
            .frame(minHeight: 56)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.warning.opacity(0.10), in: .rect(cornerRadius: 28))
            .background(Color.surface1, in: .rect(cornerRadius: 28))
            .overlay(RoundedRectangle(cornerRadius: 28).strokeBorder(Color.warning.opacity(0.55), lineWidth: 1.5))
            .contentShape(.rect(cornerRadius: 28))
        }
        .buttonStyle(.plain)
        .keyboardShortcut("q", modifiers: .option)
        .gesture(DragGesture(minimumDistance: 12).onEnded { value in
            if value.translation.height < -24 { withAnimation(.snappy) { draft.fold = .peek } }
        })
        .accessibilityLabel(quiz.title + ", " + quiz.question)
    }

    private var card: some View {
        DockCard {
            VStack(alignment: .leading, spacing: 12) {
                grabber
                header
                question
                if let excerpt = quiz.excerpt, !excerpt.rows.isEmpty {
                    QuizExcerptView(excerpt: excerpt, labels: quiz.labels, wide: wide, rows: rows(excerpt), cap: cap, picked: excerpt.pickedRange, cursor: wide && focused ? draft.selection(excerpt) : nil, open: open) { n in
                        guard excerpt.pickable, !quiz.sending else { return }
                        draft.cursor = n
                        draft.mark = nil
                        run(.pick(excerpt.tap(n, range: range)))
                    }
                    if excerpt.pickable && !quiz.sending {
                        pickControls(excerpt)
                    }
                }
                if !quiz.options.isEmpty {
                    options
                }
                if let confidence = quiz.confidence {
                    capsules(confidence)
                }
                if quiz.sending {
                    sendingRow
                } else if fold == .full {
                    footer
                }
                if let note = blocked ?? failure {
                    Label(note, systemImage: blocked == nil ? "exclamationmark.triangle.fill" : "wifi.slash")
                        .font(.footnote)
                        .foregroundStyle(blocked == nil ? Color.dangerInk : Color.captionInk)
                        .lineLimit(2)
                }
            }
        }
        .animation(.snappy, value: quiz.state)
    }

    private var grabber: some View {
        Capsule()
            .fill(Color(uiColor: .tertiaryLabel))
            .frame(width: 36, height: 5)
            .frame(maxWidth: .infinity)
            .frame(height: 12)
            .contentShape(.rect)
            .gesture(DragGesture(minimumDistance: 8).onEnded { value in drag(value.translation.height) })
            .accessibilityHidden(true)
    }

    private func drag(_ height: CGFloat) {
        withAnimation(.snappy) {
            if height < -24 {
                draft.fold = .full
            } else if height > 24 {
                draft.fold = fold == .full ? .peek : .bar
            }
        }
    }

    private var header: some View {
        HStack(spacing: 10) {
            DockBadge(symbol: "exclamationmark")
            Text(quiz.title)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.warningInk)
                .lineLimit(1)
                .layoutPriority(1)
            if let detail = [quiz.step, wide ? quiz.hint : nil].compactMap({ $0 }).joined(separator: " · ").nilIfEmpty {
                Text(detail)
                    .font(.caption)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
            if wide {
                Text("⌥Q")
                    .font(.caption2.monospaced())
                    .foregroundStyle(Color.captionInk)
                    .padding(.horizontal, 5)
                    .padding(.vertical, 2)
                    .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(Color(uiColor: .separator)))
            }
            Button {
                withAnimation(.snappy) { draft.fold = .bar }
            } label: {
                Image(systemName: "chevron.down")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.captionInk)
                    .frame(width: 44, height: 36)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .keyboardShortcut("q", modifiers: .option)
            .accessibilityLabel(quiz.labels.fold)
        }
        .contentShape(.rect)
        .gesture(DragGesture(minimumDistance: 12).onEnded { value in drag(value.translation.height) })
    }

    private var question: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if let concept = quiz.concept {
                QuizChip(text: concept)
            }
            Text(quiz.question)
                .font(.body.weight(.semibold))
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func rows(_ excerpt: Quiz.Excerpt) -> [Quiz.Row] {
        fold == .peek ? excerpt.window(QuizMetrics.peekLines).map(Quiz.Row.line) : excerpt.rows
    }

    private var cap: CGFloat? {
        fold == .peek ? nil : QuizMetrics.row * CGFloat(wide ? QuizMetrics.padLines : QuizMetrics.phoneLines)
    }

    private var open: (() -> Void)? {
        quiz.excerpt?.open == nil ? nil : { run(.open) }
    }

    private func pickControls(_ excerpt: Quiz.Excerpt) -> some View {
        HStack(spacing: 6) {
            nudge(excerpt, -1, "chevron.up", "Previous line")
            nudge(excerpt, 1, "chevron.down", "Next line")
            if let hint = quiz.confidence?.hint {
                Text(hint)
                    .font(.subheadline.weight(.semibold))
                    .monospacedDigit()
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
                    .padding(.leading, 4)
                    .layoutPriority(1)
            }
            Spacer(minLength: 8)
            Picker(quiz.hint ?? "", selection: Binding(get: { range }, set: { draft.range = $0 })) {
                Image(systemName: "minus").accessibilityLabel(quiz.labels.line).tag(false)
                Image(systemName: "line.3.horizontal").accessibilityLabel(quiz.labels.range).tag(true)
            }
            .pickerStyle(.segmented)
            .fixedSize()
            .accessibilityIdentifier("quiz-pick-mode")
        }
    }

    private func nudge(_ excerpt: Quiz.Excerpt, _ delta: Int, _ symbol: String, _ label: String) -> some View {
        let next = excerpt.nudge(delta, range: range)
        return Button {
            if let next { run(.pick(next)) }
        } label: {
            Image(systemName: symbol)
                .font(.body.weight(.semibold))
                .frame(width: 44, height: 44)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .foregroundStyle(next == nil ? Color(uiColor: .tertiaryLabel) : Color.primary)
        .disabled(next == nil || busy)
        .accessibilityLabel(label)
    }

    private var options: some View {
        VStack(spacing: 8) {
            ForEach(quiz.options) { option in
                Button {
                    run(.option(option.value))
                } label: {
                    HStack(spacing: 10) {
                        Text(option.key)
                            .font(.footnote.monospaced().weight(.semibold))
                            .foregroundStyle(option.on ? Color.onAccent : Color.captionInk)
                            .frame(width: 24, height: 24)
                            .background(option.on ? Color.accentColor : Color.surface2, in: .circle)
                        Text(option.label)
                            .font(.body)
                            .multilineTextAlignment(.leading)
                            .fixedSize(horizontal: false, vertical: true)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 12)
                    .frame(minHeight: 46)
                    .background(option.on ? Color.selectedRow : Color.surface2, in: .rect(cornerRadius: 14))
                    .contentShape(.rect(cornerRadius: 14))
                }
                .buttonStyle(.plain)
                .disabled(quiz.sending || busy)
                .accessibilityAddTraits(option.on ? .isSelected : [])
            }
        }
    }

    private func capsules(_ confidence: Quiz.Confidence) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                ForEach(confidence.levels) { level in
                    let on = confidence.value == level.value
                    Button {
                        run(.rate(level.value))
                    } label: {
                        VStack(spacing: 2) {
                            Text(level.label.capitalizedFirst)
                                .font(.body.weight(.semibold))
                                .lineLimit(1)
                                .minimumScaleFactor(0.8)
                            if wide {
                                Text(level.key)
                                    .font(.caption2.monospaced())
                                    .foregroundStyle(on ? Color.onAccent : Color.captionInk)
                            }
                        }
                        .foregroundStyle(on ? Color.onAccent : Color.primary)
                        .frame(maxWidth: .infinity, minHeight: 50)
                        .background(on ? Color.accentColor : Color.surface2, in: .capsule)
                        .contentShape(.capsule)
                    }
                    .buttonStyle(.plain)
                    .disabled(!confidence.enabled || busy)
                    .opacity(confidence.enabled ? 1 : 0.42)
                    .accessibilityLabel(confidence.label + " " + level.label)
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
            if quiz.typed, let hint = confidence.hint {
                Text(hint)
                    .font(.caption)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(2)
            }
        }
    }

    private var sendingRow: some View {
        VStack(alignment: .leading, spacing: 10) {
            QuizHoldBar(ms: quiz.confidence?.holdMs ?? 2000)
                .id(quiz.confidence?.value ?? "")
            HStack {
                Text(sendingText)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
                Spacer(minLength: 8)
                Button(quiz.labels.undo) { run(.undo) }
                    .font(.body.weight(.semibold))
                    .buttonStyle(.glass)
                    .controlSize(.large)
                    .keyboardShortcut("z", modifiers: .command)
            }
        }
    }

    private var sendingText: String {
        let level = quiz.confidence?.level(quiz.confidence?.value)?.label
        return ([quiz.labels.sending] + [level].compactMap { $0 }).joined(separator: " · ")
    }

    @ViewBuilder private var footer: some View {
        if wide {
            HStack(spacing: 6) {
                if quiz.excerpt?.pickable == true {
                    QuizKeyHint(text: "↑↓ line")
                    QuizKeyHint(text: "⇧↑↓ range")
                }
                if quiz.confidence?.sends == true {
                    QuizKeyHint(text: "1–3 rate and send")
                } else {
                    QuizKeyHint(text: "1–3 rate · ⌘↩ sends")
                }
                Spacer(minLength: 8)
                Button(quiz.labels.skip) { run(.skip) }
                    .font(.body.weight(.semibold))
                    .buttonStyle(.glass)
            }
        } else {
            HStack(spacing: 10) {
                Button { run(.skip) } label: {
                    Text(quiz.labels.skip).frame(maxWidth: .infinity, minHeight: 44)
                }
                .background(Color.surface2, in: .capsule)
                Button(action: words) {
                    Text(quiz.labels.sayIt).lineLimit(1).frame(maxWidth: .infinity, minHeight: 44)
                }
                .background(Color.surface2, in: .capsule)
            }
            .font(.body.weight(.semibold))
            .buttonStyle(.plain)
            .disabled(busy)
        }
    }

    private func run(_ action: QuizAction) {
        guard !busy || action == .undo else { return }
        busy = true
        failure = nil
        Task {
            do {
                try await act(action)
            } catch {
                failure = error.localizedDescription
            }
            busy = false
        }
    }

    private static let navigation: Set<KeyEquivalent> = [.upArrow, .downArrow, .leftArrow, .rightArrow, .pageUp, .pageDown, .home, .end, .escape, .tab, .delete, .deleteForward, .clear, .return]

    private func pickSelection() {
        guard let excerpt = quiz.excerpt, excerpt.pickable, !quiz.sending, let selection = draft.selection(excerpt) else { return }
        draft.mark = nil
        run(.pick(selection))
    }

    private func key(_ press: KeyPress) -> KeyPress.Result {
        guard wide else { return .ignored }
        let mods = press.modifiers
        if let excerpt = quiz.excerpt, excerpt.pickable, !quiz.sending, mods.isDisjoint(with: [.command, .option, .control]) {
            if press.key == .upArrow || press.key == .downArrow {
                draft.move(press.key == .upArrow ? -1 : 1, extend: mods.contains(.shift), in: excerpt)
                return .handled
            }
            if press.key == .return {
                pickSelection()
                return .handled
            }
        }
        guard mods.isDisjoint(with: [.command, .option, .control]) else { return .ignored }
        let typed = press.characters.lowercased()
        if let level = quiz.confidence?.levels.first(where: { $0.key == typed }) {
            if quiz.confidence?.enabled == true { run(.rate(level.value)) }
            return .handled
        }
        if let option = quiz.options.first(where: { $0.key == typed }), !quiz.sending {
            run(.option(option.value))
            return .handled
        }
        guard !Self.navigation.contains(press.key), !press.characters.isEmpty,
              press.characters.unicodeScalars.allSatisfy({ !CharacterSet.controlCharacters.contains($0) && !(0xF700...0xF8FF).contains($0.value) }) else { return .ignored }
        type(press.characters)
        return .handled
    }
}

struct QuizExcerptView: View {
    let excerpt: Quiz.Excerpt
    let labels: Quiz.Labels
    let wide: Bool
    let rows: [Quiz.Row]
    let cap: CGFloat?
    var picked: ClosedRange<Int>?
    var cursor: ClosedRange<Int>?
    var record = false
    let open: (() -> Void)?
    var tap: ((Int) -> Void)?
    @State private var height: CGFloat = 0

    var body: some View {
        VStack(spacing: 0) {
            head
            Divider()
            if let cap {
                ScrollViewReader { proxy in
                    ScrollView {
                        lines
                            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
                    }
                    .frame(height: min(max(height, QuizMetrics.row), cap))
                    .scrollBounceBehavior(.basedOnSize)
                    .onAppear { scroll(proxy) }
                    .onChange(of: cursor) { scroll(proxy) }
                }
            } else {
                lines
            }
        }
        .background(Color(uiColor: .systemBackground), in: .rect(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Color(uiColor: .separator).opacity(0.6)))
        .clipShape(.rect(cornerRadius: 16))
    }

    private func scroll(_ proxy: ScrollViewProxy) {
        guard let n = cursor?.upperBound ?? picked?.lowerBound else { return }
        proxy.scrollTo("line-\(n)", anchor: .center)
    }

    private var head: some View {
        HStack(spacing: 6) {
            Image(systemName: "doc")
                .font(.caption)
                .foregroundStyle(Color.captionInk)
            Text(wide ? excerpt.file : excerpt.fileName)
                .font(.footnote.weight(.semibold))
                .lineLimit(1)
                .truncationMode(.head)
                .layoutPriority(1)
            if !excerpt.commit.isEmpty {
                Text("@ " + excerpt.commit)
                    .font(.caption.monospaced())
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
            }
            Spacer(minLength: 6)
            if let open {
                Button(labels.openOnMac, action: open)
                    .font(.footnote.weight(.semibold))
                    .buttonStyle(.glass)
                    .keyboardShortcut("g", modifiers: .option)
            }
        }
        .padding(.leading, 12)
        .padding(.trailing, 6)
        .frame(minHeight: 40)
    }

    private var lines: some View {
        VStack(spacing: 0) {
            ForEach(rows) { row in
                switch row {
                case .line(let line):
                    lineRow(line)
                        .id(row.id)
                case .fold(let label):
                    Text(label)
                        .font(.caption)
                        .foregroundStyle(Color.captionInk)
                        .frame(maxWidth: .infinity, minHeight: 28, alignment: .leading)
                        .padding(.leading, 40)
                        .background(Color.surface2.opacity(0.5))
                        .id(row.id)
                }
            }
        }
    }

    private func lineRow(_ line: Quiz.Line) -> some View {
        let on = picked?.contains(line.n) == true
        let mark: String? = switch line.tone { case "success": "checkmark"; case "danger": "xmark"; default: nil }
        let ink: Color = line.tone == "success" ? .successInk : line.tone == "danger" ? .dangerInk : .primary
        let row = HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: mark ?? "checkmark")
                .font(.caption2.weight(.bold))
                .foregroundStyle(ink)
                .opacity(mark == nil ? 0 : 1)
                .frame(width: 10)
            Text("\(line.n)")
                .font(.caption.monospaced())
                .foregroundStyle(Color(uiColor: .tertiaryLabel))
                .frame(width: 22, alignment: .trailing)
            Text(excerpt.text(line, dropIndent: true).isEmpty ? " " : excerpt.text(line, dropIndent: true))
                .font(.system(size: 13, design: .monospaced))
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let label = line.label {
                Text(label)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(ink)
                    .lineLimit(1)
            }
        }
        .padding(.leading, 6)
        .padding(.trailing, 10)
        .padding(.vertical, 7)
        .frame(minHeight: QuizMetrics.row)
        .background(background(line, on: on))
        .overlay(alignment: .leading) {
            if on && !record { Rectangle().fill(Color.accentColor).frame(width: 3) }
        }
        .overlay {
            if cursor?.contains(line.n) == true { Rectangle().strokeBorder(Color.accentColor.opacity(0.7), lineWidth: 1.5) }
        }
        .contentShape(.rect)
        return Group {
            if let tap {
                Button { tap(line.n) } label: { row }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Line \(line.n): \(line.text)")
                    .accessibilityAddTraits(on ? .isSelected : [])
            } else {
                row
            }
        }
    }

    private func background(_ line: Quiz.Line, on: Bool) -> Color {
        switch line.tone {
        case "success": Color.success.opacity(0.18)
        case "danger": Color.danger.opacity(0.16)
        default: on && !record ? Color.selectedRow : .clear
        }
    }
}

struct QuizHoldBar: View {
    let ms: Int
    @State private var progress: CGFloat = 0

    var body: some View {
        GeometryReader { geometry in
            ZStack(alignment: .leading) {
                Capsule().fill(Color.surface2)
                Capsule().fill(Color.accentColor).frame(width: geometry.size.width * progress)
            }
        }
        .frame(height: 4)
        .onAppear {
            progress = 0
            withAnimation(.linear(duration: Double(ms) / 1000)) { progress = 1 }
        }
        .accessibilityHidden(true)
    }
}

struct QuizChip: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.caption.weight(.medium))
            .foregroundStyle(Color.captionInk)
            .lineLimit(1)
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(Color.surface2, in: .rect(cornerRadius: 6))
    }
}

struct QuizKeyHint: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.caption2.monospaced())
            .foregroundStyle(Color.captionInk)
            .lineLimit(1)
            .padding(.horizontal, 5)
            .padding(.vertical, 2)
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(Color(uiColor: .separator)))
    }
}

struct QuizRecord: View {
    let quiz: Quiz
    var act: ((QuizAction) -> Void)?
    @Environment(\.horizontalSizeClass) private var sizeClass

    private var wide: Bool { sizeClass == .regular }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                verdict
                if let detail = detail {
                    Text(detail)
                        .font(.caption)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
                if quiz.reveal?.sureWrong == true, let confidence = quiz.reveal?.confidence {
                    Text(confidence)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(Color.dangerInk)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Color.danger.opacity(0.14), in: .capsule)
                }
            }
            Text(quiz.question)
                .font(.subheadline.weight(.semibold))
                .fixedSize(horizontal: false, vertical: true)
            if let excerpt = quiz.excerpt, !excerpt.rows.isEmpty {
                QuizExcerptView(excerpt: excerpt, labels: quiz.labels, wide: wide, rows: excerpt.rows, cap: nil, record: true, open: nil)
            }
            if let reveal = quiz.reveal {
                if let why = reveal.why, !why.isEmpty {
                    Text(Self.inline(why))
                        .font(.subheadline)
                        .lineLimit(2)
                        .environment(\.openURL, OpenURLAction { url in MarkdownLinkPolicy.allows(url) ? .systemAction : .discarded })
                        .padding(.leading, 10)
                        .overlay(alignment: .leading) { Rectangle().fill(Color(uiColor: .separator)).frame(width: 2) }
                }
                if let note = reveal.note, !note.isEmpty {
                    Text(note)
                        .font(.footnote)
                        .foregroundStyle(Color.dangerInk)
                }
                HStack(spacing: 6) {
                    if let ref = wide ? reveal.ref : reveal.shortRef {
                        Text(ref)
                            .font(.footnote.monospaced())
                            .foregroundStyle(Color.accentColor)
                            .underline()
                            .lineLimit(1)
                            .truncationMode(.head)
                    }
                    if wide, let commit = quiz.excerpt?.commit, !commit.isEmpty {
                        Text("@ " + commit)
                            .font(.caption.monospaced())
                            .foregroundStyle(Color.captionInk)
                    }
                    Spacer(minLength: 6)
                    if let act, quiz.excerpt?.open != nil {
                        Button(quiz.labels.openOnMac) { act(.open) }
                            .font(.footnote.weight(.semibold))
                            .buttonStyle(.glass)
                    }
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.surface1, in: .rect(cornerRadius: 20))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("quiz-record")
    }

    private var detail: String? {
        let reveal = quiz.reveal
        let parts: [String?] = wide
            ? [reveal.map { quiz.labels.you + " " + $0.yours }, reveal?.sureWrong == true ? nil : reveal?.confidence, reveal?.answer.map { quiz.labels.answer + " " + $0 }]
            : [quiz.concept, reveal?.sureWrong == true ? nil : reveal?.confidence]
        return parts.compactMap { $0?.nilIfEmpty }.joined(separator: " · ").nilIfEmpty
    }

    @ViewBuilder private var verdict: some View {
        let reveal = quiz.reveal
        let tone: Color = switch reveal?.verdict {
        case "correct": .successInk
        case "close": .warningInk
        case "wrong": .dangerInk
        default: .captionInk
        }
        HStack(spacing: 4) {
            switch reveal?.verdict {
            case "correct": Image(systemName: "checkmark")
            case "close": Image(systemName: "circle.lefthalf.filled")
            case "wrong": Image(systemName: "xmark")
            case "pending": ProgressView().controlSize(.mini)
            default: EmptyView()
            }
            Text(reveal?.label ?? quiz.title)
        }
        .font(.caption.weight(.semibold))
        .foregroundStyle(tone)
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(tone.opacity(0.14), in: .capsule)
    }

    static func inline(_ text: String) -> AttributedString {
        (try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace))) ?? AttributedString(text)
    }
}

struct QuizRating: View {
    let label: String
    let levels: [Quiz.Level]
    let value: String?
    let nudge: Int
    let rate: (String) -> Void
    @State private var flash = false

    var body: some View {
        Picker(label, selection: Binding(get: { value ?? "" }, set: { rate($0) })) {
            ForEach(levels) { level in
                Text(level.label.capitalizedFirst).tag(level.value)
            }
        }
        .pickerStyle(.segmented)
        .padding(2)
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Color.warning, lineWidth: flash ? 2 : 0))
        .scaleEffect(flash ? 1.03 : 1)
        .onChange(of: nudge) {
            AccessibilityNotification.Announcement(label).post()
            withAnimation(.spring(duration: 0.3)) { flash = true }
            Task {
                try? await Task.sleep(for: .seconds(1.2))
                withAnimation(.easeOut(duration: 0.3)) { flash = false }
            }
        }
        .padding(.horizontal, 12)
        .frame(maxWidth: 820)
        .accessibilityIdentifier("quiz-rating")
    }
}

struct QuizNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var draft = QuizDraft()
    @Environment(\.horizontalSizeClass) private var sizeClass

    var body: some View {
        if let quiz = Quiz(props: node.props) {
            if quiz.dock {
                QuizCard(quiz: quiz, draft: $draft, wide: sizeClass == .regular) { action in
                    fire(quiz, action)
                }
            } else {
                QuizRecord(quiz: quiz) { fire(quiz, $0) }
            }
        }
    }

    private func fire(_ quiz: Quiz, _ action: QuizAction) {
        guard let event = action.event else { return }
        context.run { await $0.fire(node, .named(event, .object(action.fields(request: quiz.request)))) }
    }
}

private extension String {
    var capitalizedFirst: String {
        prefix(1).uppercased() + dropFirst()
    }
}
