import SwiftUI

import CompanionKit

struct ConnectRepoCard: View {
    let card: ConnectCard
    let blocked: String?
    let canQueue: Bool
    let queued: QueuedAction?
    let queuedLine: String
    @Binding var draft: ConnectDraft
    let answer: @MainActor ([ConnectAnswer]) async throws -> Void
    let undo: () -> Void
    @State private var sending = false
    @State private var armed = false
    @State private var failure: String?
    @State private var fieldsHeight: CGFloat = 0

    var body: some View {
        DockCard {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 12) {
                    DockBadge(symbol: "folder.badge.plus")
                    Text(card.title)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Color.warningInk)
                }
                ScrollView {
                    Group {
                        if let repo = card.repos.first, card.single {
                            single(repo)
                        } else {
                            batch
                        }
                    }
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { fieldsHeight = $0 }
                }
                .frame(maxHeight: fieldsHeight)
                .scrollBounceBehavior(.basedOnSize)
                if let queued {
                    QueuedLine(item: queued, queuedLine: queuedLine, remove: undo)
                } else {
                    actions
                }
                if let failure {
                    Label(failure, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(Color.danger)
                }
            }
        }
        .task {
            try? await Task.sleep(for: .milliseconds(600))
            armed = true
        }
    }

    private func single(_ repo: ConnectCard.Repo) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            RepoName(name: repo.name, font: .title2.weight(.semibold))
            facts(repo)
            quote(repo)
            ForEach(card.effects ?? []) { effect in
                Text(PlainMarkdown.attributed([effect.text, effect.note].compactMap { $0 }.joined(separator: " · ")))
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var batch: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(card.visible(draft)) { repo in
                Button { draft.toggle(repo) } label: {
                    HStack(alignment: .top, spacing: 12) {
                        Image(systemName: draft.unticked.contains(repo.key) ? "circle" : "checkmark.circle.fill")
                            .font(.title3)
                            .foregroundStyle(draft.unticked.contains(repo.key) ? Color.captionInk : Color.accentColor)
                            .frame(height: 28)
                        VStack(alignment: .leading, spacing: 4) {
                            RepoName(name: repo.name, font: .headline)
                            facts(repo)
                            quote(repo)
                            if let lands = repo.landsBy {
                                Text("Lands by \(Text(lands).fontWeight(.semibold))\(repo.source.map { " · " + $0 } ?? "")")
                                    .font(.footnote)
                                    .foregroundStyle(Color.captionInk)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                            if let brings = repo.brings {
                                Text(PlainMarkdown.attributed([brings, repo.trust].compactMap { $0 }.joined(separator: " · ")))
                                    .font(.footnote)
                                    .foregroundStyle(Color.captionInk)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .padding(.vertical, 8)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityValue(draft.unticked.contains(repo.key) ? "Skipped" : "Connects")
            }
            if let more = card.more, !draft.unfolded {
                Button { Motion.disclose { draft.unfolded = true } } label: {
                    Label(more, systemImage: "chevron.down")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.captionInk)
                        .frame(minHeight: 44)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
            if let effect = card.effect {
                Text(PlainMarkdown.attributed(effect))
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .padding(.top, 4)
            }
        }
    }

    @ViewBuilder
    private func facts(_ repo: ConnectCard.Repo) -> some View {
        if let path = repo.path {
            Text(path)
                .font(.subheadline.monospaced())
                .foregroundStyle(Color.captionInk)
                .lineLimit(1)
                .truncationMode(.middle)
        }
        if !repo.facts.isEmpty {
            Text(repo.facts)
                .font(.footnote)
                .foregroundStyle(Color.captionInk)
                .lineLimit(2)
        }
        if repo.warn == true, let state = repo.state {
            Text(state)
                .font(.footnote)
                .foregroundStyle(Color.warningInk)
        }
    }

    @ViewBuilder
    private func quote(_ repo: ConnectCard.Repo) -> some View {
        if let reason = repo.reason {
            Text(verbatim: "“" + reason + "”")
                .font(.subheadline)
                .foregroundStyle(Color.captionInk)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.leading, 10)
                .overlay(alignment: .leading) {
                    Rectangle()
                        .fill(Color(.tertiaryLabel))
                        .frame(width: 2)
                }
        }
    }

    private var answerable: Bool {
        armed && !sending && (blocked == nil || canQueue)
    }

    private var actions: some View {
        VStack(spacing: 10) {
            Button { send(card.answers(draft)) } label: {
                Text(card.connect)
                    .font(.headline)
                    .frame(maxWidth: .infinity, minHeight: 50)
                    .foregroundStyle(Color.onAccent)
                    .background(Color.accentColor, in: .capsule)
                    .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .disabled(!answerable || !draft.connects(card))
            PlainCapsule(title: "Don't connect", tint: .primary) { send(card.declineAll) }
                .disabled(!answerable)
            if !card.single, let declined = card.declined {
                Text(declined)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            Text(ConnectCard.footnote)
                .font(.footnote)
                .foregroundStyle(Color.captionInk)
            if let blocked, !canQueue {
                Text(blocked)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
            }
        }
        .opacity(blocked == nil || canQueue ? 1 : 0.5)
    }

    private func send(_ answers: [ConnectAnswer]) {
        sending = true
        Task {
            do {
                try await answer(answers)
            } catch {
                failure = error.localizedDescription
            }
            sending = false
        }
    }
}

struct RepoName: View {
    let name: String
    var font: Font = .body

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "folder")
                .font(.subheadline)
                .foregroundStyle(Color.captionInk)
            Text(name)
                .font(font)
                .lineLimit(1)
                .truncationMode(.middle)
        }
    }
}

struct QueuedLine: View {
    let item: QueuedAction
    let queuedLine: String
    let remove: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: item.attempted || item.failure != nil ? "exclamationmark.circle" : "clock")
                .foregroundStyle(Color.captionInk)
            VStack(alignment: .leading, spacing: 2) {
                Text(item.line(queued: queuedLine))
                    .font(.subheadline.weight(.semibold))
                Text(item.detail)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(2)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if item.canUndo || item.canDismiss {
                Button(item.canUndo ? "Undo" : "Dismiss", action: remove)
                    .font(.subheadline.weight(.semibold))
                    .frame(minHeight: 44)
                    .contentShape(.rect)
            }
        }
        .accessibilityElement(children: .contain)
    }
}

