import SwiftUI

import CompanionKit

struct OpenCountChip: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .foregroundStyle(Color.warningInk)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 8)
            .frame(minHeight: 20)
            .overlay(Capsule().strokeBorder(Color.warning.opacity(0.55), lineWidth: 1.5))
    }
}

struct OpenHeaderChip: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .foregroundStyle(Color.captionInk)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 9)
            .frame(minHeight: 22)
            .background(Color.surface2, in: .capsule)
    }
}

struct OpenNumber: View {
    let number: Int
    let done: Bool

    var body: some View {
        Text("\(number)")
            .font(.caption.weight(.semibold))
            .foregroundStyle(done ? Color.successInk : Color.captionInk)
            .frame(width: 22, height: 22)
            .background(done ? Color.success.opacity(0.14) : .clear, in: .circle)
            .overlay(Circle().strokeBorder(done ? Color.success.opacity(0.55) : Color(.separator), lineWidth: 1.5))
    }
}

struct OpenClosedRow: View {
    let items: OpenItems

    var body: some View {
        Label(items.summary, systemImage: "checkmark")
            .font(.footnote)
            .foregroundStyle(Color.captionInk)
            .lineLimit(1)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct OpenChoices: View {
    let item: OpenItem
    @Binding var draft: OpenDraft
    var focused: FocusState<String?>.Binding

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(item.options) { option in
                let selected = draft.picks[item.id, default: []].contains(option.label)
                Button {
                    draft.pick(option, in: item)
                } label: {
                    ChoiceRow(title: option.label, detail: option.description, symbol: ChoiceRow.symbol(multi: item.multiSelect, selected: selected), selected: selected)
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
            let open = draft.opened.contains(item.id)
            Button {
                draft.toggleOther(item)
                focused.wrappedValue = open ? nil : item.id
            } label: {
                ChoiceRow(title: "Other…", symbol: ChoiceRow.symbol(multi: item.multiSelect, selected: open), selected: open, muted: true)
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(open ? .isSelected : [])
            if open {
                TextField("Your answer", text: Binding(get: { draft.others[item.id] ?? "" }, set: { draft.others[item.id] = $0 }), axis: .vertical)
                    .lineLimit(1...4)
                    .focused(focused, equals: item.id)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Color.surface2, in: .rect(cornerRadius: 14))
            }
        }
    }
}

struct OpenOfferChoice: View {
    let item: OpenItem
    @Binding var draft: OpenDraft
    var title: String?
    var detail: String?

    var body: some View {
        let selected = draft.offers.contains(item.id)
        Button {
            draft.toggleOffer(item)
        } label: {
            ChoiceRow(title: title ?? item.label, detail: detail ?? item.description, symbol: ChoiceRow.symbol(multi: true, selected: selected), selected: selected)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

struct OpenClosingLine: View {
    let count: Int
    let agentName: String
    let undo: () -> Void

    var body: some View {
        DockCard(neutral: true) {
            HStack(spacing: 12) {
                SessionGlyphView(glyph: .open)
                VStack(alignment: .leading, spacing: 2) {
                    Text(count == 1 ? "Closed 1 item" : "Closed \(count) items")
                        .font(.subheadline.weight(.semibold))
                        .lineLimit(1)
                    Text("\(agentName) hears “none of these” next turn")
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(2)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Button("Undo", action: undo)
                    .font(.subheadline.weight(.semibold))
                    .frame(minHeight: 44)
                    .contentShape(.rect)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 6)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("open-closing")
    }
}

struct OpenItemsCard: View {
    let card: OpenCard
    let count: Int
    let folded: Bool
    let blocked: String?
    @Binding var draft: OpenDraft
    let later: @MainActor () async throws -> Void
    let send: @MainActor () async throws -> Void
    let no: () -> Void
    @State private var sending = false
    @State private var failure: String?
    @State private var contentHeight: CGFloat = 0
    @FocusState private var focused: String?

    var body: some View {
        DockCard(neutral: true) {
            if folded && !draft.unfolded {
                foldedLine
            } else {
                full
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("open-card")
    }

    private var countLabel: String {
        "\(count) open"
    }

    private var foldedLine: some View {
        Button {
            Motion.disclose { draft.unfolded = true }
        } label: {
            HStack(spacing: 10) {
                SessionGlyphView(glyph: .open)
                Text("Open after this turn")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                Spacer(minLength: 0)
                OpenCountChip(text: countLabel)
                RowChevron(always: true)
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 50)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }

    private var full: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                SessionGlyphView(glyph: .open)
                Text("Open after this turn")
                    .font(.subheadline.weight(.semibold))
                    .lineLimit(1)
                Spacer(minLength: 0)
                OpenCountChip(text: countLabel)
            }
            .padding(.horizontal, 16)
            .padding(.top, 14)
            .padding(.bottom, 10)
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(card.sections.enumerated()), id: \.element.id) { index, section in
                        if index > 0 { Divider() }
                        if section.id == current.id {
                            expanded(section)
                        } else {
                            foldedRow(section)
                        }
                    }
                    if let overflow = card.overflowLine {
                        Divider()
                        Text(overflow)
                            .font(.footnote)
                            .foregroundStyle(Color.captionInk)
                            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                            .padding(.horizontal, 16)
                    }
                }
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
                }
                .frame(maxHeight: contentHeight)
                .scrollBounceBehavior(.basedOnSize)
                .task(id: focused) {
                    guard let focused else { return }
                    try? await Task.sleep(for: .milliseconds(350))
                    withAnimation { proxy.scrollTo(Self.otherAnchor(focused), anchor: .bottom) }
                }
            }
            Divider()
            actions
                .padding(.horizontal, 16)
                .padding(.top, 12)
                .padding(.bottom, 16)
        }
    }

    private static func otherAnchor(_ item: String) -> String {
        "other/" + item
    }

    private var current: OpenSection {
        card.sections.first { $0.id == draft.section } ?? card.sections[0]
    }

    private func expanded(_ section: OpenSection) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                OpenNumber(number: section.number, done: draft.summary(section) != nil)
                OpenHeaderChip(text: section.chip)
            }
            Text(Markdown.inline(section.title))
                .font(.title3.weight(.semibold))
                .fixedSize(horizontal: false, vertical: true)
            switch section {
            case .decision(let item):
                OpenChoices(item: item, draft: $draft, focused: $focused)
                Color.clear
                    .frame(height: 0)
                    .id(Self.otherAnchor(item.id))
            case .offers(let items):
                ForEach(items) { item in
                    OpenOfferChoice(item: item, draft: $draft)
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 4)
        .padding(.bottom, 14)
    }

    private func foldedRow(_ section: OpenSection) -> some View {
        Button {
            Motion.disclose { draft.section = section.id }
        } label: {
            HStack(spacing: 10) {
                OpenNumber(number: section.number, done: draft.summary(section) != nil)
                OpenHeaderChip(text: section.chip)
                Text(draft.summary(section) ?? section.folded)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                RowChevron(always: true)
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 50)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }

    private var answerable: Bool {
        blocked == nil && !sending && !draft.answered(card.items).isEmpty
    }

    private var actions: some View {
        VStack(alignment: .leading, spacing: 10) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 10) {
                    noButton
                        .frame(maxWidth: 96)
                    laterButton
                        .frame(maxWidth: 96)
                    sendButton
                }
                VStack(spacing: 10) {
                    HStack(spacing: 10) {
                        noButton
                        laterButton
                    }
                    sendButton
                }
            }
            if let blocked {
                Text(blocked)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(2)
            }
            if let failure {
                Label(failure, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(Color.danger)
            }
        }
    }

    private var noButton: some View {
        PlainCapsule(title: "No to All", tint: Color.danger, action: no)
            .answering(blocked == nil && !sending, enabled: blocked == nil)
    }

    private var laterButton: some View {
        PlainCapsule(title: "Later", tint: .primary) { run(later) }
            .answering(blocked == nil && !sending, enabled: blocked == nil)
    }

    private var sendButton: some View {
        PrimaryCapsule(title: "Send answers") { run(send) }
            .answering(answerable, enabled: answerable)
    }

    private func run(_ work: @escaping @MainActor () async throws -> Void) {
        sending = true
        failure = nil
        Task {
            do {
                try await work()
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
            }
            sending = false
        }
    }
}

struct OpenQueueView: View {
    let context: PluginContext
    let store: AgentsStore
    @Environment(AppNavigation.self) private var navigation
    @Environment(VoiceSession.self) private var voice
    @State private var sending = false
    @State private var failure: String?
    @State private var turned = Date.distantPast
    @FocusState private var focused: String?

