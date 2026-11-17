import SwiftUI

import CompanionKit

enum SessionRole: Equatable {
    case plain
    case asideSheet
    case asideFull
}

struct AsideEntry {
    let ask: @MainActor (AsideAnchor) -> Void
    let mark: @MainActor (AsideAnchor) -> AsideState?
    let message: @MainActor (String) -> String?
}

extension EnvironmentValues {
    @Entry var asideEntry: AsideEntry?
}

extension View {
    func asideMenu(_ anchor: AsideAnchor) -> some View {
        modifier(AsideOptionMenu(anchor: anchor))
    }

    func messageAsideMenu(_ row: String) -> some View {
        modifier(MessageAsideMenu(row: row))
    }
}

private struct AsideOptionMenu: ViewModifier {
    let anchor: AsideAnchor
    @Environment(\.asideEntry) private var entry

    func body(content: Content) -> some View {
        if let entry {
            content.contextMenu {
                Button(entry.mark(anchor) == nil ? "Ask aside" : "Open aside", systemImage: AsideGlyph.symbol) { entry.ask(anchor) }
            }
        } else {
            content
        }
    }
}

private struct MessageAsideMenu: ViewModifier {
    let row: String
    @Environment(\.asideEntry) private var entry

    func body(content: Content) -> some View {
        if let entry, let text = entry.message(row) {
            content.contextMenu {
                Button("Ask aside", systemImage: AsideGlyph.symbol) {
                    entry.ask(AsideAnchor(message: String(row.split(separator: "/", maxSplits: 1).last ?? Substring(row)), quote: text))
                }
                Button("Copy", systemImage: "doc.on.doc") { UIPasteboard.general.string = text }
                ShareLink(item: text) { Label("Share…", systemImage: "square.and.arrow.up") }
            }
        } else {
            content
        }
    }
}

struct AsideGlyph: View {
    static let symbol = "bubble.left.and.bubble.right"
    let state: AsideState

    var body: some View {
        Image(systemName: Self.symbol)
            .font(.caption)
            .foregroundStyle(ink)
            .accessibilityLabel(state == .asks ? "aside asks you" : "aside")
    }

    private var ink: Color {
        switch state {
        case .thinking: .accentColor
        case .asks: Color.warningInk
        default: Color.captionInk
        }
    }
}

struct ReadOnlyPill: View {
    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: "eye")
            Text("read-only")
        }
            .font(.caption)
            .foregroundStyle(Color.captionInk)
            .lineLimit(1)
            .padding(.horizontal, 8)
            .frame(height: 20)
            .background(Color.surface2, in: .capsule)
    }
}

struct AsideHeader: View {
    let session: AgentSession
    let place: String

    var body: some View {
        VStack(spacing: 1) {
            HStack(spacing: 6) {
                Image(systemName: AsideGlyph.symbol)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                Text(session.displayTitle)
                    .font(.headline)
                    .lineLimit(1)
                    .truncationMode(.tail)
                ReadOnlyPill()
                    .fixedSize()
            }
            Text(place)
                .font(.caption)
                .foregroundStyle(Color.captionInk)
                .lineLimit(1)
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("aside.header")
    }
}

struct ForkedFromCard: View {
    let anchor: AsideAnchor
    let panel: AsidePanel?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label(anchor.message == nil ? "Forked from the question · read everything the session knew" : "Forked from a message · read everything the session knew", systemImage: AsideGlyph.symbol)
                .font(.caption)
                .foregroundStyle(Color.captionInk)
            if let quote = anchor.quote, anchor.message != nil {
                Text(quote)
                    .font(.footnote)
                    .textSelection(.enabled)
                    .padding(.leading, 10)
                    .overlay(alignment: .leading) { Capsule().fill(.quaternary).frame(width: 3) }
            } else {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    if let header = panel?.header, !header.isEmpty {
                        Text(header)
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(Color.captionInk)
                            .padding(.horizontal, 8)
                            .frame(height: 20)
                            .background(Color.surface2, in: .capsule)
                    }
                    Text(panel?.text.nilIfEmpty ?? anchor.question ?? "")
                        .font(.subheadline)
                }
                if let option = anchor.option {
                    Text(optionLine(option))
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Color(.separator), style: StrokeStyle(lineWidth: 1.5, dash: [5, 4])))
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("aside.forked")
    }

    private func optionLine(_ option: String) -> AttributedString {
        var line = AttributedString((panel?.option ?? 0) > 0 ? "Option \(panel?.option ?? 0) · " : "Option · ")
        var label = AttributedString(panel?.label.nilIfEmpty ?? option)
        label.font = .footnote.weight(.semibold)
        label.foregroundColor = .primary
        line += label
        if let description = panel?.description, !description.isEmpty { line += AttributedString(" — " + description) }
        return line
    }
}

struct AsideActions: View {
    let blocked: String?
    let sendBack: () -> Void
    let keep: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            PrimaryCapsule(title: "Send back…", action: sendBack)
                .accessibilityIdentifier("aside.sendBack")
            PlainCapsule(title: "Keep", tint: .primary, action: keep)
                .frame(width: 110)
                .accessibilityIdentifier("aside.keep")
        }
        .disabled(blocked != nil)
        .padding(.horizontal, 12)
        .frame(maxWidth: 820)
    }
}

