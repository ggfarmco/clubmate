import SwiftUI

import ClubmateKit

struct FormSheetHost: View {
    static let width: CGFloat = 560

    let node: SDUINode

    @State private var shown = true
    @Environment(\.sduiDispatch) private var dispatch
    @Environment(\.composerDrafts) private var drafts
    @Environment(\.theme) private var theme

    var body: some View {
        Color.clear
            .frame(width: 0, height: 0)
            .sheet(isPresented: $shown, onDismiss: cancel) {
                FormNodeView(node: node)
                    .padding(.horizontal, theme.spacing.xl)
                    .padding(.vertical, theme.spacing.m)
                    .frame(width: Self.width)
                    .background(theme.surface3)
                    .onExitCommand { shown = false }
                    .environment(\.sduiDispatch, dispatch)
                    .environment(\.composerDrafts, drafts)
                    .environment(\.theme, theme)
            }
    }

    private func cancel() {
        dispatch(node, .named("cancel", .null))
    }
}
