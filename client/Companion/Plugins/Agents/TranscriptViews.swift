import SwiftUI

import CompanionKit


struct RowView: View {
    let row: TranscriptRow

    var body: some View {
        switch row {
        case .entry(let entry):
            EntryView(entry: entry)
                .opacity(entry.isOutgoing ? 0.6 : 1)
        case .agent(_, let blocks):
            MarkdownView(blocks: blocks)
        case .tools(_, let group):
            if group.calls.count == 1 {
                ToolRow(tool: group.calls[0])
            } else {
                ToolGroupRow(group: group)
            }
        case .thought(_, let text, let title):
            ThoughtView(text: text, title: title)
        case .answered(let request):
            AnsweredRow(request: request)
        case .asked(let question):
            AskedRow(question: question)
        }
    }
}

struct EntryView: View {
    let entry: TranscriptEntry

    var body: some View {
        switch entry.content {
        case .user(let text, let fromAgent, let images):
            UserBubble(text: text, fromAgent: fromAgent, images: images)
        case .agent(let text):
            MarkdownView(blocks: Markdown.blocks(text))
        case .thought(let text):
            ThoughtView(text: text, title: "Thought")
        case .tool(let tool):
            ToolRow(tool: tool)
        case .plan(let items):
            PlanCard(items: items)
        case .permission(let request):
            AnsweredRow(request: request)
        case .elicitation(let question):
            AskedRow(question: question)
        case .notice(let notice):
            NoticeView(notice: notice)
        case .compaction(let compaction):
            CompactionView(compaction: compaction)
        case .other:
            EmptyView()
        }
    }
}

struct UserBubble: View {
    let text: String
    let fromAgent: Bool
    let images: [Data]

    private var shown: [Data] {
        images.filter { !$0.isEmpty }
    }

    private var omitted: Int {
        images.count - shown.count
    }

