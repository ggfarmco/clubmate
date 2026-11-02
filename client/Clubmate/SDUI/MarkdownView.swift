import AppKit
import SwiftUI

import ClubmateDocument
import ClubmateKit

extension Theme {
    func inlineMarkdown(_ text: String) -> AttributedString {
        var content = Markdown.inline(text)
        for run in content.runs where run.inlinePresentationIntent?.contains(.code) == true {
            content[run.range].font = ui
            content[run.range].backgroundColor = pressed
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
    var docLinks: [String: String] = [:]
    var openDoc: ((String) -> Void)?

    nonisolated static func == (lhs: MarkdownText, rhs: MarkdownText) -> Bool {
        lhs.text == rhs.text && lhs.muted == rhs.muted && lhs.docLinks == rhs.docLinks
    }

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: theme.spacing.l) {
            ForEach(Array(MarkdownSection.split(Markdown.blocks(text)).enumerated()), id: \.offset) { _, section in
                switch section {
                case .prose(let blocks):
                    ProseText(content: theme.prose(blocks, muted: muted, docLinks: docLinks), linkColor: NSColor(theme.accent), openDoc: openDoc)
                case .code(let language, let text):
                    CodeBlock(language: language, text: text)
                case .mermaid(let source):
                    MermaidBlock(source: source)
                }
            }
        }
    }
}

private enum MarkdownSection {
    case prose([MarkdownBlock])
    case code(language: String, text: String)
    case mermaid(String)

    static func split(_ blocks: [MarkdownBlock]) -> [MarkdownSection] {
        var sections: [MarkdownSection] = []
        var prose: [MarkdownBlock] = []
        for block in blocks {
            if case .code(let language, let text) = block {
                if !prose.isEmpty { sections.append(.prose(prose)) }
                prose = []
                sections.append(.code(language: language, text: text))
            } else if case .mermaid(let source) = block {
                if !prose.isEmpty { sections.append(.prose(prose)) }
                prose = []
                sections.append(.mermaid(source))
            } else {
                prose.append(block)
            }
        }
        if !prose.isEmpty { sections.append(.prose(prose)) }
        return sections
    }
}

@MainActor
private enum ProseCache {
    struct Key: Hashable {
        let block: MarkdownBlock
        let first: Bool
        let muted: Bool
        let docLinks: [String: String]
        let theme: String
    }

    private static let limit = 2048
    private static var pieces: [Key: NSAttributedString] = [:]

    static func piece(_ key: Key, make: () -> NSAttributedString) -> NSAttributedString {
        if let piece = pieces[key] { return piece }
        if pieces.count >= limit { pieces.removeAll() }
        let piece = make()
        pieces[key] = piece
        return piece
    }
}

extension Theme {
    @MainActor
    func prose(_ blocks: [MarkdownBlock], muted: Bool, docLinks: [String: String] = [:]) -> NSAttributedString {
        let result = NSMutableAttributedString()
        let body = nsFont(size: uiSize, bold: false)
        let theme = [fontFamily ?? "", "\(uiSize)", foregroundHex, backgroundHex].joined(separator: "\u{1}")
        for (index, block) in blocks.enumerated() {
            if index > 0 { result.append(NSAttributedString(string: "\n", attributes: [.font: body])) }
            let key = ProseCache.Key(block: block, first: index == 0, muted: muted, docLinks: docLinks, theme: theme)
            result.append(ProseCache.piece(key) { prose(block, gap: index == 0 ? 0 : spacing.l, muted: muted, docLinks: docLinks) })
        }
        return result
    }

