import SwiftUI

struct CappedScroll<Content: View>: View {
    @ViewBuilder let content: Content

    @State private var height: CGFloat = 0
    @Environment(\.theme) private var theme

    var body: some View {
        ScrollView {
            content
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
        }
        .frame(height: min(height, theme.rowHeight * 24))
        .clipped()
    }
}
