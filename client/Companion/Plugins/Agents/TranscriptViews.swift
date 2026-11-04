import SwiftUI

import ClubmateDocument
import CompanionKit


struct RowView: View {
    let row: TranscriptRow

    var body: some View {
        switch row {
        case .entry(let entry):
            EntryView(entry: entry)
                .opacity(entry.isOutgoing ? 0.6 : 1)
        case .agent(let id, let blocks, let docLinks, let docCards, let images, let imageLinks):
            AgentMessage(cards: docCards, entry: id, images: images) { MarkdownView(blocks: blocks, docLinks: docLinks, imageLinks: imageLinks) }
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
        case .user(let text, let fromAgent, let images, let files):
            UserBubble(text: text, fromAgent: fromAgent, images: images, files: files)
        case .voice(let prompt):
            VoicePromptRow(prompt: prompt)
        case .agent(let text):
            AgentMessage(cards: entry.docCards, entry: entry.id, images: entry.images) { MarkdownView(text: text, docLinks: entry.docLinks, imageLinks: entry.imageLinks) }
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
    var files: [String] = []

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
            ForEach(Array(files.enumerated()), id: \.offset) { _, name in
                Label(name, systemImage: "doc")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
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
                UserText(text: text)
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

struct VoicePromptRow: View {
    let prompt: VoicePrompt
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .trailing, spacing: 6) {
            Button {
                Motion.disclose { expanded.toggle() }
            } label: {
                HStack(spacing: 8) {
                    Text("🎙 \(prompt.heard)")
                        .lineLimit(1)
                        .truncationMode(.tail)
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .rotationEffect(.degrees(expanded ? 90 : 0))
                }
                .padding(.horizontal, 14)
                .frame(minHeight: 44)
                .background(Color.surface2, in: .capsule)
                .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Voice: \(prompt.heard)")
            .accessibilityHint(expanded ? "Hides the prompt sent to the Voice session" : "Shows the prompt sent to the Voice session")
            if expanded {
                ScrollView {
                    Text(prompt.json)
                        .font(.footnote.monospaced())
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(12)
                }
                .frame(maxHeight: 480)
                .fixedSize(horizontal: false, vertical: true)
                .background(Color.surface2, in: .rect(cornerRadius: 16))
                .clipShape(.rect(cornerRadius: 16))
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .padding(.leading, 48)
    }
}

private struct UserText: View {
    let text: String
    @State private var expanded = false

    var body: some View {
        if let long = LongMessage(text) {
            VStack(alignment: .leading, spacing: 8) {
                if expanded {
                    ScrollView {
                        Text(Markdown.inline(text))
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .frame(maxHeight: 480)
                } else {
                    Text(Markdown.inline(long.preview))
                        .textSelection(.enabled)
                }
                Button {
                    Motion.disclose { expanded.toggle() }
                } label: {
                    Text(expanded ? "Show less" : long.lines > LongMessage.previewLines ? "Show all \(long.lines) lines" : "Show all")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .frame(minHeight: 32)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
        } else {
            Text(Markdown.inline(text))
                .textSelection(.enabled)
        }
    }
}

struct AgentMessage<Content: View>: View {
    let cards: [DocCard]
    var entry = ""
    var images: [ImageRef] = []
    @ViewBuilder let text: Content
    @Environment(\.openDoc) private var openDoc
    @Environment(\.imageStore) private var imageStore
    @Environment(\.imageSession) private var imageSession
    @State private var shown: ImageRef?

    var body: some View {
        let cards = openDoc == nil ? [] : cards
        let store = imageSession.isEmpty || images.isEmpty ? nil : imageStore
        Group {
            if cards.isEmpty && store == nil {
                text
            } else {
                let groups = DocCardGroups(cards)
                VStack(alignment: .leading, spacing: 12) {
                    text
                    ForEach(groups.presented) { card in
                        DocCardButton(card: card) { openDoc?(card.path) }
                    }
                    if let plate = groups.plate {
                        MentionedDocs(label: plate, cards: groups.mentioned) { openDoc?($0) }
                    }
                    if let store {
                        ImageTiles(images: images, entry: entry, session: imageSession, store: store) { shown = $0 }
                    }
                }
            }
        }
        .environment(\.openImage, store == nil ? nil : { path in shown = images.first { $0.path == path } })
        .fullScreenCover(item: $shown) { ref in
            if let store {
                ImageScreen(ref: ref, entry: entry, session: imageSession, store: store)
            }
        }
    }
}

private struct MentionedDocs: View {
    let label: String
    let cards: [DocCard]
    let open: (String) -> Void
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Button {
                Motion.disclose { expanded.toggle() }
            } label: {
                HStack(spacing: 8) {
                    Image(systemName: "doc.on.doc")
                    Text(label)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .rotationEffect(.degrees(expanded ? 90 : 0))
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 14)
                .frame(minHeight: 44)
                .background(Color.surface2, in: .capsule)
                .contentShape(.capsule)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(label)
            .accessibilityValue(expanded ? "Expanded" : "Collapsed")
            .accessibilityHint(expanded ? "Hides the docs this message mentions" : "Shows the docs this message mentions")
            if expanded {
                ForEach(cards) { card in
                    DocCardButton(card: card) { open(card.path) }
                }
            }
        }
    }
}

private struct DocCardButton: View {
    let card: DocCard
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: card.kind.symbol)
                    .font(.body)
                    .foregroundStyle(.tint)
                    .frame(width: 40, height: 40)
                    .background(.tint.opacity(0.18), in: .rect(cornerRadius: 10))
                VStack(alignment: .leading, spacing: 2) {
                    Text(card.title)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                        .truncationMode(.tail)
                    HStack(spacing: 0) {
                        Text("\(card.kind.label) · ")
                            .fixedSize()
                        Text(card.rel)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .layoutPriority(1)
                Image(systemName: "arrow.up.forward.square")
                    .font(.body)
                    .foregroundStyle(.secondary)
            }
            .padding(12)
            .frame(maxWidth: .infinity, minHeight: 64, alignment: .leading)
            .background(Color.surface1, in: .rect(cornerRadius: 20))
            .overlay { RoundedRectangle(cornerRadius: 20).strokeBorder(.separator, lineWidth: 0.5) }
            .contentShape(.rect(cornerRadius: 20))
        }
        .buttonStyle(DocCardPressStyle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Open \(card.title)")
        .accessibilityValue(card.subtitle)
        .accessibilityAddTraits(.isButton)
    }
}

private struct DocCardPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.6 : 1)
    }
}

struct MarkdownView: View {
    let blocks: [MarkdownBlock]
    var docLinks: [String: String] = [:]
    var imageLinks: [String: String] = [:]
    @Environment(\.openDoc) private var openDoc
    @Environment(\.openImage) private var openImage
    @Environment(\.openURL) private var openURL

    init(blocks: [MarkdownBlock], docLinks: [String: String] = [:], imageLinks: [String: String] = [:]) {
        self.blocks = blocks
        self.docLinks = docLinks
        self.imageLinks = imageLinks
    }

    init(text: String, docLinks: [String: String] = [:], imageLinks: [String: String] = [:]) {
        blocks = Markdown.blocks(text)
        self.docLinks = docLinks
        self.imageLinks = imageLinks
    }

    private func inline(_ text: String) -> AttributedString {
        Markdown.inline(text, docLinks: docLinks, imageLinks: openImage == nil ? [:] : imageLinks)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                switch block {
                case .heading(let level, let text):
                    Text(inline(text))
                        .font(level <= 1 ? .title2.bold() : level == 2 ? .title3.bold() : .headline)
                case .paragraph(let text):
                    Text(inline(text))
                case .list(let items):
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text(item.marker)
                                    .font(.body)
                                    .monospacedDigit()
                                    .foregroundStyle(.primary)
                                Text(inline(item.text))
                            }
                            .padding(.leading, CGFloat(item.depth) * 16)
                        }
                    }
                case .quote(let text):
                    Text(inline(text))
                        .foregroundStyle(.secondary)
                        .padding(.leading, 12)
                        .overlay(alignment: .leading) {
                            Capsule().fill(.quaternary).frame(width: 3)
                        }
                case .code(let language, let text):
                    MarkdownCodeBlock(language: language, text: text)
                case .mermaid(let source):
                    MermaidBlock(source: source)
                case .table(let header, let align, let rows):
                    MarkdownTable(header: header, align: align, rows: rows, inline: inline)
                }
            }
        }
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, alignment: .leading)
        .environment(\.openURL, OpenURLAction { url in
            if let target = MarkdownDocLink.target(url) {
                openDoc?(target)
            } else if let target = MarkdownImageLink.target(url) {
                openImage?(target)
            } else if MarkdownLinkPolicy.allows(url) {
                openURL(url)
            }
            return .handled
        })
    }
}