    private func prose(_ block: MarkdownBlock, gap: CGFloat, muted: Bool, docLinks: [String: String]) -> NSAttributedString {
        let result = NSMutableAttributedString()
        let body = nsFont(size: uiSize, bold: false)
        let color = NSColor(muted ? self.muted : foreground)
        switch block {
        case .heading(let level, let text):
            let font = level <= 2 ? nsFont(size: uiSize + 1, bold: true) : nsFont(size: uiSize, bold: true)
            result.append(paragraph(text, font: font, color: color, indent: 0, before: gap, docLinks: docLinks))
        case .paragraph(let text):
            result.append(paragraph(text, font: body, color: color, indent: 0, before: gap, docLinks: docLinks))
        case .list(let items):
            for (number, item) in items.enumerated() {
                if number > 0 { result.append(NSAttributedString(string: "\n", attributes: [.font: body])) }
                let lead = CGFloat(item.depth) * indent
                let hang = lead + (item.marker as NSString).size(withAttributes: [.font: body]).width + spacing.m
                let line = paragraph(item.text, font: body, color: color, indent: hang, before: number == 0 ? gap : spacing.xs, docLinks: docLinks)
                let first = paragraphStyle(lead: lead, hang: hang, before: number == 0 ? gap : spacing.xs)
                first.tabStops = [NSTextTab(textAlignment: .natural, location: hang)]
                let marker = NSMutableAttributedString(string: item.marker + "\t", attributes: [.font: body, .foregroundColor: NSColor(self.muted)])
                marker.append(line)
                let head = (marker.string as NSString).paragraphRange(for: NSRange(location: 0, length: 0))
                marker.addAttribute(.paragraphStyle, value: first, range: head)
                result.append(marker)
            }
        case .quote(let text):
            let quote = NSMutableAttributedString(attributedString: paragraph(text, font: body, color: NSColor(self.muted), indent: 0, before: gap, docLinks: docLinks))
            let range = NSRange(location: 0, length: quote.length)
            quote.enumerateAttribute(.paragraphStyle, in: range) { value, range, _ in
                guard let style = (value as? NSParagraphStyle)?.mutableCopy() as? NSMutableParagraphStyle else { return }
                style.firstLineHeadIndent = gutter
                style.headIndent = gutter
                quote.addAttribute(.paragraphStyle, value: style, range: range)
            }
            quote.addAttribute(.quoteBar, value: NSColor(border), range: range)
            result.append(quote)
        case .code, .mermaid:
            break
        }
        return result
    }

    private func paragraph(_ text: String, font: NSFont, color: NSColor, indent: CGFloat, before: CGFloat, docLinks: [String: String]) -> NSAttributedString {
        let source = Markdown.inline(text, docLinks: docLinks)
        let result = NSMutableAttributedString()
        for run in source.runs {
            let intent = run.inlinePresentationIntent ?? []
            var runFont = font
            if intent.contains(.stronglyEmphasized) { runFont = NSFontManager.shared.convert(runFont, toHaveTrait: .boldFontMask) }
            if intent.contains(.emphasized) { runFont = NSFontManager.shared.convert(runFont, toHaveTrait: .italicFontMask) }
            var attributes: [NSAttributedString.Key: Any] = [.font: runFont, .foregroundColor: color, .paragraphStyle: paragraphStyle(lead: indent, hang: indent, before: 0)]
            if intent.contains(.code) { attributes[.backgroundColor] = NSColor(pressed) }
            if intent.contains(.strikethrough) { attributes[.strikethroughStyle] = NSUnderlineStyle.single.rawValue }
            if let link = run.link, MarkdownLinkPolicy.allows(link) || MarkdownDocLink.target(link) != nil { attributes[.link] = link }
            result.append(NSAttributedString(string: String(source[run.range].characters), attributes: attributes))
        }
        if result.length == 0 { return result }
        let head = (result.string as NSString).paragraphRange(for: NSRange(location: 0, length: 0))
        result.addAttribute(.paragraphStyle, value: paragraphStyle(lead: indent, hang: indent, before: before), range: head)
        return result
    }

    private func paragraphStyle(lead: CGFloat, hang: CGFloat, before: CGFloat) -> NSMutableParagraphStyle {
        let style = NSMutableParagraphStyle()
        style.firstLineHeadIndent = lead
        style.headIndent = hang
        style.paragraphSpacingBefore = before
        return style
    }
}

private extension NSAttributedString.Key {
    static let quoteBar = NSAttributedString.Key("clubmateQuoteBar")
}

private final class ProseTextView: NSTextView {
    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        guard let storage = textStorage, let layout = layoutManager else { return }
        storage.enumerateAttribute(.quoteBar, in: NSRange(location: 0, length: storage.length)) { value, range, _ in
            guard let color = value as? NSColor else { return }
            var bar = NSRect.null
            layout.enumerateLineFragments(forGlyphRange: layout.glyphRange(forCharacterRange: range, actualCharacterRange: nil)) { _, used, _, _, _ in
                bar = bar.union(used)
            }
            color.setFill()
            NSRect(x: textContainerOrigin.x, y: bar.minY + textContainerOrigin.y, width: 2, height: bar.height).fill()
        }
    }
}

private struct ProseText: NSViewRepresentable {
    let content: NSAttributedString
    let linkColor: NSColor
    let openDoc: ((String) -> Void)?

    final class Coordinator: NSObject, NSTextViewDelegate {
        var openDoc: ((String) -> Void)?

        func textView(_ textView: NSTextView, clickedOnLink link: Any, at charIndex: Int) -> Bool {
            guard let url = link as? URL, let target = MarkdownDocLink.target(url) else { return false }
            openDoc?(target)
            return true
        }
    }

    func makeCoordinator() -> Coordinator {
        Coordinator()
    }