    var body: some View {
        let images = shown
        VStack(alignment: .trailing, spacing: 4) {
            if fromAgent {
                Label("From another agent", systemImage: "sparkles")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if omitted > 0 {
                Label(omitted == 1 ? "1 image" : "\(omitted) images", systemImage: "photo.on.rectangle")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if images.count == 1, let image = UIImage(data: images[0]) {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFit()
                    .frame(maxWidth: 240, maxHeight: 240)
                    .clipShape(.rect(cornerRadius: 16))
            } else if images.count > 1 {
                ScrollView(.horizontal) {
                    HStack(spacing: 6) {
                        ForEach(Array(images.enumerated()), id: \.offset) { _, data in
                            if let image = UIImage(data: data) {
                                Image(uiImage: image)
                                    .resizable()
                                    .scaledToFill()
                                    .frame(width: 140, height: 140)
                                    .clipShape(.rect(cornerRadius: 12))
                            }
                        }
                    }
                }
                .scrollIndicators(.hidden)
                .defaultScrollAnchor(.trailing)
            }
            if !text.isEmpty {
                Text(Markdown.inline(text))
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .foregroundStyle(.primary)
                    .background(Color.surface2, in: .rect(cornerRadius: 20))
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .padding(.leading, 48)
    }
}

struct MarkdownView: View {
    let blocks: [MarkdownBlock]

    init(blocks: [MarkdownBlock]) {
        self.blocks = blocks
    }

    init(text: String) {
        blocks = Markdown.blocks(text)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                switch block {
                case .heading(let level, let text):
                    Text(Markdown.inline(text))
                        .font(level <= 1 ? .title2.bold() : level == 2 ? .title3.bold() : .headline)
                case .paragraph(let text):
                    Text(Markdown.inline(text))
                case .list(let items):
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text(item.marker)
                                    .monospacedDigit()
                                    .foregroundStyle(.secondary)
                                Text(Markdown.inline(item.text))
                            }
                            .padding(.leading, CGFloat(item.depth) * 16)
                        }
                    }
                case .quote(let text):
                    Text(Markdown.inline(text))
                        .foregroundStyle(.secondary)
                        .padding(.leading, 12)
                        .overlay(alignment: .leading) {
                            Capsule().fill(.quaternary).frame(width: 3)
                        }
                case .code(_, let text):
                    ScrollView(.horizontal, showsIndicators: false) {
                        Text(text)
                            .font(.callout.monospaced())
                            .padding(14)
                    }
                    .background(Color.surface1, in: .rect(cornerRadius: 16))
                }
            }
        }
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct ThoughtView: View {
    let text: String
    let title: String
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation(.snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .rotationEffect(.degrees(expanded ? 90 : 0))
                    Text(title)
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .frame(minHeight: 32)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if expanded {
                Text(text)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

struct CompactionView: View {
    let compaction: Compaction
    @State private var expanded = false

    private var detail: String {
        [compaction.error, compaction.summary].filter { !$0.isEmpty }.joined(separator: "\n\n")
    }

    var body: some View {
        let label = Label(compaction.title, systemImage: "arrow.down.right.and.arrow.up.left")
            .font(.callout)
            .foregroundStyle(compaction.status == "failed" ? .red : .secondary)
        if detail.isEmpty {
            label
                .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            DisclosureGroup(isExpanded: $expanded) {
                Text(detail)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.top, 4)
            } label: {
                label
            }
            .tint(.secondary)
        }
    }
}

struct ToolRow: View {
    let tool: ToolCall
    @State private var expanded: Bool

    init(tool: ToolCall) {
        self.tool = tool
        _expanded = State(initialValue: tool.status == .failed)
    }

    var body: some View {
        ToolLine(tool: tool, expanded: $expanded)
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .background(Color.surface1, in: .rect(cornerRadius: 20))
            .overlay {
                if tool.status == .failed {
                    RoundedRectangle(cornerRadius: 20).strokeBorder(Color.danger.opacity(0.5), lineWidth: 1)
                }
            }
            .onChange(of: tool.status) { _, status in
                if status == .failed { expanded = true }
            }
    }
}

struct SubagentStop {
    let enabled: Bool
    let run: @MainActor (String) async -> Bool
}

extension EnvironmentValues {
    @Entry var subagentStop: SubagentStop?
}

struct ToolLine: View {
    let tool: ToolCall
    @Binding var expanded: Bool
    @Environment(\.subagentStop) private var stop
    @State private var stopping = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button {
                withAnimation(.snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 10) {
                    Image(systemName: tool.kind.symbol)
                        .foregroundStyle(.secondary)
                        .frame(width: 20)
                    Text(tool.displayTitle)
                        .font(tool.kind == .execute ? .callout.monospaced() : .callout)
                        .lineLimit(1)
                        .foregroundStyle(tool.status == .failed ? Color.danger : .primary)
                    Spacer(minLength: 6)
                    if !tool.diffs.isEmpty {
                        ChangeCounts(changes: tool.changes)
                    }
                    ToolStatusIcon(status: tool.status)
                    if hasDetails {
                        Image(systemName: "chevron.right")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.tertiary)
                            .rotationEffect(.degrees(expanded ? 90 : 0))
                    }
                }
                .frame(minHeight: 28)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .allowsHitTesting(hasDetails)
            if tool.stoppable, let stop {
                PlainCapsule(title: stopping ? "Stopping…" : "Stop", tint: Color.danger) {
                    stopping = true
                    Task { stopping = await stop.run(tool.id) }
                }
                .disabled(!stop.enabled || stopping)
                .accessibilityLabel("Stop subagent")
            }
            if expanded {
                ForEach(Array(tool.diffs.enumerated()), id: \.offset) { _, diff in
                    DiffView(diff: diff)
                }
                if let command = tool.command, tool.diffs.isEmpty {
                    CodeBlock(text: command)
                }
                if !tool.output.isEmpty {
                    CodeBlock(text: tool.output)
                }
            }
        }
        .onChange(of: tool.stoppable) { _, _ in stopping = false }
    }

    private var hasDetails: Bool {
        !tool.diffs.isEmpty || !tool.output.isEmpty || tool.command != nil
    }
}

struct ToolGroupRow: View {
    let group: ToolGroup
    @State private var expanded: Bool
    @State private var open: Set<String> = []