    var body: some View {
        let pass = store.openPass
        Group {
            if let pass, !pass.finished, let step = pass.current {
                screen(pass, step)
            } else if store.sessions.contains(where: { $0.openCount > 0 }) || !store.loaded {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Color(.systemGroupedBackground))
            } else {
                ContentUnavailableView("Nothing Open", systemImage: "checkmark.circle", description: Text("Every decision and offer has been answered."))
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Color(.systemGroupedBackground))
            }
        }
        .navigationTitle("Open")
        .toolbarTitleDisplayMode(.inline)
        .toolbar {
            if let pass {
                ToolbarItem(placement: .principal) {
                    VStack(spacing: 1) {
                        Text("Open").font(.headline)
                        Text(subtitle(pass))
                            .font(.footnote)
                            .foregroundStyle(Color.captionInk)
                            .lineLimit(1)
                    }
                    .accessibilityElement(children: .combine)
                }
                ToolbarItem(placement: .primaryAction) {
                    Menu("More", systemImage: "ellipsis") {
                        Button("Send Now", systemImage: "paperplane") { finish(leave: false) }
                            .disabled(answered(pass) == 0 || context.connection.blocked != nil)
                        if pass.canGoBack {
                            Button("Previous Item", systemImage: "chevron.left") { store.moveOpenPass { $0.previous() } }
                        }
                        if let step = pass.current {
                            Button("No to This Item", systemImage: "xmark.circle", role: .destructive) { no(step) }
                                .disabled(context.connection.blocked != nil)
                        }
                    }
                }
            }
        }
        .alert("Something Went Wrong", isPresented: Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })) {
            Button("OK") { failure = nil }
        } message: {
            Text(failure ?? "")
        }
        .onChange(of: store.openQueue, initial: true) { store.beginOpenPass() }
        .task { await store.refresh() }
    }

    private func subtitle(_ pass: OpenPass) -> String {
        let (items, sessions) = (pass.steps.count, pass.sessions.count)
        return "\(items) \(items == 1 ? "item" : "items") · \(sessions) \(sessions == 1 ? "session" : "sessions")"
    }

    private func answered(_ pass: OpenPass) -> Int {
        pass.steps.count { store.openDraft($0.session).answer($0.item) != nil }
    }

    private func draft(_ session: String) -> Binding<OpenDraft> {
        Binding(get: { store.openDraft(session) }, set: { store.setOpenDraft($0, session: session) })
    }

    private func screen(_ pass: OpenPass, _ step: OpenStep) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                progress(pass)
                card(step)
            }
            .padding(.horizontal)
            .padding(.vertical, 12)
            .frame(maxWidth: 820)
            .frame(maxWidth: .infinity)
        }
        .id(step.id)
        .contentMargins(.bottom, navigation.floatingMic(context.model, listening: voice.active) ? VoiceButton.floating + 8 : 0, for: .scrollContent)
        .scrollDismissesKeyboard(.interactively)
        .background(Color(.systemGroupedBackground))
        .safeAreaInset(edge: .bottom) {
            bar(pass, step)
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
                .frame(maxWidth: 820)
                .frame(maxWidth: .infinity)
                .padding(.top, 8)
                .background(Color(.systemGroupedBackground))
                .voiceDock()
        }
    }

    private func progress(_ pass: OpenPass) -> some View {
        let label = Text("\(pass.position + 1) of \(pass.steps.count)")
            .font(.footnote)
            .foregroundStyle(Color.captionInk)
            .fixedSize()
        let count = answered(pass)
        return HStack(spacing: 10) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 10) {
                    HStack(spacing: 6) {
                        ForEach(pass.steps.indices, id: \.self) { index in
                            Capsule()
                                .fill(index == pass.position ? Color.primary : Color(.tertiaryLabel))
                                .frame(width: index == pass.position ? 18 : 7, height: 7)
                        }
                    }
                    label
                }
                label
            }
            Spacer(minLength: 0)
            if count > 0 {
                Text("\(count) answered")
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .fixedSize()
            }
        }
    }

    private func card(_ step: OpenStep) -> some View {
        let item = step.item
        return TimelineView(.periodic(from: .now, by: 30)) { timeline in
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 8) {
                    OpenNumber(number: item.number, done: store.openDraft(step.session).answer(item) != nil)
                    OpenHeaderChip(text: item.kind == .decision ? item.header : "Next")
                    Text([step.title, item.at.map { ShortAge.since($0, now: timeline.date) }].compactMap { $0 }.joined(separator: " · "))
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
                Text(Markdown.inline(item.kind == .decision ? item.question : item.label))
                    .font(.title3.weight(.semibold))
                    .fixedSize(horizontal: false, vertical: true)
                if !item.quote.isEmpty {
                    HStack(spacing: 10) {
                        Rectangle()
                            .fill(Color(.separator))
                            .frame(width: 2)
                        Text(Markdown.inline(item.quote))
                            .font(.footnote)
                            .foregroundStyle(Color.captionInk)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .fixedSize(horizontal: false, vertical: true)
                }
                if item.kind == .decision {
                    OpenChoices(item: item, draft: draft(step.session), focused: $focused)
                } else {
                    OpenOfferChoice(item: item, draft: draft(step.session), title: "Do this", detail: item.description)
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.surface1, in: .rect(cornerRadius: 20))
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("open-step")
    }

    private func bar(_ pass: OpenPass, _ step: OpenStep) -> some View {
        let enabled = context.connection.blocked == nil && !sending
        return VStack(spacing: 8) {
            HStack(spacing: 8) {
                PlainCapsule(title: "Later", tint: .primary) { skip(pass, step) }
                PlainCapsule(title: "Backlog", tint: .primary) { backlog(pass, step) }
                    .answering(enabled, enabled: enabled)
                PrimaryCapsule(title: !pass.isLast ? "Next" : answered(pass) > 0 ? "Send answers" : "Done") { advance(pass) }
                    .answering(!sending, enabled: !sending)
            }
            Text(context.connection.blocked ?? "Answers go to each session when you finish · Send now in •••")
                .font(.footnote)
                .foregroundStyle(Color.captionInk)
                .multilineTextAlignment(.center)
                .lineLimit(2)
        }
    }

    private func guarded(_ work: () -> Void) {
        guard Date.now.timeIntervalSince(turned) >= 0.6 else { return }
        turned = .now
        work()
    }

    private func advance(_ pass: OpenPass) {
        guarded { move(pass) }
    }

    private func move(_ pass: OpenPass) {
        if pass.isLast {
            finish(leave: true)
        } else {
            store.moveOpenPass { $0.next() }
        }
    }

    private func skip(_ pass: OpenPass, _ step: OpenStep) {
        guarded {
            var cleared = store.openDraft(step.session)
            cleared.clear([step.item])
            store.setOpenDraft(cleared, session: step.session)
            move(pass)
        }
    }

    private func backlog(_ pass: OpenPass, _ step: OpenStep) {
        guarded { close(step) { try await store.backlogOpen(step.session, items: [step.item]) } }
    }

    private func no(_ step: OpenStep) {
        close(step) { try await store.noOpen(step.session, items: [step.item]) }
    }

    private func close(_ step: OpenStep, _ work: @escaping @MainActor () async throws -> Void) {
        sending = true
        store.moveOpenPass { $0.file(step) }
        Task {
            do {
                try await work()
                store.moveOpenPass { if $0.current?.id == step.id { $0.next() } }
                if store.openPass?.atEnd != false { finish(leave: true) }
            } catch {
                store.moveOpenPass { $0.unfile(step) }
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
            }
            sending = false
        }
    }

    private func finish(leave leaving: Bool) {
        sending = true
        Task {
            do {
                try await store.sendOpenPass()
                if leaving { leave() }
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
            }
            sending = false
        }
    }

    private func leave() {
        store.endOpenPass()
        navigation.detail = nil
        navigation.preferredColumn = .content
    }
}
