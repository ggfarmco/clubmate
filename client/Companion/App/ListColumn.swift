import SwiftUI

import CompanionKit

enum ListColumn {
    static let width = ColumnStep.Room.list
}

struct ColumnList: ViewModifier {
    @Environment(AppNavigation.self) private var navigation: AppNavigation?

    func body(content: Content) -> some View {
        let wide = (navigation?.layout ?? .compact) != .compact
        content
            .listStyle(.insetGrouped)
            .scrollContentBackground(wide ? .hidden : .automatic)
            .background(wide ? Color.listGround : Color.clear)
    }
}

struct ColumnRow: ViewModifier {
    let selected: Bool
    let always: Bool
    let ring: Bool
    @Environment(AppNavigation.self) private var navigation: AppNavigation?

    func body(content: Content) -> some View {
        let wide = (navigation?.layout ?? .compact) != .compact
        if wide {
            content
                .listRowBackground(background(selected ? Color.selectedRow : Color.surface1))
                .foregroundStyle(selected ? AnyShapeStyle(Color(.label)) : AnyShapeStyle(.foreground))
        } else if ring {
            content.listRowBackground(background(selected ? Color.selectedRow : Color.surface1))
        } else if always {
            content.listRowBackground(selected ? Color.selectedRow : nil)
        } else {
            content
        }
    }

    private func background(_ fill: Color) -> some View {
        fill.overlay {
            if ring {
                RoundedRectangle(cornerRadius: 26, style: .continuous).strokeBorder(Color.accentColor, lineWidth: 1.5)
            }
        }
    }
}

extension View {
    func columnList() -> some View {
        modifier(ColumnList())
    }

    func columnRow(selected: Bool = false, always: Bool = false, ring: Bool = false) -> some View {
        modifier(ColumnRow(selected: selected, always: always, ring: ring))
    }

    func bareRow() -> some View {
        listRowBackground(Color.clear)
    }

    func bareEditor() -> some View {
        scrollContentBackground(.hidden)
    }
}
