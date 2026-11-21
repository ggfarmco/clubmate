import SwiftUI

struct UpdatingLine: View {
    var body: some View {
        HStack(spacing: 6) {
            ProgressView()
                .controlSize(.mini)
            Text("Updating…")
        }
        .font(.footnote)
        .foregroundStyle(Color.captionInk)
    }
}