struct LandDock: View {
    let card: LandCard
    let later: Bool
    let session: String?
    let blocked: String?
    let review: () -> Void
    let openSession: (String) -> Void
    let notNow: (Bool) -> Void

    var body: some View {
        if later {
            Button { notNow(false) } label: {
                HStack(spacing: 12) {
                    DockBadge(symbol: "arrow.triangle.merge")
                    Text(card.title)
                        .font(.subheadline.weight(.semibold))
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    RowChevron(always: true)
                }
                .padding(12)
                .background(Color.surface1, in: .rect(cornerRadius: 20))
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
        } else {
            VStack(spacing: 10) {
                Button(action: review) {
                    Text(card.reviewLabel)
                        .font(.headline)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        .frame(maxWidth: .infinity, minHeight: 50)
                        .foregroundStyle(.primary)
                        .background(Color.surface2, in: .capsule)
                        .contentShape(.capsule)
                }
                .buttonStyle(.plain)
                .disabled(blocked != nil)
                HStack(spacing: 10) {
                    if let session {
                        PlainCapsule(title: "Open its session", tint: .primary) { openSession(session) }
                    }
                    PlainCapsule(title: "Not now", tint: .primary) { notNow(true) }
                }
                if let blocked {
                    Text(blocked)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                }
            }
        }
    }
}

struct LandReviewSheet: View {
    let card: LandCard
    let store: AgentsStore
    let blocked: String?
    let merge: @MainActor (String?) async throws -> Void
    @State private var commits: LandCommits?
    @State private var failure: String?
    @State private var merging = false
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    RepoName(name: card.name, font: .title2.weight(.semibold))
                    if let detail = commits?.detail ?? card.detail {
                        Text(PlainMarkdown.attributed(detail))
                            .font(.subheadline)
                    }
                    if let rule = card.rule {
                        Text(rule)
                            .font(.footnote)
                            .foregroundStyle(Color.captionInk)
                    }
                    commitList
                }
                .padding()
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .background(Color(.systemGroupedBackground))
            .safeAreaInset(edge: .bottom) {
                VStack(spacing: 8) {
                    Button(action: run) {
                        Text(card.mergeLabel)
                            .font(.headline)
                            .frame(maxWidth: .infinity, minHeight: 50)
                            .foregroundStyle(Color.onAccent)
                            .background(Color.accentColor, in: .capsule)
                            .contentShape(.capsule)
                    }
                    .buttonStyle(.plain)
                    .disabled(!mergeable)
                    .opacity(mergeable ? 1 : 0.5)
                    if let message = blocked ?? failure {
                        Text(message)
                            .font(.footnote)
                            .foregroundStyle(failure != nil ? Color.danger : Color.captionInk)
                    }
                }
                .padding()
            }
            .navigationTitle(card.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .task { await load() }
        }
    }

    private var mergeable: Bool {
        card.canMerge(commits: commits, blocked: blocked) && !merging
    }

    private func load() async {
        do {
            commits = try await store.landCommits(card.request)
        } catch {
            commits = nil
            failure = (error as? RemoteError)?.message ?? error.localizedDescription
        }
    }

    @ViewBuilder
    private var commitList: some View {
        if let commits {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(commits.commits) { commit in
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text(commit.sha)
                            .font(.footnote.monospaced())
                            .foregroundStyle(Color.captionInk)
                        Text(verbatim: commit.title)
                            .font(.subheadline)
                            .lineLimit(2)
                    }
                    .padding(.vertical, 8)
                    .padding(.horizontal, 14)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                if let more = commits.more, more > 0 {
                    Text("\(more) more")
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 8)
                }
            }
            .background(Color.surface2, in: .rect(cornerRadius: 20))
        } else if failure == nil {
            ProgressView()
                .frame(maxWidth: .infinity)
        }
    }

    private func run() {
        merging = true
        Task {
            do {
                try await merge(commits?.tip)
                dismiss()
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
                await load()
            }
            merging = false
        }
    }
}

