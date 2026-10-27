import Observation
import SwiftUI

import ClubmateKit

@MainActor
@Observable
final class EventBanner {
    private(set) var message: String?

    @ObservationIgnored private var dismissal: Task<Void, Never>?

    func show(_ message: String) {
        self.message = message
        dismissal?.cancel()
        dismissal = Task { [weak self] in
            try? await Task.sleep(for: .seconds(3))
            guard !Task.isCancelled else { return }
            self?.message = nil
        }
    }
}

private struct EventBannerModifier: ViewModifier {
    let shared: EventBanner?

    @State private var own = EventBanner()
    @Environment(\.theme) private var theme

    private var banner: EventBanner { shared ?? own }

    func body(content: Content) -> some View {
        content
            .environment(banner)
            .overlay(alignment: .bottom) {
                if let message = banner.message {
                    BannerLabel(text: message, edge: .top)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
            .animation(.easeOut(duration: 0.15), value: banner.message)
    }
}

struct BannerLabel: View {
    let text: String
    let edge: Alignment

    @Environment(\.theme) private var theme

    var body: some View {
        Text(text)
            .font(theme.ui)
            .foregroundStyle(theme.foreground)
            .lineLimit(2)
            .textSelection(.enabled)
            .padding(.horizontal, theme.gutter * 1.5)
            .padding(.vertical, theme.gutter * 0.75)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.danger.opacity(0.25))
            .background(theme.background)
            .overlay(alignment: edge) { theme.border.frame(height: 1) }
    }
}

extension View {
    func eventBanner(_ banner: EventBanner? = nil) -> some View {
        modifier(EventBannerModifier(shared: banner))
    }
}
