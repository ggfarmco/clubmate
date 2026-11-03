import Charts
import SwiftUI

import ClubmateDocument
import CompanionKit

struct TextNode: View {
    let node: SDUINode
    @State private var expanded = false
    @Environment(\.sduiInset) private var inset

    private var text: String { node.string("text") ?? "" }

    private var lines: Int? {
        node.props["lines"]?.numberValue.flatMap { Int(exactly: $0) }.flatMap { $0 >= 2 ? min($0, 50) : nil }
    }

    private var clamped: Bool {
        guard let lines else { return false }
        return text.split(whereSeparator: \.isNewline).count > lines || text.count > lines * 45
    }

    private var font: Font {
        switch node.string("style") {
        case "muted": .subheadline
        case "title": .title3.weight(.semibold)
        case "mono": .callout.monospaced()
        default: .body
        }
    }

    private var color: Color {
        SDUITone.ink(node.string("tone")) ?? (node.string("style") == "muted" ? .secondary : .primary)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(text)
                .font(font)
                .foregroundStyle(color)
                .lineLimit(expanded ? nil : lines)
                .textSelection(.enabled)
            if clamped {
                Button(expanded ? "Show less" : "Show all") { Motion.disclose { expanded.toggle() } }
                    .font(.footnote.weight(.semibold))
                    .frame(minHeight: 32)
            }
        }
        .frame(maxWidth: inset > 0 ? .infinity : nil, alignment: .leading)
        .padding(.horizontal, inset)
    }
}

struct BadgeNode: View {
    let node: SDUINode

    var body: some View {
        BadgeView(text: node.string("text") ?? "", tone: node.string("tone"))
    }
}

struct BadgeView: View {
    let text: String
    let tone: String?

    var body: some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .monospacedDigit()
            .lineLimit(1)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .foregroundStyle(SDUITone.ink(tone) ?? .secondary)
            .background(SDUITone.fill(tone)?.opacity(0.15) ?? Color.surface2, in: .capsule)
            .fixedSize()
    }
}

struct IconNode: View {
    let node: SDUINode

    var body: some View {
        Image(systemName: node.string("name") ?? "questionmark")
            .foregroundStyle(SDUITone.ink(node.string("tone")) ?? .secondary)
    }
}

struct MarkdownNode: View {
    let node: SDUINode
    @Environment(\.sduiInset) private var inset

    var body: some View {
        MarkdownView(text: node.string("text") ?? "")
            .padding(.horizontal, inset)
    }
}

struct DiffNode: View {
    let node: SDUINode
    @Environment(\.sduiInset) private var inset

    var body: some View {
        SDUIDiff(text: node.string("text") ?? "")
            .padding(.horizontal, inset)
    }
}

struct SDUIDiff: View {
    let text: String
    var limit = 12
    @State private var full = false

    private var files: [DiffFile] { DiffFile.parse(text) }

    var body: some View {
        let files = files
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(files.enumerated()), id: \.offset) { _, file in
                lines(file, limit: limit)
            }
            if files.contains(where: { $0.lines.count > limit }) {
                Button("Full diff") { full = true }
                    .font(.footnote.weight(.semibold))
                    .frame(minHeight: 32)
            }
        }
        .sheet(isPresented: $full) {
            NavigationStack {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        ForEach(Array(files.enumerated()), id: \.offset) { _, file in
                            lines(file, limit: Int.max)
                        }
                    }
                    .padding()
                }
                .navigationTitle("Diff")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { full = false }
                    }
                }
            }
        }
    }

    private func lines(_ file: DiffFile, limit: Int) -> some View {
        DiffView(fileName: file.path ?? "Diff", lines: file.lines.map { DiffLine(text: $0.text, kind: $0.kind) }, limit: limit)
    }

    static func changes(_ text: String) -> (added: Int, removed: Int) {
        DiffFile.parse(text).reduce((0, 0)) { ($0.0 + $1.added, $0.1 + $1.removed) }
    }
}