struct ReposCardView: View {
    let card: ReposCard
    let fold: ReposFold
    let blocked: String?
    let toggleCard: () -> Void
    let toggleRow: (ReposCard.Row) -> Void
    let run: (ReposCard.Action, ReposCard.Row) -> Void
    let archive: () -> Void
    @State private var leaving: Leaving?

    private struct Leaving: Identifiable {
        let action: ReposCard.Action
        let row: ReposCard.Row

        var id: String { row.id }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            VStack(alignment: .leading, spacing: 0) {
                header
                if !fold.collapsed(card) {
                    ForEach(Array(card.rows.enumerated()), id: \.element.id) { index, row in
                        if index > 0 { Divider() }
                        rowView(row)
                    }
                }
            }
            .background(Color.surface1, in: .rect(cornerRadius: 20))
            if !fold.collapsed(card), let footer = card.footer {
                Text(footer)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .padding(.horizontal, 4)
            }
            if let label = card.archive {
                PlainCapsule(title: label, tint: .primary, action: archive)
            }
        }
        .confirmationDialog(leaving?.action.confirm ?? "", isPresented: Binding(get: { leaving != nil }, set: { if !$0 { leaving = nil } }), titleVisibility: .visible, presenting: leaving) { item in
            Button(item.action.label, role: .destructive) { run(item.action, item.row) }
        }
    }

    private var header: some View {
        Button(action: toggleCard) {
            HStack(spacing: 12) {
                Image(systemName: "folder")
                    .foregroundStyle(Color.captionInk)
                Text(card.title)
                    .font(.headline)
                    .foregroundStyle(.primary)
                Spacer(minLength: 8)
                if fold.collapsed(card) {
                    segments
                        .font(.subheadline)
                        .lineLimit(1)
                } else if card.ordered {
                    Text("lands in order")
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
                }
            }
            .padding(.horizontal, 14)
            .frame(minHeight: 50)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }

    private var segments: Text {
        card.summary.enumerated().reduce(Text("")) { line, item in
            line + Text(item.offset > 0 ? " · " : "").foregroundStyle(Color.captionInk) + Text(item.element.text).foregroundStyle(item.element.tone.toneInk(default: Color.captionInk))
        }
    }

    private func rowView(_ row: ReposCard.Row) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Button { toggleRow(row) } label: {
                HStack(spacing: 12) {
                    Text("\(row.order)")
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
                        .frame(width: 28, height: 28)
                        .overlay(Circle().strokeBorder(Color(.tertiaryLabel), lineWidth: 1))
                    VStack(alignment: .leading, spacing: 2) {
                        RepoName(name: row.name, font: .body.weight(.semibold))
                        Text(row.state)
                            .font(.subheadline)
                            .foregroundStyle(row.tone.toneInk(default: Color.captionInk))
                            .lineLimit(1)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    if row.tone == "warning" {
                        SessionGlyphView(glyph: .cap, size: 24)
                    }
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .frame(minHeight: 50)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .disabled(row.opens != true)
            if fold.open(row) {
                openRow(row)
                    .padding(.horizontal, 14)
                    .padding(.bottom, 12)
            }
        }
    }

    private func openRow(_ row: ReposCard.Row) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            if let tail = row.tail, !tail.isEmpty {
                CodeBlock(text: tail, lines: 8)
                    .background(Color(.systemGroupedBackground), in: .rect(cornerRadius: 16))
            }
            if let note = row.note {
                Text(note)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
            }
            ForEach(row.buttons) { action in
                Group {
                    if action.kind == .fix {
                        Button { run(action, row) } label: {
                            Text(action.label)
                                .font(.headline)
                                .frame(maxWidth: .infinity, minHeight: 44)
                                .foregroundStyle(Color.onAccent)
                                .background(Color.accentColor, in: .capsule)
                                .contentShape(.capsule)
                        }
                        .buttonStyle(.plain)
                    } else {
                        PlainCapsule(title: action.label, tint: .primary) {
                            if action.confirm != nil {
                                leaving = Leaving(action: action, row: row)
                            } else {
                                run(action, row)
                            }
                        }
                    }
                }
                .disabled(action.kind != .session && blocked != nil)
                .opacity(action.kind != .session && blocked != nil ? 0.5 : 1)
            }
            if let blocked {
                Text(blocked)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
            }
        }
    }
}

