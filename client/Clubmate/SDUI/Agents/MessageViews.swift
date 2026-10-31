import SwiftUI

import ClubmateKit

struct MessageNodeView: View, Equatable {
    let node: SDUINode

    nonisolated static func == (lhs: MessageNodeView, rhs: MessageNodeView) -> Bool {
        lhs.node == rhs.node
    }

    @Environment(\.theme) private var theme

    var body: some View {
        let text = node.string("text") ?? ""
        switch node.messageRole {
        case .user:
            HStack(spacing: 0) {
                Spacer(minLength: theme.gutter * 8)
                VStack(alignment: .leading, spacing: theme.spacing.s) {
                    if let from = node.string("from") {
                        Text("from \(from)")
                            .font(theme.uiSmall)
                            .foregroundStyle(theme.muted)
                    }
                    MarkdownText(text: text).equatable()
                    AttachmentLabels(attachments: node.attachments())
                }
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, theme.gutter + theme.spacing.s)
                .padding(.vertical, theme.gutter)
                .background(UnevenRoundedRectangle(topLeadingRadius: 10, bottomLeadingRadius: 10, bottomTrailingRadius: 3, topTrailingRadius: 10).fill(theme.foreground.opacity(0.06)))
            }
        case .thought:
            ThoughtView(text: text)
        case .agent:
            MarkdownText(text: text)
                .equatable()
                .frame(maxWidth: .infinity, alignment: .leading)
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

    @State private var expanded = false
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.s) {
            Button { expanded.toggle() } label: {
                HStack(spacing: theme.spacing.s) {
                    Image(systemName: expanded ? "chevron.down" : "chevron.right")
                        .font(theme.glyph)
                    Text("Thinking")
                        .italic()
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
