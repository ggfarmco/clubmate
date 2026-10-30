import SwiftUI

import ClubmateKit

struct Rail: View {
    let sections: [SectionInfo]
    let selected: String?
    let select: (String) -> Void
    let move: (String, String) -> Void
    let resetOrder: (() -> Void)?

    @Environment(\.theme) private var theme

    var body: some View {
        VStack(spacing: 0) {
            ForEach(sections, id: \.id) { section in
                RailButton(section: section, selected: section.id == selected, accepts: accepts) { select(section.id) } drop: { dragged in
                    move(dragged, section.id)
                }
            }
            Spacer(minLength: 0)
        }
        .frame(width: 36)
        .frame(maxHeight: .infinity)
        .background(theme.background, ignoresSafeAreaEdges: [])
        .contentShape(Rectangle())
        .contextMenu {
            Button("Reset Order") { resetOrder?() }
                .disabled(resetOrder == nil)
        }
    }

    private func accepts(_ id: String) -> Bool {
        sections.contains { $0.id == id }
    }
}

private struct RailButton: View {
    let section: SectionInfo
    let selected: Bool
    let accepts: (String) -> Bool
    let action: () -> Void
    let drop: (String) -> Void

    @State private var hovering = false
    @State private var targeted = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            icon
                .foregroundStyle(selected || hovering ? theme.foreground : theme.muted)
                .frame(width: 36, height: 36)
                .background(background, ignoresSafeAreaEdges: [])
                .overlay(alignment: .leading) {
                    if selected { theme.accent.frame(width: 2) }
                }
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(section.title)
        .onHover { hovering = $0 }
        .draggable(section.id) {
            icon.foregroundStyle(theme.foreground)
        }
        .dropDestination(for: String.self) { items, _ in
            guard let dragged = items.first, dragged != section.id, accepts(dragged) else { return false }
            drop(dragged)
            return true
        } isTargeted: { targeted = $0 }
    }

    private var icon: some View {
        Image(systemName: SymbolName.resolve(section.icon, fallback: "square.grid.2x2"))
            .font(.system(size: 14))
    }

    private var background: Color {
        if targeted { return theme.accent.opacity(0.18) }
        return selected ? theme.foreground.opacity(0.06) : .clear
    }
}