extension Optional<String> {
    func toneInk(default fallback: Color) -> Color {
        switch self {
        case "warning": Color.warningInk
        case "danger": Color.dangerInk
        case "success": Color.successInk
        default: fallback
        }
    }
}

struct ArchiveSheet: View {
    let plan: ArchivePlan?
    let mac: String
    let unavailable: Bool
    let run: @MainActor (ArchivePlan, ArchiveMode) async throws -> Void
    @State private var failure: String?
    @State private var running = false
    @State private var height: CGFloat = 0
    @Environment(\.dismiss) private var dismiss

    private static let grabber: CGFloat = 24

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text("Archive this task?")
                    .font(.title2.weight(.bold))
                if let plan {
                    content(plan)
                } else if unavailable {
                    Text("\(mac) is offline, and this phone hasn't seen what Archive would do. Open this task again when the Mac is back.")
                        .foregroundStyle(Color.captionInk)
                    PlainCapsule(title: "Cancel", tint: .primary) { dismiss() }
                } else {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                }
            }
            .padding()
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
        }
        .scrollBounceBehavior(.basedOnSize)
        .presentationDetents(height > 0 ? [.height(height + Self.grabber)] : [.large])
        .presentationDragIndicator(.visible)
        .presentationBackground(Color.surface1)
    }

    @ViewBuilder
    private func content(_ plan: ArchivePlan) -> some View {
        Text(plan.lead)
            .foregroundStyle(Color.captionInk)
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(plan.repos.enumerated()), id: \.element.id) { index, repo in
                if index > 0 { Divider() }
                VStack(alignment: .leading, spacing: 4) {
                    RepoName(name: repo.name, font: .headline)
                    Text(repo.summary)
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
                    if let why = repo.why {
                        Text(why)
                            .font(.subheadline)
                            .foregroundStyle(repo.whyTone.toneInk(default: Color(.tertiaryLabel)))
                    }
                }
                .padding(14)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .background(Color.surface2, in: .rect(cornerRadius: 20))
        ForEach(plan.cronLines ?? [], id: \.self) { line in
            Label(line, systemImage: "clock")
                .font(.footnote)
                .foregroundStyle(Color.captionInk)
        }
        if let warning = plan.cronWarn {
            Label(warning, systemImage: "exclamationmark.triangle")
                .font(.footnote)
                .foregroundStyle(Color.warningInk)
        }
        Text(plan.rule(mac: mac))
            .font(.footnote)
            .foregroundStyle(Color.captionInk)
        if let message = failure ?? plan.held {
            Text(message)
                .font(.footnote)
                .foregroundStyle(Color.dangerInk)
        }
        Button { start(plan, .archive) } label: {
            Text("Archive task")
                .font(.headline)
                .frame(maxWidth: .infinity, minHeight: 50)
                .foregroundStyle(.primary)
                .background(Color.surface2, in: .capsule)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .disabled(running || plan.held != nil)
        HStack(spacing: 10) {
            PlainCapsule(title: "Close sessions only", tint: .primary) { start(plan, .closeOnly) }
                .disabled(running || plan.held != nil)
            PlainCapsule(title: "Cancel", tint: .primary) { dismiss() }
                .disabled(running)
        }
    }

    private func start(_ plan: ArchivePlan, _ mode: ArchiveMode) {
        running = true
        Task {
            do {
                try await run(plan, mode)
                dismiss()
            } catch {
                failure = error.localizedDescription
            }
            running = false
        }
    }
}