    init(group: ToolGroup) {
        self.group = group
        _expanded = State(initialValue: group.failed)
        _open = State(initialValue: Set(group.calls.filter { $0.status == .failed }.map(\.id)))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button {
                withAnimation(.snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 12) {
                    Image(systemName: group.symbol)
                        .foregroundStyle(.secondary)
                        .frame(width: 22)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(group.title)
                            .font(.callout)
                            .foregroundStyle(group.failed ? Color.danger : .primary)
                        if !expanded {
                            HStack(spacing: 4) {
                                Text(group.detail)
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                                    .layoutPriority(-1)
                                if group.changes.added + group.changes.removed > 0 {
                                    ChangeCounts(changes: group.changes)
                                }
                            }
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        }
                    }
                    Spacer(minLength: 6)
                    if group.running {
                        ProgressView().controlSize(.small)
                    }
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(expanded ? 90 : 0))
                }
                .frame(minHeight: 36)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(group.title): \(group.detail)")
            if expanded {
                ForEach(group.calls, id: \.id) { call in
                    ToolLine(tool: call, expanded: Binding(
                        get: { open.contains(call.id) },
                        set: { if $0 { open.insert(call.id) } else { open.remove(call.id) } }
                    ))
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background(Color.surface1, in: .rect(cornerRadius: 20))
        .overlay {
            if group.failed {
                RoundedRectangle(cornerRadius: 20).strokeBorder(Color.danger.opacity(0.5), lineWidth: 1)
            }
        }
        .onChange(of: group.calls.filter { $0.status == .failed }.map(\.id)) { _, failed in
            guard !failed.isEmpty else { return }
            expanded = true
            open.formUnion(failed)
        }
    }
}

struct ChangeCounts: View {
    let changes: (added: Int, removed: Int)

    var body: some View {
        HStack(spacing: 4) {
            Text("+\(changes.added)").foregroundStyle(Color.success)
            Text("−\(changes.removed)").foregroundStyle(Color.danger)
        }
        .font(.footnote.monospacedDigit().weight(.medium))
        .fixedSize()
    }
}

struct ToolStatusIcon: View {
    let status: ToolStatus

    var body: some View {
        switch status {
        case .pending, .inProgress:
            ProgressView().controlSize(.small)
        case .completed:
            Image(systemName: "checkmark").foregroundStyle(.secondary).font(.caption.weight(.bold))
        case .failed:
            Image(systemName: "xmark").foregroundStyle(Color.danger).font(.caption.weight(.bold))
        case .cancelled:
            Image(systemName: "slash.circle").foregroundStyle(.secondary).font(.caption)
        case .other:
            EmptyView()
        }
    }
}

struct CodeBlock: View {
    let text: String
    var lines = 40

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Text(text)
                .font(.callout.monospaced())
                .lineLimit(lines)
                .textSelection(.enabled)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.systemGroupedBackground), in: .rect(cornerRadius: 16))
    }
}

struct DiffView: View {
    let diff: ToolCall.FileDiff
    var limit = 400

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Label(diff.fileName, systemImage: "doc")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
            ScrollView(.horizontal, showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(diff.lines.prefix(limit).enumerated()), id: \.offset) { _, line in
                        HStack(spacing: 6) {
                            Text(sign(line.kind))
                                .foregroundStyle(color(line.kind))
                                .frame(width: 10)
                            Text(line.text.isEmpty ? " " : line.text)
                        }
                        .font(.caption.monospaced())
                        .padding(.horizontal, 10)
                        .padding(.vertical, 1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(color(line.kind).opacity(line.kind == .context ? 0 : 0.12))
                    }
                }
            }
            .padding(.bottom, 6)
        }
        .background(Color(.systemGroupedBackground), in: .rect(cornerRadius: 16))
    }

    private func sign(_ kind: DiffLine.Kind) -> String {
        switch kind {
        case .added: "+"
        case .removed: "−"
        case .hunk, .context: " "
        }
    }

    private func color(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: Color.success
        case .removed: Color.danger
        case .hunk: .accentColor
        case .context: .secondary
        }
    }
}

