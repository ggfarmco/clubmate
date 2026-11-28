import SwiftUI

import CompanionKit

struct TranscriptNode: View {
    let node: SDUINode
    let context: SDUIContext
    @Environment(\.sduiInScroll) private var inScroll

    private enum Part: Identifiable {
        case node(SDUIChild)
        case tools([SDUIChild])

        var id: String {
            switch self {
            case .node(let child): child.id
            case .tools(let cards): "tools:" + (cards.first?.id ?? "")
            }
        }

        func contains(_ id: String) -> Bool {
            switch self {
            case .node(let child): child.id == id
            case .tools(let cards): cards.contains { $0.id == id }
            }
        }
    }

    private struct Anchor: Equatable {
        let id: String
        let rev: Int
    }

    @MainActor private static var scrolled: [String: Anchor] = [:]

    private var anchor: Anchor? {
        guard let id = node.string("anchor") else { return nil }
        return Anchor(id: id, rev: node.props["anchorRev"]?.numberValue.flatMap { Int(exactly: $0) } ?? 0)
    }

    private static func folds(_ node: SDUINode) -> Bool {
        node.type == "toolCard" && node.string("status") != "failed" && node.bool("standalone") != true
    }

    private var parts: [Part] {
        var parts: [Part] = []
        var run: [SDUIChild] = []
        func close() {
            if run.count == 1 { parts.append(.node(run[0])) } else if run.count > 1 { parts.append(.tools(run)) }
            run = []
        }
        for child in node.identifiedChildren {
            if Self.folds(child.node) {
                run.append(child)
            } else {
                close()
                parts.append(.node(child))
            }
        }
        close()
        return parts
    }

    var body: some View {
        if inScroll {
            column
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    column.padding(.vertical, 12)
                }
                .defaultScrollAnchor(.bottom)
                .scrollDismissesKeyboard(.interactively)
                .environment(\.sduiInScroll, true)
                .onChange(of: anchor, initial: true) { _, anchor in
                    scroll(to: anchor, with: proxy)
                }
            }
        }
    }

    private func scroll(to anchor: Anchor?, with proxy: ScrollViewProxy) {
        guard let key = node.id, let anchor, Self.scrolled[key] != anchor else { return }
        Self.scrolled[key] = anchor
        guard let part = parts.first(where: { $0.contains(anchor.id) }) else { return }
        Task { proxy.scrollTo(part.id, anchor: .top) }
    }

    private var column: some View {
        VStack(alignment: .leading, spacing: 14) {
            ForEach(parts) { part in
                Group {
                    switch part {
                    case .node(let child): SDUINodeView(node: child.node, context: context)
                    case .tools(let cards): ToolCardGroup(cards: cards, context: context)
                    }
                }
                .id(part.id)
            }
        }
    }
}

struct MessageNode: View {
    let node: SDUINode
    let context: SDUIContext
    @Environment(\.sduiInset) private var inset

    private var attachments: [String] {
        (node.props["attachments"]?.arrayValue ?? []).compactMap { value in
            guard let path = value["path"]?.stringValue else { return nil }
            guard value["kind"]?.stringValue == "selection", let start = value["startLine"]?.numberValue, let end = value["endLine"]?.numberValue else { return path }
            return "\(path) · lines \(Int(start))–\(Int(end))"
        }
    }

    private var thought: String {
        guard let seconds = node.props["seconds"]?.numberValue else { return "Thinking" }
        let value = max(Int(seconds), 1)
        return value < 60 ? "Thought for \(value) s" : "Thought for \(value / 60) min"
    }

    private var session: String {
        guard let id = node.id, let range = id.range(of: "/msg/") else { return "" }
        return String(id[..<range.lowerBound])
    }

    private var entry: String {
        guard let id = node.id, let range = id.range(of: "/msg/") else { return "" }
        return String(id[range.upperBound...])
    }

    private var gate: GateMessage? {
        guard MessageRole(node.string("role")) == .user, let value = node.props["gate"], let data = try? JSONEncoder().encode(value) else { return nil }
        return (try? JSONDecoder().decode(GateMessage.self, from: data)).flatMap { $0.groups.isEmpty ? nil : $0 }
    }

    var body: some View {
        let text = node.string("text") ?? ""
        let gate = self.gate
        Group {
            switch MessageRole(node.string("role")) {
            case .user:
                if let gate {
                    GateMessageBubble(message: gate)
                } else {
                    UserBubble(text: text, fromAgent: node.string("from")?.isEmpty == false, images: [], files: attachments, key: node.id)
                }
            case .thought: ThoughtView(key: node.id, text: text, title: thought, open: node.bool("open") == true)
            case .agent:
                if let to = node.props["handoff"]?.objectValue.map({ $0["to"]?.stringValue ?? "" }), let id = node.id {
                    HandoffRow(key: id, title: Handoff.title(to: to, text: text)) { agent(text) }
                } else {
                    agent(text)
                }
            }
        }
        .padding(.horizontal, inset)
    }

