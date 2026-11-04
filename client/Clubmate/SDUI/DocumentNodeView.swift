import AppKit
import SwiftUI

import ClubmateDocument
import ClubmateKit

extension EnvironmentValues {
    @Entry var documentZoom: DocumentZoom?
    @Entry var documentFind: DocumentFind?
}

struct DocumentNodeView: View {
    let node: SDUINode

    @Environment(\.theme) private var theme
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.documentZoom) private var zoom
    @Environment(\.documentFind) private var find

    private var content: DocumentContent {
        DocumentContent(path: node.string("path") ?? "", html: node.string("html") ?? "", rev: Int(node.props["rev"]?.numberValue ?? 0), anchor: node.string("anchor"), kind: node.string("kind") == "artifact" ? .artifact : .markdown)
    }

    private var fit: Bool {
        node.string("size") == "fit"
    }

    var body: some View {
        DocumentWebView(content: content, theme: theme.document, size: fit ? .fit : .fill, zoom: fit ? nil : zoom, find: fit ? nil : find) { link in
            open(link)
        }
        .frame(maxWidth: .infinity, maxHeight: fit ? nil : .infinity)
        .overlay(alignment: .topTrailing) {
            if !fit, let find, find.visible {
                DocumentFindBar(find: find)
                    .padding(.trailing, theme.gutter)
                    .padding(.top, theme.spacing.s)
            }
        }
    }

    private func open(_ link: DocumentLink) {
        switch link {
        case .doc(let path, let anchor):
            dispatch(node, .named("openDoc", .string(anchor.map { "\(path)#\($0)" } ?? path)))
        case .file(let path):
            dispatch(node, .named("openFile", .string(path)))
        case .external(let url):
            NSWorkspace.shared.open(url)
        case .anchor, .ignore:
            break
        }
    }
}

private struct DocumentFindBar: View {
    let find: DocumentFind

    @Environment(\.theme) private var theme
    @State private var text = ""

    var body: some View {
        HStack(spacing: theme.spacing.s) {
            Image(systemName: "magnifyingglass")
                .font(theme.uiSmall)
                .foregroundStyle(theme.muted)
            PaletteField(text: $text, placeholder: "Find", move: step, submit: { step(1) }, cancel: find.hide, fontSize: theme.uiSize)
                .id(find.focusRequest)
                .frame(width: 180, height: theme.control)
            Text(count)
                .font(theme.uiSmall)
                .monospacedDigit()
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .fixedSize()
            FindBarButton(icon: "chevron.up", help: "Previous match ⇧⌘G", disabled: find.total == 0) { step(-1) }
            FindBarButton(icon: "chevron.down", help: "Next match ⌘G", disabled: find.total == 0) { step(1) }
            FindBarButton(icon: "xmark", help: "Close Find ⎋", disabled: false, action: find.hide)
        }
        .padding(.leading, theme.spacing.m)
        .padding(.trailing, theme.spacing.xs)
        .padding(.vertical, theme.spacing.xs)
        .background(RoundedRectangle(cornerRadius: theme.radius.l).fill(theme.surface3))
        .overlay(RoundedRectangle(cornerRadius: theme.radius.l).stroke(theme.borderStrong, lineWidth: 1))
        .shadow(color: .black.opacity(0.2), radius: 8, y: 2)
        .onAppear { text = find.query }
        .onChange(of: text) { _, query in
            Task { await find.search(query) }
        }
    }

    private var count: String {
        guard !find.query.isEmpty else { return "" }
        guard find.total > 0 else { return "No results" }
        return "\(find.current)/\(find.total)\(find.more ? "+" : "")"
    }

    private func step(_ delta: Int) {
        Task { delta < 0 ? await find.previous() : await find.next() }
    }
}

private struct FindBarButton: View {
    let icon: String
    let help: String
    let disabled: Bool
    let action: () -> Void

    @Environment(\.theme) private var theme
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: icon)
                .font(theme.uiSmall.weight(.semibold))
                .foregroundStyle(theme.text2)
                .frame(width: theme.control, height: theme.control)
                .background(RoundedRectangle(cornerRadius: theme.radius.m).fill(hovering && !disabled ? theme.hover : .clear))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(disabled)
        .opacity(disabled ? 0.5 : 1)
        .onHover { hovering = $0 }
        .help(help)
    }
}
