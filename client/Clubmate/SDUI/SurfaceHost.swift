import SwiftUI

struct SurfaceHost: View {
    let sessionID: String

    @Environment(\.surfaceDeck) private var deck
    @Environment(\.theme) private var theme

    var body: some View {
        if let deck, !sessionID.isEmpty {
            SurfaceDeckRepresentable(deck: deck, sessionID: sessionID)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            Text("terminal \(sessionID)")
                .font(theme.ui)
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(theme.background, ignoresSafeAreaEdges: [])
        }
    }
}

private struct SurfaceDeckRepresentable: NSViewRepresentable {
    let deck: SurfaceDeck
    let sessionID: String

    func makeNSView(context: Context) -> SurfaceDeckView {
        deck.container
    }

    func updateNSView(_ view: SurfaceDeckView, context: Context) {
        deck.select(sessionID)
    }
}