struct AsideStrip: View {
    let pinned: PinnedQuestion
    let panel: AsidePanel?
    let anchorOption: String?
    let blocked: String?
    let pick: (String) -> Void
    let other: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            (Text("The parent asks").foregroundStyle(Color.warningInk) + Text(" · " + (pinned.text.nilIfEmpty ?? pinned.title)))
                .font(.footnote)
                .lineLimit(2)
            FlowLayout(spacing: 8) {
                    ForEach(pinned.options) { option in
                        chip(option.label, on: option.on, marked: option.value == anchorOption) { pick(option.value) }
                            .accessibilityIdentifier("aside.strip." + option.value)
                    }
                    if pinned.other {
                        chip("Other…", on: pinned.otherOn, marked: false, action: other)
                            .accessibilityIdentifier("aside.strip.other")
                    }
            }
            .disabled(blocked != nil)
            Text("Pick here to answer question \(max(panel?.question ?? 1, 1)); Next / Send stay in the parent's card.")
                .font(.caption)
                .foregroundStyle(Color.captionInk)
        }
        .padding(12)
        .background(Color.warning.opacity(0.10), in: .rect(cornerRadius: 16))
        .padding(.horizontal)
        .padding(.vertical, 6)
        .frame(maxWidth: 820)
        .frame(maxWidth: .infinity)
        .background(Color(.systemGroupedBackground))
    }

    private func chip(_ title: String, on: Bool, marked: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Text(title).lineLimit(1)
                if marked { Image(systemName: AsideGlyph.symbol).font(.caption2) }
            }
            .font(.subheadline.weight(.semibold))
            .padding(.horizontal, 14)
            .frame(minHeight: 44)
            .background(Color.surface2, in: .capsule)
            .overlay(Capsule().strokeBorder(on ? Color.accentColor : .clear, lineWidth: 1.5))
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}

struct AsidesPill: View {
    let count: Int
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            Text("\(count) asides")
                .font(.footnote.weight(.semibold))
                .padding(.horizontal, 4)
        }
        .accessibilityIdentifier("asides.pill")
    }
}

struct AsideListSheet: View {
    let asides: [AgentSession]
    let open: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List(asides) { aside in
                Button {
                    open(aside.id)
                    dismiss()
                } label: {
                    HStack(spacing: 12) {
                        AsideGlyph(state: aside.aside?.state ?? .open)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(aside.displayTitle)
                                .lineLimit(2)
                                .foregroundStyle(.primary)
                            Text(aside.aside?.detail ?? "")
                                .font(.footnote)
                                .foregroundStyle(aside.aside?.state == .asks ? Color.warningInk : Color.captionInk)
                        }
                        Spacer(minLength: 0)
                        RowChevron()
                    }
                    .listRowDensity()
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
            .navigationTitle("Asides")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Done") { dismiss() } }
            }
        }
        .presentationDetents([.medium, .large])
    }
}

struct SendBackSheet: View {
    enum Source: String, CaseIterable, Identifiable {
        case summary = "Summary"
        case last = "Last message"
        case quote = "Quote…"

        var id: String { rawValue }
    }

    let store: AgentsStore
    let aside: String
    let parent: String
    let parentTitle: String
    let blocked: String?
    @State var to: AsideTarget
    @State private var from: Source = .summary
    @State private var text = ""
    @State private var close = true
    @State private var sending = false
    @State private var failure: String?
    @State private var noSummary = false
    @Environment(\.dismiss) private var dismiss