    private func agent(_ text: String) -> some View {
        AgentMessage(cards: node.docCards, entry: entry, images: node.images) { MarkdownView(text: text, docLinks: node.docLinks, imageLinks: node.imageLinks, taskLinks: node.taskLinks) }
            .environment(\.openDoc, Opener(key: node.id ?? "") { target in
                context.run { await $0.fire(node, .named("openDoc", .string(target))) }
            })
            .environment(\.imageSession, session)
    }
}

struct PlanNode: View {
    let node: SDUINode
    @Environment(\.sduiInset) private var inset

    var body: some View {
        PlanCard(items: (node.props["entries"]?.arrayValue ?? []).map(PlanItem.init), key: node.id)
            .padding(.horizontal, inset)
    }
}

struct ToolCardNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var expanded = false
    @Environment(\.sduiInset) private var inset

    var body: some View {
        ToolCardLine(node: node, context: context, expanded: $expanded)
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .background(Color.surface1, in: .rect(cornerRadius: 20))
            .overlay {
                if node.string("status") == "failed", node.string("statusLine") == nil {
                    RoundedRectangle(cornerRadius: 20).strokeBorder(Color.danger.opacity(0.5), lineWidth: 1)
                }
            }
            .padding(.horizontal, inset)
    }
}

struct ToolCardGroup: View {
    let cards: [SDUIChild]
    let context: SDUIContext
    @State private var expanded = false
    @State private var open: Set<String> = []
    @Environment(\.sduiInset) private var inset

