import SwiftUI

import ClubmateKit

struct TreeDockView: View {
    let dock: [SDUITreeDock]
    let selected: String?
    let cursor: String?
    let active: Bool
    let toggle: (String) -> Void
    let add: (SDUITreeDock) -> Void
    let open: (String) -> Void

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(spacing: 0) {
            ForEach(dock) { section in
                header(section)
                if section.expanded, !section.rows.isEmpty {
                    rows(section)
                }
            }
        }
        .padding(.vertical, theme.spacing.xs)
        .background(theme.surface1)
        .overlay(alignment: .top) { theme.border.frame(height: 1) }
    }

    private func header(_ section: SDUITreeDock) -> some View {
        HStack(spacing: 0) {
            ItemRow(item: item(section), selected: false, active: active, cursor: cursor == section.id, tone: nil, detailTone: section.attention ? "warning" : nil, action: { toggle(section.id) }) {
                Button { toggle(section.id) } label: {
                    Image(systemName: "chevron.up")
                        .font(.system(size: 8, weight: .semibold))
                        .rotationEffect(.degrees(section.expanded ? 180 : 0))
                        .foregroundStyle(theme.muted)
                        .frame(width: theme.indent, height: theme.rowHeight)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(section.expanded ? "Fold \(section.label)" : "Unfold \(section.label)")
            }
            if let action = section.add {
                Button { add(section) } label: {
                    Image(systemName: "plus")
                        .font(theme.glyph)
                        .foregroundStyle(theme.muted)
                        .frame(width: theme.rowHeight, height: theme.rowHeight)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help(action.label)
                .accessibilityLabel(action.label)
            }
        }
    }

    private func rows(_ section: SDUITreeDock) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(section.rows) { row in
                    ItemRow(item: row.item, selected: row.id == selected, active: active, cursor: cursor == row.id, muted: row.muted, faint: row.faint, tone: row.tone, detailTone: row.detailTone, detailFaint: row.detailFaint, depth: 1 + (section.depths[row.id] ?? 0), badges: row.badges, labelIcon: row.labelIcon, action: { open(row.id) }) {
                        Color.clear.frame(width: theme.indent)
                    }
                    .id(row.id)
                }
            }
        }
        .frame(height: theme.rowHeight * CGFloat(min(section.rows.count, section.cap)))
        .clipped()
        .overlay(alignment: .leading) { theme.border.frame(width: 1).padding(.leading, theme.indent + theme.spacing.s) }
    }

    private func item(_ section: SDUITreeDock) -> SDUIItem {
        var fields: [String: JSONValue] = ["id": .string(section.id), "label": .string(section.label)]
        if let icon = section.icon { fields["icon"] = .string(icon) }
        if let detail = section.detail { fields["detail"] = .string(detail) }
        return SDUIItem(.object(fields))!
    }
}

struct TreeDockFade: View {
    @Environment(\.theme) private var theme

    var body: some View {
        LinearGradient(colors: [theme.surface1.opacity(0), theme.surface1], startPoint: .top, endPoint: .bottom)
            .frame(height: theme.rowHeight)
            .allowsHitTesting(false)
    }
}