struct DocumentNode: View {
    let node: SDUINode
    let context: SDUIContext
    @Environment(\.colorScheme) private var scheme
    @Environment(\.openURL) private var openURL
    @Environment(\.sduiInScroll) private var inScroll
    @Environment(\.sduiInset) private var inset

    private var fit: Bool { node.string("size") == "fit" }

    private var content: DocumentContent {
        DocumentContent(path: node.string("path") ?? "", html: node.string("html") ?? "", rev: Int(node.props["rev"]?.numberValue ?? 0), anchor: node.string("anchor"), kind: node.string("kind") == "artifact" ? .artifact : .markdown)
    }

    var body: some View {
        DocumentWebView(content: content, theme: .system(dark: scheme == .dark), size: fit ? .fit : .fill, onLink: follow)
            .frame(maxWidth: .infinity)
            .frame(height: !fit && inScroll ? 480 : nil)
            .frame(maxHeight: fit || inScroll ? nil : .infinity)
            .padding(.horizontal, fit ? inset : 0)
    }

    private func follow(_ link: DocumentLink) {
        switch link {
        case .doc(let path, let anchor):
            context.run { await $0.fire(node, .named("openDoc", .string(anchor.map { "\(path)#\($0)" } ?? path))) }
        case .file(let path):
            context.run { await $0.fire(node, .named("openFile", .string(path))) }
        case .external(let url):
            openURL(url)
        case .anchor, .ignore:
            break
        }
    }
}

struct SparklineNode: View {
    let node: SDUINode
    @Environment(\.sduiInset) private var inset

    var body: some View {
        if let spark = node.spark() {
            if let height = node.sparklineHeight() {
                SparkChart(spark: spark)
                    .frame(maxWidth: .infinity)
                    .frame(height: height)
                    .padding(.horizontal, inset)
            } else {
                SparkChart(spark: spark)
                    .frame(width: 60, height: 16)
            }
        }
    }
}

struct ProgressNode: View {
    let node: SDUINode
    @Environment(\.sduiInset) private var inset

    var body: some View {
        let total = node.props["total"]?.numberValue ?? 0
        let value = node.props["value"]?.numberValue ?? 0
        ProgressView(value: total > 0 ? min(max(value, 0), total) : 0, total: max(total, 1))
            .tint(SDUITone.fill(node.string("tone")) ?? (node.string("tone") == "neutral" ? .secondary : .accentColor))
            .padding(.horizontal, inset)
            .accessibilityValue("\(value.formatted()) of \(total.formatted())")
    }
}

struct SparkChart: View {
    let spark: SDUISpark
    var tint: Color = .accentColor

    var body: some View {
        Chart(Array(spark.values.enumerated()), id: \.offset) { index, value in
            LineMark(x: .value("Sample", index), y: .value("Value", min(max(value, 0), spark.max)))
                .interpolationMethod(.monotone)
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartLegend(.hidden)
        .chartYScale(domain: 0...spark.max)
        .foregroundStyle(tint)
        .accessibilityHidden(true)
    }
}

struct HeaderNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var confirming: SDUINode?
    @Environment(\.sduiInset) private var inset
    @Environment(\.sduiForm) private var form

    private var line: String {
        [node.string("agent"), node.string("branch"), node.string("mode")].compactMap { $0?.isEmpty == false ? $0 : nil }.joined(separator: " · ")
    }

