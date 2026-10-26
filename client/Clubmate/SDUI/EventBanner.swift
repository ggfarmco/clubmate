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
                    Text(message)
                        .font(theme.ui)
                        .foregroundStyle(theme.foreground)
                        .lineLimit(2)
                        .padding(.horizontal, theme.gutter * 1.5)
                        .padding(.vertical, theme.gutter * 0.75)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(theme.danger.opacity(0.25))
                        .background(theme.background)
                        .overlay(alignment: .top) { theme.border.frame(height: 1) }
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
            .animation(.easeOut(duration: 0.15), value: banner.message)
    }
}

extension View {
    func eventBanner(_ banner: EventBanner? = nil) -> some View {
        modifier(EventBannerModifier(shared: banner))
    }
}

extension ClubmateError {
    var bannerText: String {
        switch self {
        case .insecureRuntime(let detail): "insecure runtime: \(detail)"
        case .untrustedPeer: "untrusted core peer"
        case .notRunning: "core is not running"
        case .timeout: "request timed out"
        case .rpc(let code, let message): "\(message) (\(code))"
        case .protocolError(let detail): "protocol error: \(detail)"
        case .coreStartFailed(let detail): "core start failed: \(detail)"
        }
    }
}