private struct MarkdownTable: View {
    let header: [String]
    let align: [MarkdownAlign]
    let rows: [[String]]
    let inline: (String) -> AttributedString

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .topLeading, horizontalSpacing: 16, verticalSpacing: 8) {
                GridRow {
                    ForEach(Array(header.enumerated()), id: \.offset) { column, text in
                        cell(text, column)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(.secondary)
                            .gridColumnAlignment(alignment(column))
                    }
                }
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    Divider()
                        .gridCellUnsizedAxes(.horizontal)
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { column, text in
                            cell(text, column)
                        }
                    }
                }
            }
            .monospacedDigit()
        }
    }

    private func cell(_ text: String, _ column: Int) -> some View {
        CappedWidth(cap: 240) {
            Text(inline(text))
                .multilineTextAlignment(alignment(column) == .trailing ? .trailing : alignment(column) == .center ? .center : .leading)
        }
    }

    private func alignment(_ column: Int) -> HorizontalAlignment {
        switch align.indices.contains(column) ? align[column] : .none {
        case .center: .center
        case .right: .trailing
        case .none, .left: .leading
        }
    }
}

private struct CappedWidth: Layout {
    let cap: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard let child = subviews.first else { return .zero }
        let width = min(child.sizeThatFits(.unspecified).width, proposal.width ?? cap, cap)
        return child.sizeThatFits(ProposedViewSize(width: width, height: nil))
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        subviews.first?.place(at: bounds.origin, proposal: ProposedViewSize(width: bounds.width, height: nil))
    }
}