struct PlanCard: View {
    let items: [PlanItem]

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Plan")
                Spacer()
                Text("\(items.filter { $0.status == .completed }.count) of \(items.count)")
                    .monospacedDigit()
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    Image(systemName: symbol(item.status))
                        .foregroundStyle(color(item.status))
                        .frame(width: 20)
                    Text(item.content)
                        .fontWeight(item.status == .inProgress ? .semibold : .regular)
                        .foregroundStyle(item.status == .inProgress ? .primary : .secondary)
                }
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.surface1, in: .rect(cornerRadius: 20))
    }

    private func symbol(_ status: PlanStatus) -> String {
        switch status {
        case .completed: "checkmark"
        case .inProgress: "play.circle"
        case .pending: "circle"
        }
    }

    private func color(_ status: PlanStatus) -> Color {
        switch status {
        case .completed: .secondary
        case .inProgress: .accentColor
        case .pending: Color(.tertiaryLabel)
        }
    }
}

struct AnsweredRow: View {
    let request: PermissionRequest

    var body: some View {
        Label {
            (Text(request.verdict + " · ") + Text(request.subject).font(.footnote.monospaced()) + Text(time))
                .lineLimit(1)
                .truncationMode(.middle)
                .accessibilityLabel(request.summary)
        } icon: {
            Image(systemName: request.allowed ? "checkmark" : request.outcome == nil ? "clock" : "xmark")
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var time: String {
        request.answeredAt.map { " · " + $0.formatted(date: .omitted, time: .shortened) } ?? ""
    }
}

struct DockCard<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        content
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background {
                RoundedRectangle(cornerRadius: 20)
                    .fill(Color.surface1)
                    .overlay(RoundedRectangle(cornerRadius: 20).fill(Color.warning.opacity(0.10)))
            }
            .overlay(RoundedRectangle(cornerRadius: 20).strokeBorder(Color.warning.opacity(0.55), lineWidth: 1))
    }
}

struct DockBadge: View {
    let symbol: String

    var body: some View {
        Image(systemName: symbol)
            .font(.footnote.weight(.bold))
            .foregroundStyle(Color.onAccent)
            .frame(width: 28, height: 28)
            .background(Color.warning, in: .circle)
    }
}

struct PermissionCard: View {
    let request: PermissionRequest
    let more: Int
    let agentName: String
    let place: String
    let blocked: String?
    let choose: @MainActor (String) async throws -> Void
    @State private var chosen: String?
    @State private var failure: String?
    @State private var confirmingAlways = false
    @State private var showingAll = false
    @State private var armed = false

