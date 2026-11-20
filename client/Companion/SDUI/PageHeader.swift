import SwiftUI

import ClubmateDocument
import CompanionKit

struct SDUIPage {
    let title: String
    let find: DocumentFind

    func dropTitle(_ html: String) -> String {
        guard let match = html.firstMatch(of: /^\s*<h1[^>]*>(.*?)<\/h1>/.dotMatchesNewlines()) else { return html }
        let text = String(match.1).replacing(/<[^>]+>/, with: "").trimmingCharacters(in: .whitespacesAndNewlines)
        return text == title ? String(html[match.range.upperBound...]) : html
    }
}

extension EnvironmentValues {
    @Entry var sduiPage: SDUIPage?
}

extension SDUINode {
    var isPageHeader: Bool {
        type == "header" && string("field") == nil
    }

    var pageDocument: (header: SDUINode, document: SDUINode)? {
        guard type == "stack", string("direction") != "horizontal", children.count == 2, children[0].isPageHeader, children[1].type == "document", children[1].string("size") != "fit" else { return nil }
        return (children[0], children[1])
    }
}

struct PageHeaderInline: View {
    let node: SDUINode
    let page: SDUIPage
    let sectionLine: String?
    var inset: CGFloat = 20

    private var title: String? {
        let title = node.string("title") ?? ""
        return title.isEmpty || title == page.title ? nil : title
    }

    private var meta: String? {
        guard sectionLine != nil, let meta = node.string("meta"), !meta.isEmpty else { return nil }
        return meta
    }

    var isEmpty: Bool {
        title == nil && meta == nil && node.headerBadges().isEmpty
    }

    var body: some View {
        if !isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                if let title {
                    Text(title)
                        .font(.title2.weight(.bold))
                        .lineLimit(2)
                }
                if let meta {
                    Text(meta)
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
                let badges = node.headerBadges()
                if !badges.isEmpty {
                    FlowLayout(spacing: 6) {
                        ForEach(Array(badges.enumerated()), id: \.offset) { _, badge in
                            BadgeView(text: badge.text, tone: badge.tone)
                        }
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, inset)
            .padding(.bottom, 8)
        }
    }
}

struct PageHeaderColumn: View {
    let title: String
    var meta: String?
    var badges: [SDUIHeaderBadge] = []

    static let top: CGFloat = 10
    static let titleEnd: Double = 44

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(.title.weight(.bold))
                .lineLimit(3)
                .accessibilityAddTraits(.isHeader)
                .accessibilityIdentifier("page.title")
            if let meta, !meta.isEmpty {
                Text(meta)
                    .font(.footnote)
                    .foregroundStyle(Color.captionInk)
                    .lineLimit(1)
            }
            if !badges.isEmpty {
                FlowLayout(spacing: 6) {
                    ForEach(Array(badges.enumerated()), id: \.offset) { _, badge in
                        BadgeView(text: badge.text, tone: badge.tone)
                    }
                }
                .padding(.top, 6)
            }
        }
        .frame(maxWidth: PadLayout.measure, alignment: .leading)
        .padding(.horizontal, PadLayout.margin)
        .frame(maxWidth: .infinity)
        .padding(.top, Self.top)
        .padding(.bottom, 14)
    }

    static func meta(_ parts: String?...) -> String? {
        let line = parts.compactMap { $0?.isEmpty == false ? $0 : nil }.joined(separator: " · ")
        return line.isEmpty ? nil : line
    }
}

struct PadPageTitle: ViewModifier {
    let title: String
    @Environment(\.documentReading) private var reading
    @Environment(\.horizontalSizeClass) private var sizeClass

    func body(content: Content) -> some View {
        if PadLayout.on(sizeClass) {
            content
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .principal) {
                        Text(reading?.titled == true ? title : "")
                            .font(.headline)
                            .lineLimit(1)
                            .contentTransition(.opacity)
                            .animation(.easeOut(duration: 0.2), value: reading?.titled)
                            .accessibilityIdentifier("page.barTitle")
                    }
                }
        } else {
            content.navigationBarTitleDisplayMode(.large)
        }
    }
}

struct PageHeaderBar: ViewModifier {
    let node: SDUINode
    let context: SDUIContext
    let find: DocumentFind?
    @State private var confirming: SDUINode?
    @Environment(\.sduiForm) private var form
    @Environment(\.sectionLine) private var sectionLine
    @Environment(\.backChipShown) private var chip
    @Environment(\.horizontalSizeClass) private var sizeClass

    private var bar: [SDUIChild] {
        guard sectionLine == nil, !chip else { return [] }
        return Array(node.headerBar.filter { $0.node.string("variant") == "primary" }.prefix(2))
    }

    private var more: [SDUIChild] {
        let ids = Set(bar.map(\.id))
        return node.headerButtons.filter { !ids.contains($0.id) }
    }

    private var meta: String? {
        guard sectionLine == nil, !PadLayout.on(sizeClass), let meta = node.string("meta"), !meta.isEmpty else { return nil }
        return meta
    }

    func body(content: Content) -> some View {
        subtitled(content)
            .preference(key: PageVoiceKey.self, value: true)
            .toolbar {
                ToolbarItemGroup(placement: .topBarTrailing) {
                    ForEach(bar) { child in
                        Button(child.node.string("label") ?? "") { press(child.node) }
                            .disabled(child.node.bool("disabled") == true)
                    }
                }
                PageVoiceItem()
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        ShowInButton()
                        if let find {
                            Button("Find in Page", systemImage: "magnifyingglass") { find.show() }
                        }
                        ForEach(more) { child in
                            Button(child.node.string("label") ?? "", systemImage: child.node.string("icon") ?? "", role: child.node.string("variant") == "danger" ? .destructive : nil) { press(child.node) }
                                .disabled(child.node.bool("disabled") == true)
                        }
                    } label: {
                        Image(systemName: "ellipsis")
                    }
                    .accessibilityLabel("More Actions")
                }
            }
            .alert(confirming?.confirmation() ?? "", isPresented: Binding(get: { confirming != nil }, set: { if !$0 { confirming = nil } }), presenting: confirming) { button in
                Button(button.confirmActionTitle(), role: button.string("variant") == "danger" ? .destructive : nil) { SDUIPress.run(button, context: context, form: form) }
                Button("Cancel", role: .cancel) {}
            } message: { button in
                if let message = button.confirmMessage() { Text(message) }
            }
    }

    @ViewBuilder
    private func subtitled(_ content: Content) -> some View {
        if let meta {
            content.navigationSubtitle(meta)
        } else {
            content
        }
    }

    private func press(_ button: SDUINode) {
        if button.confirmation() != nil { confirming = button } else { SDUIPress.run(button, context: context, form: form) }
    }
}