struct MermaidBlock: View {
    static let width: CGFloat = 800
    static let maxHeight: CGFloat = 420

    let source: String

    @State private var image: CGImage?
    @State private var enlarged = false
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = DocumentTheme.system(dark: scheme == .dark)
        let shown = MermaidRenderer.shared.cached(source: source, theme: theme, width: Self.width) ?? image
        Group {
            if let shown {
                Button { enlarged = true } label: {
                    diagram(shown)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Diagram")
                .accessibilityHint("Opens it full screen")
            } else {
                CodeBlock(text: source)
            }
        }
        .task(id: theme) {
            image = await MermaidRenderer.shared.image(source: source, theme: theme, width: Self.width)
        }
        .fullScreenCover(isPresented: $enlarged) {
            MermaidScreen(source: source)
        }
    }

    private func diagram(_ image: CGImage) -> some View {
        let natural = CGSize(width: CGFloat(image.width) / 2, height: CGFloat(image.height) / 2)
        let width = natural.width * min(1, Self.maxHeight / max(natural.height, 1))
        return Image(decorative: image, scale: 2)
            .resizable()
            .aspectRatio(contentMode: .fit)
            .frame(maxWidth: width)
            .padding(12)
            .frame(maxWidth: .infinity)
            .background(Color(.systemBackground), in: .rect(cornerRadius: 16))
            .contentShape(.rect(cornerRadius: 16))
    }
}

private struct MermaidScreen: View {
    let source: String

    @State private var image: CGImage?
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let theme = DocumentTheme.system(dark: scheme == .dark)
        ZoomScreen(title: "Diagram") {
            GeometryReader { proxy in
                Group {
                    if let image {
                        ZoomableImage(image: image, scale: 4)
                    } else {
                        ProgressView()
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                    }
                }
                .task(id: theme) {
                    image = await MermaidRenderer.shared.image(source: source, theme: theme, width: max(proxy.size.width, MermaidBlock.width), scale: 4)
                }
            }
        }
    }
}

struct ThoughtView: View {
    let text: String
    let title: String
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                Motion.disclose { expanded.toggle() }
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
            VStack(alignment: .leading, spacing: 4) {
                Button {
                    Motion.disclose { expanded.toggle() }
                } label: {
                    HStack(spacing: 6) {
                        label
                        Spacer(minLength: 6)
                        Image(systemName: "chevron.right")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.tertiary)
                            .rotationEffect(.degrees(expanded ? 90 : 0))
                    }
                    .frame(minHeight: 28)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityValue(expanded ? "Expanded" : "Collapsed")
                if expanded {
                    Text(detail)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
    }
}

struct ToolRow: View {
    let tool: ToolCall
    @State private var expanded = false

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
    }
}

struct SubagentStop {
    let enabled: Bool
    let run: @MainActor (String) async -> Bool
}

extension EnvironmentValues {
    @Entry var subagentStop: SubagentStop?
    @Entry var openDoc: (@MainActor (String) -> Void)?
}

