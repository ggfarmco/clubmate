import SwiftUI

import ClubmateKit

struct MessageNodeView: View, Equatable {
    let node: SDUINode

    nonisolated static func == (lhs: MessageNodeView, rhs: MessageNodeView) -> Bool {
        lhs.node == rhs.node
    }

    @Environment(\.theme) private var theme
    @Environment(\.sduiDispatch) private var dispatch

    var body: some View {
        let text = node.string("text") ?? ""
        switch node.messageRole {
        case .user:
            if let heard = node.string("heard"), let id = node.id {
                VoicePromptView(id: id, heard: heard, text: text)
            } else {
                userMessage(text)
            }
        case .thought:
            ThoughtView(text: text, seconds: node.props["seconds"]?.numberValue.map(Int.init))
        case .agent:
            let open = { [node, dispatch] (target: String) in dispatch(node, .named("openDoc", .string(target))) }
            let message = node.id ?? ""
            let images = node.images
            let openImage = { (path: String) in ImageViewer.shared.open(message: message, path: path) }
            VStack(alignment: .leading, spacing: theme.spacing.l) {
                MarkdownText(text: text, links: ProseLinks(docs: node.docLinks, images: node.imageLinks), openDoc: open, openImage: openImage)
                    .equatable()
                    .frame(maxWidth: .infinity, alignment: .leading)
                let cards = node.docCards
                if !cards.isEmpty {
                    MessageDocCards(id: (node.id ?? cards[0].path) + "/docs", groups: DocCardGroups(cards), open: open)
                }
                if !images.isEmpty {
                    MessageImages(message: message, images: images)
                }
            }
            .sheet(item: ImageViewer.shared.binding(message: message)) { shown in
                ImageViewerSheet(path: shown.path, rel: images.first { $0.path == shown.path }?.rel, size: NSApp.keyWindow?.contentLayoutRect.size)
            }
        }
    }

    private func userMessage(_ text: String) -> some View {
        HStack(spacing: 0) {
            Spacer(minLength: theme.gutter * 8)
            VStack(alignment: .leading, spacing: theme.spacing.s) {
                if let from = node.string("from") {
                    Text("from \(from)")
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
                UserMessageText(id: node.id, text: text)
                AttachmentLabels(attachments: node.attachments())
            }
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, theme.gutter + theme.spacing.s)
            .padding(.vertical, theme.gutter)
            .background(UnevenRoundedRectangle(topLeadingRadius: theme.radius.l, bottomLeadingRadius: theme.radius.l, bottomTrailingRadius: theme.radius.s, topTrailingRadius: theme.radius.l).fill(theme.surface1))
        }
    }
}

private struct VoicePromptView: View {
    let id: String
    let heard: String
    let text: String

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        let expanded = MessageExpansion.shared.expanded.contains(id)
        HStack(spacing: 0) {
            Spacer(minLength: theme.gutter * 8)
            VStack(alignment: .trailing, spacing: theme.spacing.s) {
                Button { MessageExpansion.shared.toggle(id) } label: {
                    HStack(spacing: theme.spacing.s) {
                        DisclosureChevron(expanded: expanded)
                            .foregroundStyle(theme.muted)
                        Text("🎙 \(heard)")
                            .foregroundStyle(hovering ? theme.foreground : theme.text2)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                    .padding(.horizontal, theme.gutter + theme.spacing.s)
                    .padding(.vertical, theme.spacing.m)
                    .background(Capsule().fill(theme.surface1))
                    .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .onHover { hovering = $0 }
                .help(heard)
                .accessibilityLabel(heard)
                .accessibilityValue(expanded ? "Expanded" : "Collapsed")
                if expanded {
                    CappedScroll {
                        Text(Self.pretty(text))
                            .font(theme.mono)
                            .textSelection(.enabled)
                            .fixedSize(horizontal: false, vertical: true)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(theme.gutter)
                    }
                    .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.surface1))
                }
            }
        }
    }

    private static func pretty(_ text: String) -> String {
        guard let object = try? JSONSerialization.jsonObject(with: Data(text.utf8)),
              let data = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]),
              let pretty = String(data: data, encoding: .utf8)
        else { return text }
        return pretty
    }
}

@MainActor @Observable final class MessageExpansion {
    static let shared = MessageExpansion()

    private(set) var expanded: Set<String> = []

    func toggle(_ id: String) {
        Motion.disclose {
            if expanded.remove(id) == nil { expanded.insert(id) }
        }
    }

    func set(_ id: String, expanded shown: Bool) {
        if shown { expanded.insert(id) } else { expanded.remove(id) }
    }
}

private struct UserMessageText: View {
    let id: String?
    let text: String

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        if let id, let long = LongMessage(text) {
            let expanded = MessageExpansion.shared.expanded.contains(id)
            if expanded {
                CappedScroll {
                    MarkdownText(text: text).equatable()
                }
            } else {
                MarkdownText(text: long.preview).equatable()
            }
            Button { MessageExpansion.shared.toggle(id) } label: {
                Text(expanded ? "Show less" : long.lines > LongMessage.previewLines ? "Show all \(long.lines) lines" : "Show all")
                    .font(theme.uiSmall)
                    .foregroundStyle(hovering ? theme.foreground : theme.text2)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .onHover { hovering = $0 }
        } else {
            MarkdownText(text: text).equatable()
        }
    }
}

private struct AttachmentLabels: View {
    let attachments: [SDUIAttachment]

    @Environment(\.theme) private var theme

    var body: some View {
        ForEach(attachments) { attachment in
            Text(attachment.label)
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.label)
        }
    }
}

private struct ThoughtView: View {
    let text: String
    let seconds: Int?

    @State private var expanded = false
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            Button { Motion.disclose { expanded.toggle() } } label: {
                HStack(spacing: theme.spacing.s) {
                    DisclosureChevron(expanded: expanded)
                    Text(seconds.map(ShortAge.thought) ?? "Thinking")
                        .italic(seconds == nil)
                }
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if expanded {
                MarkdownText(text: text, muted: true)
                    .equatable()
                    .padding(.leading, theme.indent)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}