    func makeNSView(context: Context) -> ProseTextView {
        let view = ProseTextView(usingTextLayoutManager: false)
        view.delegate = context.coordinator
        view.isEditable = false
        view.isSelectable = true
        view.isRichText = false
        view.drawsBackground = false
        view.textContainerInset = .zero
        view.textContainer?.lineFragmentPadding = 0
        view.textContainer?.widthTracksTextView = true
        view.isVerticallyResizable = false
        view.isHorizontallyResizable = false
        return view
    }

    func updateNSView(_ view: ProseTextView, context: Context) {
        context.coordinator.openDoc = openDoc
        view.linkTextAttributes = [.foregroundColor: linkColor, .cursor: NSCursor.pointingHand]
        guard let storage = view.textStorage, !storage.isEqual(to: content) else { return }
        storage.setAttributedString(content)
    }

    func sizeThatFits(_ proposal: ProposedViewSize, nsView view: ProseTextView, context: Context) -> CGSize? {
        guard let container = view.textContainer, let layout = view.layoutManager else { return nil }
        let width = proposal.width.flatMap { $0.isFinite ? $0 : nil } ?? .greatestFiniteMagnitude
        container.containerSize = NSSize(width: width, height: .greatestFiniteMagnitude)
        layout.ensureLayout(for: container)
        let used = layout.usedRect(for: container)
        return CGSize(width: min(width, ceil(used.width)), height: ceil(used.height))
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
        .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(theme.surface2))
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
                .accessibilityLabel(copied ? "Copied" : "Copy")
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

private struct MermaidBlock: View {
    static let width: CGFloat = 960
    static let maxHeight: CGFloat = 480

    let source: String

    @State private var image: CGImage?
    @State private var enlarged = false
    @Environment(\.theme) private var theme

    var body: some View {
        let shown = MermaidRenderer.shared.cached(source: source, theme: theme.document, width: Self.width) ?? image
        Group {
            if let shown {
                Button { enlarged = true } label: {
                    diagram(shown)
                }
                .buttonStyle(.plain)
                .help("Enlarge diagram")
                .accessibilityLabel("Diagram")
            } else {
                CodeBlock(language: "mermaid", text: source)
            }
        }
        .task(id: theme.document) {
            image = await MermaidRenderer.shared.image(source: source, theme: theme.document, width: Self.width)
        }
        .sheet(isPresented: $enlarged) {
            MermaidSheet(source: source, size: NSApp.keyWindow?.contentLayoutRect.size)
        }
    }

    private func diagram(_ image: CGImage) -> some View {
        let natural = CGSize(width: CGFloat(image.width) / 2, height: CGFloat(image.height) / 2)
        let width = natural.width * min(1, Self.maxHeight / max(natural.height, 1))
        return Image(decorative: image, scale: 2)
            .resizable()
            .aspectRatio(contentMode: .fit)
            .frame(maxWidth: width)
            .padding(theme.spacing.m)
            .overlay(RoundedRectangle(cornerRadius: theme.radius.m).strokeBorder(theme.border))
            .contentShape(Rectangle())
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct MermaidSheet: View {
    let source: String
    let size: CGSize?

    @State private var image: CGImage?
    @State private var zoom: ZoomRequest?
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: theme.spacing.s) {
                Text("Diagram")
                    .font(theme.strong)
                    .foregroundStyle(theme.foreground)
                Spacer(minLength: 0)
                zoomButton("minus.magnifyingglass", "Zoom out", .zoomOut)
                zoomButton("arrow.up.left.and.down.right.magnifyingglass", "Fit", .fit)
                zoomButton("plus.magnifyingglass", "Zoom in", .zoomIn)
                CardButton(label: "Close", shortcut: "Esc") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(.horizontal, theme.spacing.l)
            .frame(height: theme.header)
            Rectangle().fill(theme.border).frame(height: 1)
            Group {
                if let image {
                    ZoomableImage(image: image, scale: 4, request: zoom)
                } else {
                    ProgressView()
                        .controlSize(.small)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .task(id: theme.document) {
                image = await MermaidRenderer.shared.image(source: source, theme: theme.document, width: width, scale: 4)
            }
        }
        .frame(width: width, height: height)
        .background(theme.background)
    }

    private var width: CGFloat { max(640, (size?.width ?? 1040) - 80) }
    private var height: CGFloat { max(420, (size?.height ?? 760) - 80) }

    private func zoomButton(_ icon: String, _ help: String, _ action: ZoomAction) -> some View {
        Button { zoom = ZoomRequest(action) } label: {
            Image(systemName: icon)
                .font(theme.ui)
                .foregroundStyle(theme.muted)
                .frame(width: theme.control, height: theme.control)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(help)
        .accessibilityLabel(help)
    }
}