    var body: some View {
        DockCard {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 12) {
                    DockBadge(symbol: "hand.raised.fill")
                    Text(request.question)
                        .font(.title3.weight(.semibold))
                        .lineLimit(2)
                    Spacer(minLength: 0)
                    if let never = request.option(.rejectAlways) {
                        Menu {
                            Button("Reject and don't ask again", systemImage: "hand.raised.slash", role: .destructive) { pick(never) }
                        } label: {
                            Image(systemName: "ellipsis")
                                .font(.body.weight(.semibold))
                                .foregroundStyle(.secondary)
                                .frame(width: 44, height: 44)
                                .contentShape(.rect)
                        }
                        .tint(.secondary)
                        .accessibilityLabel("More Answers")
                        .disabled(!answerable)
                    }
                }
                CodeBlock(text: request.subject, lines: PermissionRequest.dockCommandLines)
                ForEach(Array(request.toolCall.diffs.prefix(1).enumerated()), id: \.offset) { _, diff in
                    DiffView(diff: diff, limit: PermissionRequest.dockDiffLines)
                }
                if let hidden = request.hiddenInDock {
                    Button { showingAll = true } label: {
                        Label("Hidden: \(hidden) · Show all", systemImage: "eye")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(Color.warning)
                            .frame(minHeight: 44)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                }
                if let caption = caption {
                    Text(caption)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                VStack(spacing: 10) {
                    if let once = request.option(.allowOnce) {
                        Button { pick(once) } label: {
                            Text("Allow once")
                                .font(.headline)
                                .frame(maxWidth: .infinity, minHeight: 50)
                                .foregroundStyle(Color.onAccent)
                                .background(Color.accentColor, in: .capsule)
                                .contentShape(.capsule)
                        }
                        .buttonStyle(.plain)
                    }
                    HStack(spacing: 10) {
                        if request.option(.allowAlways) != nil {
                            PlainCapsule(title: "Always allow…", tint: .primary) { confirmingAlways = true }
                        }
                        if let reject = request.option(.rejectOnce) {
                            PlainCapsule(title: "Reject", tint: Color.danger) { pick(reject) }
                        }
                    }
                }
                .disabled(!answerable)
                .opacity(enabled ? 1 : 0.5)
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
        .sheet(isPresented: $showingAll) {
            NavigationStack {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        CodeBlock(text: request.subject, lines: Int.max)
                        ForEach(Array(request.toolCall.diffs.enumerated()), id: \.offset) { _, diff in
                            DiffView(diff: diff, limit: Int.max)
                        }
                    }
                    .padding()
                }
                .navigationTitle(request.question)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { showingAll = false }
                    }
                }
            }
        }
        .sheet(isPresented: $confirmingAlways) {
            AlwaysAllowSheet(request: request, agentName: agentName, place: place) {
                confirmingAlways = false
                if let always = request.option(.allowAlways) { pick(always) }
            }
        }
    }

    private var enabled: Bool {
        blocked == nil
    }

    private var answerable: Bool {
        enabled && armed && chosen == nil
    }

    private var caption: String? {
        if let blocked { return blocked }
        let intent = (request.toolCall.intent ?? (request.toolCall.displayTitle != request.subject ? request.toolCall.displayTitle : nil)).map { "\(agentName) says: \($0)" }
        let rest = more > 0 ? (more == 1 ? "1 more request waiting" : "\(more) more requests waiting") : nil
        let parts = [intent, rest].compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    private func pick(_ option: PermissionOption) {
        chosen = option.optionId
        failure = nil
        Task {
            do {
                try await choose(option.optionId)
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
                chosen = nil
            }
        }
    }
}

struct PlainCapsule: View {
    let title: String
    let tint: Color
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(.headline)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .frame(maxWidth: .infinity, minHeight: 44)
                .foregroundStyle(tint)
                .background(Color.surface2, in: .capsule)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
    }
}

struct AlwaysAllowSheet: View {
    let request: PermissionRequest
    let agentName: String
    let place: String
    let confirm: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var height: CGFloat = 360

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(request.alwaysTitle)
                .font(.title3.weight(.semibold))
            Text(scope)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            if let label = request.option(.allowAlways)?.name {
                Text("\(agentName)'s option: “\(label)”")
                    .font(.subheadline)
                    .fixedSize(horizontal: false, vertical: true)
            }
            VStack(alignment: .leading, spacing: 4) {
                Text("Likely rule, read from that label")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                CodeBlock(text: request.alwaysRule, lines: 4)
            }
            VStack(spacing: 6) {
                PlainCapsule(title: "Always allow", tint: .primary, action: confirm)
                Button { dismiss() } label: {
                    Text("Cancel")
                        .font(.headline)
                        .frame(maxWidth: .infinity, minHeight: 44)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .keyboardShortcut(.cancelAction)
            }
        }
        .padding(24)
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
        .presentationDetents([.height(height)])
        .presentationDragIndicator(.hidden)
    }

    private var scope: AttributedString {
        let what = request.toolCall.kind == .execute ? "run matching commands" : "do this"
        if request.alwaysForSession {
            return Markdown.inline("\(agentName) will \(what) without asking; its option says until this session ends.")
        }
        let place = place.isEmpty ? "this workspace" : "**\(place)**"
        return Markdown.inline("\(agentName) will \(what) in \(place) without asking, on this Mac, until you remove the rule it stores (usually in `.claude/settings.local.json`).")
    }
}

