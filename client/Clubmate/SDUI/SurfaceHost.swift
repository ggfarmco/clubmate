import SwiftUI

struct SurfaceHost: View {
    let sessionID: String

    @Environment(\.theme) private var theme

    var body: some View {
        Text("terminal \(sessionID)")
            .font(theme.ui)
            .foregroundStyle(theme.muted)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(theme.background, ignoresSafeAreaEdges: [])
    }
}