    private var running: SDUINode? {
        cards.last { ToolStatus($0.node.string("status") ?? "pending").isOpen }?.node
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button {
                Motion.disclose { expanded.toggle() }
            } label: {
                HStack(spacing: 12) {
                    Image(systemName: "square.stack.3d.up")
                        .foregroundStyle(Color.captionInk)
                        .frame(width: 22)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("\(cards.count) actions")
                            .font(.callout)
                        if !expanded {
                            Text((running ?? cards.last?.node)?.string("title") ?? "")
                                .font(.footnote)
                                .foregroundStyle(Color.captionInk)
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                    }
                    Spacer(minLength: 6)
                    if let icon = running?.string("statusIcon") {
                        Image(systemName: icon).foregroundStyle(Color.captionInk)
                    } else if running != nil {
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
            if expanded {
                ForEach(cards) { card in
                    ToolCardLine(node: card.node, context: context, expanded: Binding(
                        get: { open.contains(card.id) },
                        set: { if $0 { open.insert(card.id) } else { open.remove(card.id) } }
                    ))
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background(Color.surface1, in: .rect(cornerRadius: 20))
        .padding(.horizontal, inset)
    }
}

struct ToolCardLine: View {
    let node: SDUINode
    let context: SDUIContext
    @Binding var expanded: Bool
    @Environment(\.horizontalSizeClass) private var sizeClass

    private struct Action: Identifiable {
        let action: String
        let label: String
        let icon: String?
        let value: JSONValue

        var id: String { action }
    }

    private var kind: ToolKind { ToolKind(node.string("kind")) }
    private var status: ToolStatus { ToolStatus(node.string("status") ?? "pending") }
    private var statusLine: String? { node.string("statusLine") }
    private var format: String? { node.props["body"]?["format"]?.stringValue }
    private var text: String { node.props["body"]?["text"]?.stringValue ?? "" }

    private var actions: [Action] {
        (node.props["actions"]?.arrayValue ?? []).compactMap { value in
            guard let action = value["action"]?.stringValue, let label = value["label"]?.stringValue else { return nil }
            return Action(action: action, label: label, icon: value["icon"]?.stringValue, value: value["value"] ?? .null)
        }
    }

    private var hasDetails: Bool {
        !text.isEmpty || !actions.isEmpty
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Button {
                guard hasDetails else { return }
                Motion.disclose { expanded.toggle() }
            } label: {
                HStack(spacing: 10) {
                    Image(systemName: kind.symbol)
                        .foregroundStyle(Color.captionInk)
                        .frame(width: 20)
                    Text(node.string("title") ?? "")
                        .font(kind == .execute ? .callout.monospaced() : .callout)
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .foregroundStyle(status == .failed && statusLine == nil ? Color.danger : status == .cancelled ? Color.captionInk : .primary)
                    Spacer(minLength: 6)
                    if format == "diff" {
                        ChangeCounts(changes: SDUIDiff.changes(text))
                    }
                    if statusLine != nil, sizeClass == .regular, let word = node.string("statusWord") {
                        HStack(spacing: 4) {
                            if let icon = node.string("statusIcon") { Image(systemName: icon) }
                            Text(word).lineLimit(1)
                        }
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .fixedSize()
                    } else if statusLine != nil {
                        if let icon = node.string("statusIcon") {
                            Image(systemName: icon).foregroundStyle(Color.captionInk)
                        }
                    } else {
                        ToolStatusIcon(status: status)
                    }
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
            if sizeClass != .regular, let line = statusLine {
                Text(line)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
                    .padding(.leading, 30)
            }
            if expanded {
                if let held = node.props["slotHeld"]?.objectValue, let title = held["title"]?.stringValue {
                    Text((["Held by " + title] + [held["rest"]?.stringValue].compactMap { $0 }).joined(separator: " · "))
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                }
                if let wait = node.string("slotWait") {
                    Text(wait)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                }
                if format == "diff" {
                    SDUIDiff(text: text)
                } else if !text.isEmpty {
                    CodeBlock(text: text)
                }
                if node.bool("truncated") == true {
                    Text("Output truncated")
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                }
                if !actions.isEmpty {
                    FlowLayout(spacing: 8) {
                        ForEach(actions) { action in
                            Button {
                                context.run { await $0.fire(node, .named(action.action, action.value)) }
                            } label: {
                                Label(action.label, systemImage: action.icon ?? "arrow.up.right")
                                    .font(.footnote.weight(.semibold))
                                    .padding(.horizontal, 14)
                                    .frame(minHeight: 36)
                                    .background(Color.surface2, in: .capsule)
                                    .frame(minHeight: 44)
                                    .contentShape(.rect)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
            }
        }
    }
}

struct PermissionCardNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var chosen: String?
    @State private var always: Option?
    @Environment(\.sduiInset) private var inset

    struct Option: Identifiable {
        let id: String
        let label: String
        let kind: PermissionKind
        let menu: Bool
        let rule: String?
        let scope: String?
    }

    private var options: [Option] {
        (node.props["options"]?.arrayValue ?? []).compactMap { value in
            guard let id = value["id"]?.stringValue, let label = value["label"]?.stringValue else { return nil }
            return Option(id: id, label: label, kind: PermissionKind(value["kind"]?.stringValue), menu: value["menu"]?.boolValue == true, rule: value["rule"]?.stringValue, scope: value["scope"]?.stringValue)
        }
    }

    private var outcome: String? {
        chosen ?? node.string("outcome").flatMap { $0.isEmpty ? nil : $0 }
    }

    var body: some View {
        let options = options
        let buttons = options.filter { !$0.menu }
        let menu = options.filter(\.menu)
        DockCard {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 12) {
                    DockBadge(symbol: "hand.raised.fill")
                    Text(node.string("title") ?? node.string("text") ?? "")
                        .font(.title3.weight(.semibold))
                        .lineLimit(3)
                    Spacer(minLength: 0)
                    if !menu.isEmpty, outcome == nil {
                        Menu {
                            ForEach(menu) { option in
                                Button(option.label, systemImage: option.kind.rejects ? "hand.raised.slash" : "checkmark", role: option.kind.rejects ? .destructive : nil) { tap(option) }
                            }
                        } label: {
                            Image(systemName: "ellipsis")
                                .font(.body.weight(.semibold))
                                .foregroundStyle(Color.captionInk)
                                .frame(width: 44, height: 44)
                                .contentShape(.rect)
                        }
                        .tint(Color.captionInk)
                        .accessibilityLabel("More Answers")
                    }
                }
                if node.string("title") != nil, let command = node.string("command"), !command.isEmpty {
                    CodeBlock(text: command, lines: 6)
                }
                if let caption = node.string("caption"), !caption.isEmpty {
                    Text(caption)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(2)
                }
                if let outcome {
                    Label(outcome == "cancelled" ? "Cancelled" : options.first { $0.id == outcome }?.label ?? outcome, systemImage: outcome == "cancelled" ? "xmark" : "checkmark")
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                } else {
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
                            ForEach(buttons.dropFirst()) { option in
                                PlainCapsule(title: option.label, tint: option.kind.rejects ? Color.danger : .primary) { tap(option) }
                            }
                        }
                    }
                    if let note = node.string("note"), !note.isEmpty {
                        PermissionNote(text: note)
                    }
                }
            }
        }
        .padding(.horizontal, inset)
        .alert("Always allow?", isPresented: Binding(get: { always != nil }, set: { if !$0 { always = nil } }), presenting: always) { option in
            Button(option.label) { pick(option) }
            Button("Cancel", role: .cancel) {}
        } message: { option in
            Text([option.rule ?? option.label, option.scope].compactMap { $0 }.joined(separator: "\n"))
        }
    }

    private func tap(_ option: Option) {
        if option.kind == .allowAlways { always = option } else { pick(option) }
    }

    private func pick(_ option: Option) {
        chosen = option.id
        let request = node.string("request") ?? ""
        context.run { await $0.fire(node, .permission(request: request, option: option.id)) }
    }
}

struct ComposerNode: View {
    let node: SDUINode
    let context: SDUIContext
    @State private var text = ""
    @Environment(\.sduiInset) private var inset

    private var empty: Bool { text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
    private var running: Bool { node.bool("running") == true }

    private var chips: [ContextChip] {
        (node.props["attachments"]?.arrayValue ?? []).compactMap { value in
            guard value["context"]?.boolValue == true, let kind = value["kind"]?.stringValue, let label = value["path"]?.stringValue else { return nil }
            return ContextChip(id: value["id"]?.stringValue, kind: kind, label: kind == "file" ? (label as NSString).lastPathComponent : label, removable: value["removable"]?.boolValue == true)
        }
    }

    private var contextLabel: String? { node.string("context").flatMap { $0.isEmpty ? nil : $0 } }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !chips.isEmpty || contextLabel != nil {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(chips, id: \.key) { chip in
                            ContextChipView(chip: chip, open: { context.run { await $0.fire(node, .named("context", chip.openValue)) } }, remove: { id in context.run { await $0.fire(node, .named("detach", .string(id))) } })
                        }
                        if let contextLabel {
                            ContextToggleView(label: contextLabel) { context.run { await $0.fire(node, .named("context", .null)) } }
                        }
                    }
                }
            }
            HStack(alignment: .bottom, spacing: 10) {
                TextField(node.string("placeholder") ?? "Message", text: $text, axis: .vertical)
                    .lineLimit(1...6)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 11)
                    .background(Color.surface2, in: .rect(cornerRadius: 22))
                Button(action: send) {
                    Image(systemName: "arrow.up")
                        .font(.body.weight(.bold))
                        .foregroundStyle(empty || node.bool("docked") == true ? Color.captionInk : Color.onAccent)
                        .frame(width: 44, height: 44)
                        .background(empty || node.bool("docked") == true ? Color.surface2 : Color.accentColor, in: .circle)
                }
                .buttonStyle(.plain)
                .disabled(empty)
                .accessibilityLabel(running && node.bool("steers") != true ? "Queue" : "Send")
            }
            if let suggestion = node.string("suggestion"), !suggestion.isEmpty, text.isEmpty {
                SuggestedPrompt(text: suggestion) { text = suggestion }
            }
            if running, node.bool("steers") != true {
                Text("Sends after the current turn")
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
            }
        }
        .padding(.horizontal, inset)
    }

    private func send() {
        let value = text
        text = ""
        context.run { await $0.fire(node, .named("send", .object(["text": .string(value)]))) }
    }
}

struct ContextChip {
    let id: String?
    let kind: String
    let label: String
    let removable: Bool

