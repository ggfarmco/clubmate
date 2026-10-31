import SwiftUI

import CompanionKit

struct EntryView: View {
    let entry: TranscriptEntry
    let session: AgentSession?
    let answer: @MainActor (String, String) async throws -> Void

    var body: some View {
        switch entry.content {
        case .user(let text, let fromAgent, let images):
            UserBubble(text: text, fromAgent: fromAgent, images: images)
        case .agent(let text):
            MarkdownView(text: text)
        case .thought(let text):
            ThoughtView(text: text)
        case .tool(let tool):
            ToolRow(tool: tool)
        case .plan(let items):
            PlanCard(items: items)
        case .permission(let request):
            PermissionCard(request: request, pending: session?.pending.contains(request.id) ?? false) { option in
                try await answer(request.id, option)
            }
        case .notice(let notice):
            NoticeView(notice: notice)
        case .other:
            EmptyView()
        }
    }
}

struct UserBubble: View {
    let text: String
    let fromAgent: Bool
    let images: [Data]

    var body: some View {
        VStack(alignment: .trailing, spacing: 4) {
            if fromAgent {
                Label("From another agent", systemImage: "sparkles")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            ForEach(Array(images.enumerated()), id: \.offset) { _, data in
                if let image = UIImage(data: data) {
                    Image(uiImage: image)
                        .resizable()
                        .scaledToFit()
                        .frame(maxWidth: 240, maxHeight: 240)
                        .clipShape(.rect(cornerRadius: 16))
                }
            }
            if !text.isEmpty {
                Text(Markdown.inline(text))
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .foregroundStyle(fromAgent ? Color.primary : Color.white)
                    .background(fromAgent ? AnyShapeStyle(.fill.secondary) : AnyShapeStyle(.tint), in: .rect(cornerRadius: 20))
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .padding(.leading, 48)
    }
}

struct MarkdownView: View {
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(Markdown.blocks(text).enumerated()), id: \.offset) { _, block in
                switch block {
                case .heading(let level, let text):
                    Text(Markdown.inline(text))
                        .font(level <= 1 ? .title2.bold() : level == 2 ? .title3.bold() : .headline)
                case .paragraph(let text):
                    Text(Markdown.inline(text))
                case .bullet(let marker, let depth, let text):
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(marker)
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                        Text(Markdown.inline(text))
                    }
                    .padding(.leading, CGFloat(depth) * 16)
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
                            .padding(12)
                    }
                    .background(.fill.tertiary, in: .rect(cornerRadius: 12))
                }
            }
        }
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct ThoughtView: View {
    let text: String
    @State private var expanded = false

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            Text(text)
                .font(.callout)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, 4)
        } label: {
            Label("Thinking", systemImage: "brain")
                .font(.callout)
                .foregroundStyle(.secondary)
        }
        .tint(.secondary)
    }
}

struct ToolRow: View {
    let tool: ToolCall
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button {
                withAnimation(.snappy) { expanded.toggle() }
            } label: {
                HStack(spacing: 10) {
                    Image(systemName: tool.symbol)
                        .foregroundStyle(.secondary)
                        .frame(width: 20)
                    Text(tool.displayTitle)
                        .font(tool.kind == "execute" ? .callout.monospaced() : .callout)
                        .lineLimit(1)
                        .foregroundStyle(.primary)
                    Spacer(minLength: 6)
                    if !tool.diffs.isEmpty {
                        let changes = tool.changes
                        HStack(spacing: 4) {
                            Text("+\(changes.added)").foregroundStyle(.green)
                            Text("−\(changes.removed)").foregroundStyle(.red)
                        }
                        .font(.caption.monospacedDigit().weight(.medium))
                    }
                    ToolStatus(phase: tool.phase)
                    if hasDetails {
                        Image(systemName: "chevron.right")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.tertiary)
                            .rotationEffect(.degrees(expanded ? 90 : 0))
                    }
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .allowsHitTesting(hasDetails)
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
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(.fill.quaternary, in: .rect(cornerRadius: 14))
    }

    private var hasDetails: Bool {
        !tool.diffs.isEmpty || !tool.output.isEmpty || tool.command != nil
    }
}

