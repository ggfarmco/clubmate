import AppKit
import SwiftUI

import ClubmateKit

extension Theme {
    func inlineMarkdown(_ text: String) -> AttributedString {
        var content = Markdown.inline(text)
        for run in content.runs where run.inlinePresentationIntent?.contains(.code) == true {
            content[run.range].font = ui
            content[run.range].backgroundColor = foreground.opacity(0.1)
        }
        return content
    }
}

extension View {
    func safeLinks() -> some View {
        environment(\.openURL, OpenURLAction { url in
            MarkdownLinkPolicy.allows(url) ? .systemAction : .discarded
        })
    }
}

struct MarkdownText: View, Equatable {
    let text: String
    var muted = false

    nonisolated static func == (lhs: MarkdownText, rhs: MarkdownText) -> Bool {
        lhs.text == rhs.text && lhs.muted == rhs.muted
    }

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            ForEach(Array(Markdown.blocks(text).enumerated()), id: \.offset) { _, block in
                view(for: block)
            }
        }
        .font(theme.ui)
        .foregroundStyle(muted ? theme.muted : theme.foreground)
        .tint(theme.accent)
        .safeLinks()
    }

    @ViewBuilder private func view(for block: MarkdownBlock) -> some View {
        switch block {
        case .heading(let level, let text):
            Text(theme.inlineMarkdown(text))
                .font(level <= 2 ? theme.title : theme.ui.weight(.semibold))
                .textSelection(.enabled)
        case .paragraph(let text):
            Text(theme.inlineMarkdown(text))
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
        case .list(let items):
            VStack(alignment: .leading, spacing: theme.spacing.xs) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    HStack(alignment: .firstTextBaseline, spacing: theme.spacing.m) {
                        Text(item.marker)
                            .foregroundStyle(theme.muted)
                        Text(theme.inlineMarkdown(item.text))
                            .textSelection(.enabled)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .padding(.leading, CGFloat(item.depth) * theme.indent)
                }
            }
        case .code(let language, let text):
            CodeBlock(language: language, text: text)
        }
    }
}

private struct CodeBlock: View {
    let language: String
    let text: String

    @State private var copied = false
    @Environment(\.theme) private var theme

    var body: some View {
        ScrollView(.horizontal) {
            Text(text)
                .font(theme.ui.monospaced())
                .foregroundStyle(theme.foreground)
                .textSelection(.enabled)
                .fixedSize()
                .padding(theme.gutter)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 6).fill(theme.foreground.opacity(0.06)))
        .overlay(alignment: .topTrailing) {
            HStack(spacing: theme.spacing.s) {
                if !language.isEmpty {
                    Text(language)
                        .font(theme.uiSmall)
                        .foregroundStyle(theme.muted)
                }
                Button(action: copy) {
                    Image(systemName: copied ? "checkmark" : "doc.on.doc")
                        .foregroundStyle(copied ? theme.success : theme.muted)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("Copy")
            }
            .padding(theme.spacing.m)
        }
    }

    private func copy() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        copied = true
        Task {
            try? await Task.sleep(for: .seconds(1.5))
            copied = false
        }
    }
}