    var key: String { id ?? kind + label }
    var openValue: JSONValue { id.map { .string($0) } ?? .null }
    var symbol: String {
        switch kind {
        case "review": "checklist"
        case "page": "book"
        default: "doc.text"
        }
    }
}

struct ContextToggleView: View {
    let label: String
    let toggle: () -> Void

    var body: some View {
        Button(action: toggle) {
            Text(label)
                .font(.footnote)
                .foregroundStyle(Color.captionInk)
                .padding(.horizontal, 12)
                .frame(minHeight: 32)
                .overlay(Capsule().strokeBorder(Color.captionInk.opacity(0.5), style: StrokeStyle(lineWidth: 1, dash: [3, 2])))
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("composer.context")
    }
}

struct ContextChipView: View {
    let chip: ContextChip
    let open: () -> Void
    let remove: (String) -> Void

    var body: some View {
        HStack(spacing: 6) {
            Button(action: open) {
                Label(chip.label, systemImage: chip.symbol)
                    .font(.footnote)
                    .lineLimit(1)
                    .foregroundStyle(Color.captionInk)
                    .padding(.leading, 12)
                    .padding(.trailing, chip.removable ? 0 : 12)
                    .frame(minHeight: 32)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            if chip.removable, let id = chip.id {
                Button { remove(id) } label: {
                    Image(systemName: "xmark")
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(Color.captionInk)
                        .frame(width: 32, height: 32)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Leave out \(chip.label)")
            }
        }
        .background(Color.surface2, in: .capsule)
        .accessibilityIdentifier("composer.chip.\(chip.kind)")
    }
}

struct SuggestedPrompt: View {
    let text: String
    let use: () -> Void

    var body: some View {
        Button(action: use) {
            Text("Try \"\(text)\"")
                .font(.footnote)
                .foregroundStyle(Color.captionInk)
                .lineLimit(2)
                .multilineTextAlignment(.leading)
                .frame(minHeight: 44, alignment: .leading)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .padding(.leading, 4)
        .accessibilityHint("Puts the prompt in the message")
    }
}