struct ToolLine: View {
    let tool: ToolCall
    @Binding var expanded: Bool
    var link = false
    @Environment(\.subagentStop) private var stop
    @Environment(\.openDoc) private var openDoc
    @State private var stopping = false
    @State private var confirmingStop = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button {
                guard hasDetails else { return }
                Motion.disclose { expanded.toggle() }
            } label: {
                HStack(spacing: 10) {
                    Image(systemName: tool.kind.symbol)
                        .foregroundStyle(.secondary)
                        .frame(width: 20)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(tool.displayTitle)
                            .font(tool.kind == .execute ? .callout.monospaced() : .callout)
                            .lineLimit(1)
                            .foregroundStyle(tool.status == .failed ? Color.danger : .primary)
                        if tool.stoppable {
                            TimelineView(.periodic(from: .now, by: 30)) { timeline in
                                Text(stopping ? "Stopping…" : tool.runningLine(now: timeline.date))
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                            }
                        }
                    }
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
            .allowsHitTesting(hasDetails || tool.stoppable)
            .contextMenu {
                if tool.stoppable, let stop {
                    Button("Stop Subagent…", systemImage: "stop.circle", role: .destructive) { confirmingStop = true }
                        .disabled(!stop.enabled || stopping)
                }
            }
            .confirmationDialog("Stop this subagent?", isPresented: $confirmingStop, titleVisibility: .visible) {
                Button("Stop Subagent", role: .destructive) {
                    guard let stop else { return }
                    stopping = true
                    Task { stopping = await stop.run(tool.id) }
                }
            } message: {
                Text("Only this subagent stops. The session keeps running.")
            }
            if let doc = tool.doc, let openDoc {
                if link {
                    Button("Open \(URL(filePath: doc).lastPathComponent)") { openDoc(doc) }
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.tint)
                        .lineLimit(1)
                        .buttonStyle(.plain)
                        .padding(.leading, 30)
                } else {
                    PlainCapsule(title: "Open", tint: .primary) { openDoc(doc) }
                        .accessibilityLabel("Open \(URL(filePath: doc).lastPathComponent)")
                }
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
    @State private var expanded = false
    @State private var open: Set<String> = []
    @Environment(AppNavigation.self) private var navigation

    private static let listed = 3

    var body: some View {
        if navigation.layout == .compact {
            folded
        } else {
            list
        }
    }

    private var list: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(group.title)
                .font(.footnote)
                .foregroundStyle(group.failed ? Color.danger : .secondary)
            ForEach(expanded ? group.calls : Array(group.calls.prefix(Self.listed)), id: \.id) { call in
                line(call, link: true)
            }
            if group.calls.count > Self.listed {
                Button(expanded ? "Show fewer" : "Show all \(group.calls.count)") {
                    Motion.disclose { expanded.toggle() }
                }
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.tint)
                .buttonStyle(.plain)
                .frame(minHeight: 32)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.surface1, in: .rect(cornerRadius: 20))
        .overlay {
            if group.failed {
                RoundedRectangle(cornerRadius: 20).strokeBorder(Color.danger.opacity(0.5), lineWidth: 1)
            }
        }
    }

    private func line(_ call: ToolCall, link: Bool = false) -> some View {
        ToolLine(tool: call, expanded: Binding(
            get: { open.contains(call.id) },
            set: { if $0 { open.insert(call.id) } else { open.remove(call.id) } }
        ), link: link)
    }

    private var folded: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button {
                Motion.disclose { expanded.toggle() }
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
                    line(call)
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

private struct MarkdownCodeBlock: View {
    let language: String
    let text: String

    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 0) {
                Text(language)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                Spacer(minLength: 8)
                Button(action: copy) {
                    Image(systemName: copied ? "checkmark" : "doc.on.doc")
                        .font(.footnote)
                        .foregroundStyle(copied ? Color.successInk : Color.secondary)
                        .frame(width: 44, height: 44)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(copied ? "Copied" : "Copy")
            }
            .padding(.leading, 14)
            ScrollView(.horizontal, showsIndicators: false) {
                Text(text)
                    .font(.callout.monospaced())
                    .padding([.horizontal, .bottom], 14)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.surface1, in: .rect(cornerRadius: 16))
        .sensoryFeedback(.success, trigger: copied) { _, now in now }
    }

    private func copy() {
        UIPasteboard.general.string = text
        copied = true
        Task {
            try? await Task.sleep(for: .seconds(1.5))
            copied = false
        }
    }
}