struct QuestionCard: View {
    let question: String
    let agentName: String
    let subagent: Bool

    var body: some View {
        DockCard {
            VStack(alignment: .leading, spacing: 10) {
                HStack(spacing: 12) {
                    DockBadge(symbol: "questionmark.bubble.fill")
                    Text("\(agentName) is asking")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Color.warning)
                }
                if !question.isEmpty {
                    Text(Markdown.inline(question))
                        .font(.title3.weight(.semibold))
                        .lineLimit(4)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Text(subagent ? "Paused until you answer. Your reply resumes the subagent." : "Paused until you answer.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

struct AskedRow: View {
    let question: Elicitation

    var body: some View {
        Label {
            Text((question.answer ?? "No longer pending") + " · " + question.request.message)
                .lineLimit(1)
                .truncationMode(.tail)
        } icon: {
            Image(systemName: question.response?.action == "accept" ? "checkmark" : question.response == nil ? "clock" : "xmark")
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct ElicitationCard: View {
    let question: Elicitation
    let agentName: String
    let blocked: String?
    let send: @MainActor (String, [Elicitation.Answer]) async throws -> Void
    @State private var picks: [String: Set<String>] = [:]
    @State private var texts: [String: String] = [:]
    @State private var toggles: [String: Bool] = [:]
    @State private var sending = false
    @State private var failure: String?
    @State private var fieldsHeight: CGFloat = 0
    @State private var armed = false
    @Environment(\.openURL) private var openURL

    private static let maxFieldsHeight: CGFloat = 300

    var body: some View {
        DockCard {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 12) {
                    DockBadge(symbol: question.isURL ? "person.badge.key.fill" : "questionmark.bubble.fill")
                    Text(question.isURL ? "\(agentName) needs you to sign in" : "\(agentName) is asking")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Color.warning)
                }
                Text(Markdown.inline(question.request.message))
                    .font(.title3.weight(.semibold))
                    .lineLimit(4)
                    .fixedSize(horizontal: false, vertical: true)
                if question.isURL {
                    link
                } else {
                    ScrollView {
                        fields
                            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { fieldsHeight = $0 }
                    }
                    .frame(height: min(fieldsHeight, Self.maxFieldsHeight))
                    .scrollBounceBehavior(.basedOnSize)
                }
                actions
                    .disabled(!answerable)
                    .opacity(enabled ? 1 : 0.5)
                if let blocked {
                    Text(blocked)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                if let failure {
                    Label(failure, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(Color.danger)
                }
            }
        }
        .accessibilityElement(children: .contain)
        .task {
            try? await Task.sleep(for: .milliseconds(600))
            armed = true
        }
    }

    private var enabled: Bool {
        blocked == nil
    }

    private var answerable: Bool {
        enabled && armed && !sending
    }

    @ViewBuilder
    private var link: some View {
        if let url = question.request.url {
            CodeBlock(text: url, lines: 2)
        }
    }

    private var fields: some View {
        let single = question.singleChoice
        return VStack(alignment: .leading, spacing: 12) {
            ForEach(question.request.fields) { field in
                VStack(alignment: .leading, spacing: 6) {
                    if field.key != single?.key, !field.title.isEmpty || !field.description.isEmpty {
                        Text(field.description.isEmpty || field.type == "text" ? field.label : field.description)
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.secondary)
                    }
                    input(field, single: field.key == single?.key)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private func input(_ field: Elicitation.Field, single: Bool) -> some View {
        switch field.type {
        case "choice", "multi":
            ForEach(field.options) { option in
                let selected = picks[field.key, default: []].contains(option.value)
                Button {
                    if single {
                        submit("accept", [Elicitation.Answer(key: field.key, value: option.value)])
                    } else {
                        toggle(field, option)
                    }
                } label: {
                    optionRow(option, symbol: single ? nil : selected ? "checkmark.circle.fill" : "circle")
                }
                .buttonStyle(.plain)
                .disabled(!answerable)
            }
        case "boolean":
            Toggle(field.label, isOn: Binding(get: { toggles[field.key] ?? false }, set: { toggles[field.key] = $0 }))
        default:
            TextField(field.type == "text" ? (field.description.isEmpty ? field.label : field.description) : field.label, text: Binding(get: { texts[field.key] ?? "" }, set: { texts[field.key] = $0 }), axis: .vertical)
                .lineLimit(1...4)
                .keyboardType(field.type == "text" ? .default : .decimalPad)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color.surface2, in: .rect(cornerRadius: 14))
        }
    }

    private func optionRow(_ option: Elicitation.Option, symbol: String?) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            if let symbol {
                Image(systemName: symbol)
                    .foregroundStyle(symbol == "circle" ? Color.secondary : Color.accentColor)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(option.label)
                    .font(.headline)
                if let detail = option.description, !detail.isEmpty {
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .padding(.vertical, 4)
        .background(Color.surface2, in: .rect(cornerRadius: 14))
        .contentShape(.rect)
    }

    @ViewBuilder
    private var actions: some View {
        if question.isURL {
            VStack(spacing: 10) {
                primary("Open") {
                    if let link = question.request.url, let url = URL(string: link) { openURL(url) }
                    submit("accept", [])
                }
                PlainCapsule(title: "Decline", tint: Color.danger) { submit("decline", []) }
            }
        } else {
            HStack(spacing: 10) {
                if question.singleChoice == nil || question.request.fields.count > 1 {
                    primary("Send") { submit("accept", answers) }
                }
                PlainCapsule(title: "Skip", tint: .primary) { submit("decline", []) }
            }
        }
    }

    private func primary(_ title: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(.headline)
                .frame(maxWidth: .infinity, minHeight: 50)
                .foregroundStyle(Color.onAccent)
                .background(Color.accentColor, in: .capsule)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
    }

    private var answers: [Elicitation.Answer] {
        question.request.fields.flatMap { field -> [Elicitation.Answer] in
            switch field.type {
            case "choice", "multi":
                return field.options.filter { picks[field.key, default: []].contains($0.value) }.map { Elicitation.Answer(key: field.key, value: $0.value) }
            case "boolean":
                return toggles[field.key].map { [Elicitation.Answer(key: field.key, value: $0 ? "true" : "false")] } ?? []
            default:
                let text = texts[field.key] ?? ""
                return text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? [] : [Elicitation.Answer(key: field.key, value: text)]
            }
        }
    }

    private func toggle(_ field: Elicitation.Field, _ option: Elicitation.Option) {
        var set = picks[field.key, default: []]
        if set.contains(option.value) {
            set.remove(option.value)
        } else if field.type == "choice" {
            set = [option.value]
        } else {
            set.insert(option.value)
        }
        picks[field.key] = set
    }

    private func submit(_ action: String, _ answers: [Elicitation.Answer]) {
        sending = true
        failure = nil
        Task {
            do {
                try await send(action, answers)
            } catch {
                failure = (error as? RemoteError)?.message ?? error.localizedDescription
                sending = false
            }
        }
    }
}

struct NoticeView: View {
    let notice: Notice

    var body: some View {
        VStack(spacing: 2) {
            Label(notice.title, systemImage: "info.circle")
                .font(.footnote.weight(.semibold))
            if !notice.description.isEmpty {
                Text(notice.description)
                    .font(.footnote)
                    .multilineTextAlignment(.center)
            }
        }
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 4)
    }
}
