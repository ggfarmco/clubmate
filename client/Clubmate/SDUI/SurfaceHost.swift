import SwiftUI

struct SurfaceHost: View {
    let sessionID: String

    @Environment(\.surfaceDeck) private var deck
    @Environment(\.theme) private var theme
    @State private var host = UUID()

    var body: some View {
        if GhosttyApp.shared.app == nil {
            placeholder(GhosttyApp.shared.failure ?? "terminal unavailable")
        } else if let deck, !sessionID.isEmpty {
            Group {
                if deck.owner == host {
                    SurfaceDeckRepresentable(deck: deck, sessionID: sessionID)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    placeholder("one terminal per window")
                }
            }
            .task(id: deck.owner) { deck.claim(host) }
            .onDisappear { deck.release(host) }
        } else {
            placeholder("terminal \(sessionID)")
        }
    }

    private func placeholder(_ text: String) -> some View {
        Text(text)
            .font(theme.ui)
            .foregroundStyle(theme.muted)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(theme.background, ignoresSafeAreaEdges: [])
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