    var body: some View {
        let split = node.headerOverflow()
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                if let icon = node.string("icon") {
                    Image(systemName: icon)
                        .font(.title3)
                        .foregroundStyle(SDUITone.ink(node.headerStatusTone) ?? .secondary)
                }
                title
                Spacer(minLength: 4)
                if !split.more.isEmpty {
                    Menu {
                        ForEach(split.more) { child in
                            Button(child.node.string("label") ?? "", systemImage: child.node.string("icon") ?? "") { press(child.node) }
                                .disabled(child.node.bool("disabled") == true)
                        }
                    } label: {
                        Image(systemName: "ellipsis")
                            .font(.body.weight(.semibold))
                            .frame(width: 44, height: 44)
                            .contentShape(.rect)
                    }
                    .tint(.secondary)
                    .accessibilityLabel("More Actions")
                }
            }
            if !line.isEmpty {
                Text(line)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            let badges = node.headerBadges()
            if !badges.isEmpty {
                FlowLayout(spacing: 6) {
                    ForEach(Array(badges.enumerated()), id: \.offset) { _, badge in
                        BadgeView(text: badge.text, tone: badge.tone)
                    }
                }
            }
            if let meta = node.string("meta"), !meta.isEmpty {
                Text(meta)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let error = node.string("error"), !error.isEmpty, form != nil {
                Text(error)
                    .font(.footnote)
                    .foregroundStyle(Color.dangerInk)
            }
            if let field = node.headerField {
                SDUINodeView(node: field.node, context: context)
            }
            let bar = split.shown + (node.headerStop.map { [$0] } ?? [])
            if !bar.isEmpty {
                FlowLayout(spacing: 8) {
                    ForEach(bar) { child in
                        SDUINodeView(node: child.node, context: context)
                    }
                }
            }
        }
        .environment(\.sduiInset, 0)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, inset)
        .alert(confirming?.confirmation() ?? "", isPresented: Binding(get: { confirming != nil }, set: { if !$0 { confirming = nil } }), presenting: confirming) { button in
            Button(button.confirmActionTitle(), role: button.string("variant") == "danger" ? .destructive : nil) { SDUIPress.run(button, context: context, form: form) }
            Button("Cancel", role: .cancel) {}
        }
    }

    @ViewBuilder
    private var title: some View {
        if let field = node.string("field"), !field.isEmpty, let form {
            HeaderTitleField(title: node.string("title") ?? "", field: field, form: form, error: node.string("error"))
        } else {
            Text(node.string("title") ?? "")
                .font(.title2.weight(.bold))
                .lineLimit(2)
        }
    }

    private func press(_ button: SDUINode) {
        if button.confirmation() != nil { confirming = button } else { SDUIPress.run(button, context: context, form: form) }
    }
}

private struct HeaderTitleField: View {
    let title: String
    let field: String
    let form: SDUIFormState
    let error: String?
    @State private var text = ""
    @FocusState private var focused: Bool

    var body: some View {
        TextField("Title", text: Binding(get: { text }, set: { value in
            text = value.split(whereSeparator: \.isNewline).joined(separator: " ")
            form.values[field] = .string(text)
            form.typed()
            if value.contains(where: \.isNewline) {
                focused = false
                if !form.autosave { form.submit() }
            }
        }), axis: .vertical)
            .font(.title2.weight(.bold))
            .lineLimit(1...3)
            .focused($focused)
            .submitLabel(.done)
            .onChange(of: focused) { _, now in if !now { form.flush() } }
            .onChange(of: title, initial: true) { _, value in
                if focused { return }
                text = value
                form.values[field] = .string(value)
            }
    }
}

struct EmptyNode: View {
    let node: SDUINode
    let context: SDUIContext

    var body: some View {
        let text = node.string("text") ?? ""
        let title = node.string("title")
        ContentUnavailableView {
            if let icon = node.string("icon") {
                Label(title ?? text, systemImage: icon)
            } else {
                Text(title ?? text)
            }
        } description: {
            VStack(spacing: 8) {
                if title != nil { Text(text) }
                if let hint = node.string("hint"), !hint.isEmpty {
                    Text(hint)
                        .font(.footnote.monospaced())
                        .foregroundStyle(.secondary)
                }
            }
        } actions: {
            SDUIChildren(node: node, context: context)
                .environment(\.sduiInset, 0)
        }
    }
}