    private var panel: AsidePanel? { store.transcripts[aside]?.asidePanel }
    private var messages: [TranscriptEntry] { store.transcripts[aside]?.agentMessages ?? [] }
    private var answers: Bool { panel?.answer == true }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    if answers {
                        Picker("To", selection: $to) {
                            Text("Answer question \(max(panel?.question ?? 1, 1))").tag(AsideTarget.answer)
                            Text("Draft in its composer").tag(AsideTarget.draft)
                        }
                        .pickerStyle(.segmented)
                        if to == .answer, let label = panel?.label.nilIfEmpty {
                            HStack(spacing: 8) {
                                Text("Picks").foregroundStyle(Color.captionInk)
                                Label(label, systemImage: "checkmark")
                                    .font(.footnote.weight(.semibold))
                                    .padding(.horizontal, 10)
                                    .frame(height: 26)
                                    .overlay(Capsule().strokeBorder(Color.accentColor, lineWidth: 1.5))
                                Text(panel?.multi == true ? "; the note goes to its composer" : "and adds a note").foregroundStyle(Color.captionInk)
                            }
                            .font(.footnote)
                        }
                    }
                    Picker("From", selection: $from) {
                        ForEach(Source.allCases) { Text($0.rawValue).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    if from == .quote {
                        quotes
                    }
                    ZStack(alignment: .topLeading) {
                        TextEditor(text: $text)
                            .frame(minHeight: 120)
                            .scrollContentBackground(.hidden)
                            .accessibilityIdentifier("sendBack.text")
                        if from == .summary, store.summaryAsked[aside] == nil, text.isEmpty {
                            Button("Ask \(agentName) for a Summary", systemImage: "text.badge.star") { requestSummary() }
                                .font(.subheadline.weight(.semibold))
                                .disabled(blocked != nil)
                                .padding(8)
                                .accessibilityIdentifier("sendBack.summary")
                        }
                    }
                    .padding(8)
                    .background(Color.surface2, in: .rect(cornerRadius: 12))
                    if noSummary, from == .last {
                        Text("No summary came back, so this is the aside's last message")
                            .font(.footnote)
                            .foregroundStyle(Color.captionInk)
                    }
                    if summarizing, from == .summary {
                        HStack(spacing: 8) {
                            ProgressView()
                            Text(fallback == nil ? "Writing a summary…" : "Writing a summary… the last message stands in until it's ready").foregroundStyle(Color.captionInk)
                        }
                        .font(.footnote)
                        .accessibilityIdentifier("sendBack.summarizing")
                    }
                    Toggle("Close the aside after", isOn: $close)
                        .tint(Color.accentColor)
                    if let failure {
                        Label(failure, systemImage: "exclamationmark.triangle.fill")
                            .font(.footnote)
                            .foregroundStyle(Color.dangerInk)
                    }
                }
                .padding()
            }
            .safeAreaInset(edge: .bottom) {
                VStack(spacing: 8) {
                    PrimaryCapsule(title: sending ? "Sending…" : "Send back", action: send)
                        .disabled(blocked != nil || sending || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                        .opacity(blocked != nil || sending || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? 0.5 : 1)
                        .accessibilityIdentifier("sendBack.send")
                    Text(blocked ?? (to == .answer ? "The card still waits for your Next / Send" : "It waits in its composer for you to send"))
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                }
                .padding(.horizontal)
                .padding(.bottom, 8)
            }
            .navigationTitle("Send back")
            .navigationSubtitle("to " + parentTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        store.forgetSummary(aside)
                        dismiss()
                    }
                }
            }
            .onChange(of: from, initial: true) { _, source in fill(source) }
            .onChange(of: summary) { _, summary in
                guard let summary, from == .summary, text.isEmpty || text == fallback else { return }
                text = summary
            }
            .onChange(of: summaryMissed) { _, missed in
                guard missed, from == .summary else { return }
                noSummary = true
                let unedited = text.isEmpty || text == fallback
                from = .last
                if unedited { text = fallback ?? "" }
            }
        }
        .onAppear { if !answers { to = .draft } }
    }

    private var quotes: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(messages.reversed()) { entry in
                if case .agent(let body) = entry.content {
                    Button {
                        text = body
                    } label: {
                        Text(body)
                            .font(.footnote)
                            .lineLimit(3)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(10)
                            .background(text == body ? Color.accentColor.opacity(0.22) : Color.surface2, in: .rect(cornerRadius: 12))
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    private var fallback: String? {
        let entries = store.transcripts[aside]?.entries ?? []
        let before = store.summaryAsked[aside].map { Array(entries.prefix($0)) } ?? entries
        guard let entry = before.last(where: { if case .agent = $0.content { true } else { false } }), case .agent(let body) = entry.content else { return nil }
        return body
    }

    private var summarizing: Bool {
        store.summaryAsked[aside] != nil && summary == nil && !summaryMissed
    }

    private var afterRequest: [TranscriptEntry]? {
        guard let asked = store.summaryAsked[aside], let transcript = store.transcripts[aside], !transcript.session.isTurnActive else { return nil }
        return Array(transcript.entries.dropFirst(asked))
    }

    private var summary: String? {
        guard let entry = afterRequest?.last(where: { if case .agent = $0.content { true } else { false } }), case .agent(let body) = entry.content else { return nil }
        return body
    }

    private var summaryMissed: Bool {
        guard let tail = afterRequest, summary == nil else { return false }
        return tail.contains { if case .user = $0.content { true } else { false } }
    }

    private var agentName: String {
        store.session(aside)?.agentName ?? "Claude"
    }

    private func requestSummary() {
        guard store.summaryAsked[aside] == nil, blocked == nil else { return }
        text = fallback ?? ""
        Task {
            do {
                try await store.asideSummary(aside)
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
            }
        }
    }

    private func fill(_ source: Source) {
        switch source {
        case .summary:
            if let summary {
                text = summary
            } else if store.summaryAsked[aside] == nil, store.session(aside)?.isTurnActive == true {
                from = .last
            } else if store.summaryAsked[aside] == nil {
                requestSummary()
            } else {
                text = fallback ?? ""
            }
        case .last:
            text = fallback ?? ""
        case .quote:
            break
        }
    }

    private func send() {
        sending = true
        Task {
            do {
                try await store.asideSend(aside, parent: parent, to: to, text: text, close: close)
                store.forgetSummary(aside)
                dismiss()
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
            }
            sending = false
        }
    }
}

extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}

struct AsideItem: Identifiable, Hashable {
    let id: String
}

struct SendBackStart: Identifiable {
    let id = UUID()
    let to: AsideTarget
}