struct DiffView: View {
    let fileName: String
    let lines: [DiffLine]
    var limit = 400

    init(diff: ToolCall.FileDiff, limit: Int = 400) {
        self.init(fileName: diff.fileName, lines: diff.lines, limit: limit)
    }

    init(fileName: String, lines: [DiffLine], limit: Int = 400) {
        self.fileName = fileName
        self.lines = lines
        self.limit = limit
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Label(fileName, systemImage: "doc")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
            ScrollView(.horizontal, showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(lines.prefix(limit).enumerated()), id: \.offset) { _, line in
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
    @State private var showingDone = false

    var body: some View {
        let fold = PlanFold(items.map(\.status))
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Plan · \(fold.done) of \(fold.total)")
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                Spacer()
                if fold.folds {
                    Button(showingDone ? "Hide done" : "Show done") {
                        Motion.disclose { showingDone.toggle() }
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(.tint)
                    .frame(minHeight: 44)
                    .padding(.vertical, -12)
                    .contentShape(.rect)
                }
            }
            .font(.subheadline)
            ForEach(Array(items.enumerated()).filter { fold.shows($0.element.status, showingDone: showingDone) }, id: \.offset) { _, item in
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
    var square = false

    var body: some View {
        Image(systemName: symbol)
            .font(.footnote.weight(.bold))
            .foregroundStyle(Color.onAccent)
            .frame(width: 28, height: 28)
            .background(Color.warning, in: square ? AnyShape(.rect(cornerRadius: 7)) : AnyShape(.circle))
    }
}

struct PermissionCard: View {
    let request: PermissionRequest
    let more: Int
    let agentName: String
    let place: String
    let blocked: String?
    var askAlways: AlwaysAsk?
    let choose: @MainActor (String) async throws -> Void
    @State private var chosen: String?
    @State private var failure: String?
    @State private var pendingAlways: AlwaysConfirm?
    @State private var showingAll = false
    @State private var armed = false
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        DockCard {
            VStack(alignment: .leading, spacing: 12) {
                if navigation.layout == .compact || subject.contains("\n") {
                    stackedHeader
                } else {
                    ViewThatFits(in: .horizontal) {
                        inlineHeader
                        stackedHeader
                    }
                }
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
                Group {
                    if navigation.layout == .compact {
                        stackedButtons
                    } else {
                        ViewThatFits(in: .horizontal) {
                            buttonRow
                            stackedButtons
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
        .onChange(of: askAlways) { _, ask in
            if let ask, answerable { pendingAlways = request.alwaysConfirm(ask.option) }
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
        .sheet(item: $pendingAlways) { option in
            AlwaysAllowSheet(request: request, option: option, agentName: agentName, place: place) {
                pendingAlways = nil
                pick(option.optionID)
            }
        }
    }

    private var enabled: Bool {
        blocked == nil
    }

    private var answerable: Bool {
        enabled && armed && chosen == nil
    }

    private var title: some View {
        Text(request.card?.title ?? request.question)
            .font(.title3.weight(.semibold))
    }

    private var stackedHeader: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 12) {
                DockBadge(symbol: "hand.raised.fill")
                title
                    .lineLimit(2)
                Spacer(minLength: 0)
                menuButton
            }
            CodeBlock(text: subject, lines: PermissionRequest.dockCommandLines)
        }
    }

    private var inlineHeader: some View {
        HStack(spacing: 12) {
            DockBadge(symbol: "hand.raised.fill")
            title
                .lineLimit(1)
                .fixedSize()
            Text(subject)
                .font(.callout.monospaced())
                .lineLimit(1)
                .fixedSize()
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(.systemGroupedBackground), in: .rect(cornerRadius: 16))
            menuButton
        }
    }

    @ViewBuilder
    private var menuButton: some View {
        if !menu.isEmpty {
            Menu {
                ForEach(menu, id: \.id) { option in
                    Button(option.label, systemImage: option.kind.rejects ? "hand.raised.slash" : "checkmark", role: option.kind.rejects ? .destructive : nil) { pick(option.id) }
                }
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

    private var stackedButtons: some View {
        VStack(spacing: 10) {
            if let first = buttons.first {
                Button { tap(first) } label: {
                    Text(first.label)
                        .font(.headline)
                        .frame(maxWidth: .infinity, minHeight: 50)
                        .foregroundStyle(Color.onAccent)
                        .background(Color.accentColor, in: .capsule)
                        .contentShape(.capsule)
                }
                .buttonStyle(.plain)
            }
            HStack(spacing: 10) {
                ForEach(buttons.dropFirst(), id: \.id) { option in
                    PlainCapsule(title: option.label, tint: option.kind.rejects ? Color.danger : .primary) { tap(option) }
                }
            }
        }
    }

    private var buttonRow: some View {
        let rejectAt = buttons.indices.dropFirst().first { buttons[$0].kind.rejects }
        return HStack(spacing: 10) {
            ForEach(Array(buttons.enumerated()), id: \.element.id) { index, option in
                if index == rejectAt {
                    Spacer(minLength: 0)
                }
                Button { tap(option) } label: {
                    Text(option.label)
                        .font(.headline)
                        .lineLimit(1)
                        .padding(.horizontal, 22)
                        .frame(minHeight: index == 0 ? 50 : 44)
                        .foregroundStyle(index == 0 ? Color.onAccent : option.kind.rejects ? Color.danger : .primary)
                        .background(index == 0 ? Color.accentColor : Color.surface2, in: .capsule)
                        .contentShape(.capsule)
                }
                .buttonStyle(.plain)
                .fixedSize()
            }
        }
    }

    private var subject: String {
        request.card?.command ?? request.subject
    }

    private var buttons: [PermissionCardContent.Option] {
        request.card?.buttons ?? request.fallbackButtons
    }

    private var menu: [PermissionCardContent.Option] {
        request.card?.menu ?? request.fallbackMenu
    }

    private func tap(_ option: PermissionCardContent.Option) {
        if option.kind == .allowAlways { pendingAlways = request.alwaysConfirm(option.id) } else { pick(option.id) }
    }

    private var caption: String? {
        if let blocked { return blocked }
        let said = request.card.map(\.caption) ?? (request.toolCall.intent ?? (request.toolCall.displayTitle != request.subject ? request.toolCall.displayTitle : nil)).map { "\(agentName) says: \($0)" }
        let intent = said
        let rest = more > 0 ? (more == 1 ? "1 more request waiting" : "\(more) more requests waiting") : nil
        let parts = [intent, rest].compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    private func pick(_ option: String) {
        chosen = option
        failure = nil
        Task {
            do {
                try await choose(option)
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
    let option: AlwaysConfirm
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
            if !option.guessed, let rule = option.rule {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Rule it stores")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    CodeBlock(text: rule, lines: 4)
                }
            } else {
                Text("\(agentName)'s option: “\(option.label)”")
                    .font(.subheadline)
                    .fixedSize(horizontal: false, vertical: true)
                if option.guessed, let rule = option.rule {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Likely rule, read from that label")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        CodeBlock(text: rule, lines: 4)
                    }
                }
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
        if let workspace = option.workspace {
            let name = workspace.replacingOccurrences(of: "-", with: "-\u{2060}")
            return Markdown.inline("\(agentName) will \(what) in **\(name)** without asking, on this Mac, until you remove the rule.")
        }
        if let scope = option.scope {
            return Markdown.inline("\(agentName) will \(what) without asking, \(scope), until you remove the rule.")
        }
        if option.forSession {
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
    @Binding var draft: ElicitationDraft
    let send: @MainActor (String, [Elicitation.Answer]) async throws -> Void
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
                let selected = draft.picks[field.key, default: []].contains(option.value)
                Button {
                    if single {
                        submit("accept", [Elicitation.Answer(key: field.key, value: option.value)])
                    } else {
                        draft.toggle(field, option)
                    }
                } label: {
                    optionRow(option, symbol: single ? nil : selected ? "checkmark.circle.fill" : "circle")
                }
                .buttonStyle(.plain)
                .disabled(!answerable)
            }
        case "boolean":
            Toggle(field.label, isOn: Binding(get: { draft.toggles[field.key] ?? false }, set: { draft.toggles[field.key] = $0 }))
        default:
            TextField(field.type == "text" ? (field.description.isEmpty ? field.label : field.description) : field.label, text: Binding(get: { draft.texts[field.key] ?? "" }, set: { draft.texts[field.key] = $0 }), axis: .vertical)
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
                    guard let link = question.request.url, let url = URL(string: link), MarkdownLinkPolicy.allows(url) else { return }
                    openURL(url)
                    submit("accept", [])
                }
                PlainCapsule(title: "Decline", tint: Color.danger) { submit("decline", []) }
            }
        } else {
            HStack(spacing: 10) {
                if question.singleChoice == nil || question.request.fields.count > 1 {
                    primary("Send") { submit("accept", draft.answers(question.request.fields)) }
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
