import SwiftUI

import ClubmateKit

struct Rail: View {
    let sections: [SectionInfo]
    let selected: String?
    let select: (String) -> Void

    @Environment(\.theme) private var theme

    static func symbol(_ icon: String) -> String {
        icon == "terminal" ? "terminal" : "square.grid.2x2"
    }

    var body: some View {
        VStack(spacing: 0) {
            ForEach(sections, id: \.id) { section in
                RailButton(section: section, selected: section.id == selected) { select(section.id) }
            }
            Spacer(minLength: 0)
        }
        .frame(width: 36)
        .frame(maxHeight: .infinity)
        .background(theme.background, ignoresSafeAreaEdges: [])
    }
}

private struct RailButton: View {
    let section: SectionInfo
    let selected: Bool
    let action: () -> Void

    @State private var hovering = false
    @Environment(\.theme) private var theme

    var body: some View {
        Button(action: action) {
            Image(systemName: Rail.symbol(section.icon))
                .font(.system(size: 14))
                .foregroundStyle(selected || hovering ? theme.foreground : theme.muted)
                .frame(width: 36, height: 36)
                .background(selected ? theme.foreground.opacity(0.06) : .clear, ignoresSafeAreaEdges: [])
                .overlay(alignment: .leading) {
                    if selected { theme.accent.frame(width: 2) }
                }
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(section.title)
        .onHover { hovering = $0 }
    }
}