struct ToolStatus: View {
    let phase: ToolCall.Phase

    var body: some View {
        switch phase {
        case .running:
            ProgressView().controlSize(.small)
        case .completed:
            Image(systemName: "checkmark").foregroundStyle(.green).font(.caption.weight(.bold))
        case .failed:
            Image(systemName: "xmark").foregroundStyle(.red).font(.caption.weight(.bold))
        case .cancelled:
            Image(systemName: "slash.circle").foregroundStyle(.secondary).font(.caption)
        }
    }
}

struct CodeBlock: View {
    let text: String

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Text(text)
                .font(.caption.monospaced())
                .lineLimit(40)
                .textSelection(.enabled)
                .padding(10)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.background.secondary, in: .rect(cornerRadius: 10))
    }
}

struct DiffView: View {
    let diff: ToolCall.FileDiff

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Label(diff.fileName, systemImage: "doc")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
            ScrollView(.horizontal, showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(diff.lines.prefix(400).enumerated()), id: \.offset) { _, line in
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
        .background(.background.secondary, in: .rect(cornerRadius: 10))
    }

    private func sign(_ kind: DiffLine.Kind) -> String {
        switch kind {
        case .added: "+"
        case .removed: "−"
        case .context: " "
        }
    }

    private func color(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: .green
        case .removed: .red
        case .context: .secondary
        }
    }
}

struct PlanCard: View {
    let items: [PlanItem]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label("Plan", systemImage: "list.bullet.clipboard")
                .font(.subheadline.weight(.semibold))
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    Image(systemName: symbol(item.status))
                        .foregroundStyle(color(item.status))
                    Text(item.content)
                        .strikethrough(item.status == "completed", color: .secondary)
                        .foregroundStyle(item.status == "completed" ? .secondary : .primary)
                }
                .font(.callout)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.fill.quaternary, in: .rect(cornerRadius: 16))
    }

    private func symbol(_ status: String) -> String {
        switch status {
        case "completed": "checkmark.circle.fill"
        case "in_progress": "circle.dotted.circle"
        default: "circle"
        }
    }

    private func color(_ status: String) -> Color {
        switch status {
        case "completed": .green
        case "in_progress": .accentColor
        default: .secondary
        }
    }
}

struct PermissionCard: View {
    let request: PermissionRequest
    let pending: Bool
    let choose: @MainActor (String) async throws -> Void
    @State private var chosen: String?
    @State private var failure: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label(pending ? "Permission Needed" : "Permission", systemImage: "hand.raised.fill")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(pending ? .orange : .secondary)
            HStack(spacing: 10) {
                Image(systemName: request.toolCall.symbol)
                    .foregroundStyle(.secondary)
                Text(request.toolCall.displayTitle)
                    .font(.body.weight(.medium))
            }
            if let command = request.toolCall.command, command != request.toolCall.displayTitle {
                CodeBlock(text: command)
            }
            ForEach(Array(request.toolCall.diffs.enumerated()), id: \.offset) { _, diff in
                DiffView(diff: diff)
            }
            if pending {
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) { buttons }
                    VStack(alignment: .leading, spacing: 8) { buttons }
                }
                if let failure {
                    Label(failure, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.red)
                }
            } else {
                Label(request.answer ?? "No longer pending", systemImage: request.outcome?.outcome == "selected" ? "checkmark.circle" : "clock.badge.xmark")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            RoundedRectangle(cornerRadius: 18)
                .fill(pending ? AnyShapeStyle(Color.orange.opacity(0.10)) : AnyShapeStyle(.fill.quaternary))
        }
        .overlay {
            if pending {
                RoundedRectangle(cornerRadius: 18).strokeBorder(.orange.opacity(0.4), lineWidth: 1)
            }
        }
    }

    @ViewBuilder
    private var buttons: some View {
        ForEach(request.options) { option in
            if option.isAllow {
                Button(option.name) { pick(option) }
                    .buttonStyle(.glassProminent)
                    .tint(option.kind == "allow_once" ? .accentColor : .green)
            } else {
                Button(option.name, role: .destructive) { pick(option) }
                    .buttonStyle(.glass)
            }
        }
        .disabled(chosen != nil)
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