struct QueueNoteRow: View {
    let note: QueueNote
    let dismiss: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "exclamationmark.circle")
                .foregroundStyle(Color.captionInk)
            Text(note.text)
                .font(.footnote)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(action: dismiss) {
                Image(systemName: "xmark")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.captionInk)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Dismiss")
        }
        .padding(.leading, 12)
        .background(Color.surface1, in: .rect(cornerRadius: 20))
    }
}

struct FolderRemove {
    let run: @MainActor (String) async -> Void
}

extension EnvironmentValues {
    @Entry var folderRemove: FolderRemove?
}

struct AFKNoticeRow: View {
    let notice: Notice
    let afk: Notice.AFK
    @Environment(\.folderRemove) private var remove
    @State private var asking = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: "folder.badge.plus")
                    .foregroundStyle(Color.captionInk)
                    .frame(height: 22)
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 8) {
                        Text(notice.title)
                            .font(.subheadline.weight(.semibold))
                            .fixedSize(horizontal: false, vertical: true)
                        Text("AFK")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(Color.captionInk)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.surface2, in: .capsule)
                    }
                    Text(afk.detail)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                }
            }
            if let label = afk.remove, let remove, let path = notice.path {
                PlainCapsule(title: label, tint: .primary) { asking = true }
                    .confirmationDialog(label, isPresented: $asking, titleVisibility: .visible) {
                        Button(label, role: .destructive) {
                            Task { await remove.run(path) }
                        }
                    } message: {
                        Text("The files stay on disk. Cron jobs that run there pause.")
                    }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.surface1, in: .rect(cornerRadius: 20))
    }
}

struct PendingRepoRow: View {
    let row: RepoRow

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            SessionGlyphView(glyph: row.tone == "warning" ? .cap : .closed, size: 24)
            VStack(alignment: .leading, spacing: 2) {
                RepoName(name: row.name)
                Text(row.word ?? "")
                    .font(.subheadline)
                    .foregroundStyle(row.tone.toneInk(default: Color.captionInk))
                    .lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.leading, 24)
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
    }
}

struct QueueSection: View {
    @Environment(CompanionModel.self) private var model

    var body: some View {
        let queue = model.queue
        if !queue.items.isEmpty || !queue.notes.isEmpty {
            Section("Queued") {
                ForEach(queue.items) { item in
                    QueuedLine(item: item, queuedLine: model.connection(item.computer)?.queuedLine ?? "Queued") {
                        queue.cancel(item)
                    }
                }
                ForEach(queue.notes) { note in
                    QueueNoteRow(note: note) { queue.dismiss(note) }
                        .listRowInsets(EdgeInsets())
                }
            }
        }
    }
}
