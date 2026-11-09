import SwiftUI

import CompanionKit

struct RequestBannerHost: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        let banner = current.flatMap { RequestBanner(model.needsYou(), current: $0) }
        Group {
            if let banner {
                RequestBannerView(banner: banner) {
                    navigation.workspaces[banner.item.computer] = banner.item.workspace.id
                    navigation.open(computer: banner.item.computer, plugin: AgentsPluginInfo.id, detail: banner.item.session.id)
                }
                .transition(.opacity)
            }
        }
        .animation(.snappy, value: banner)
    }

    private var current: String? {
        guard let item = navigation.sidebar, item.plugin == AgentsPluginInfo.id, let detail = navigation.detail, AgentsPluginInfo.isSession(detail) else { return nil }
        return NeedsYouItem.id(computer: item.computer, session: detail)
    }
}

struct RequestBannerView: View {
    let banner: RequestBanner
    let open: () -> Void

    var body: some View {
        let item = banner.item
        Button(action: open) {
            HStack(spacing: 12) {
                Image(systemName: Self.symbol(item.session.glyph))
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(Color.warningInk)
                    .frame(width: 28, height: 28)
                    .background(Color(.tertiarySystemFill), in: .circle)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.session.displayTitle)
                        .font(.subheadline.weight(.semibold))
                        .lineLimit(1)
                    item.mac.lead(item.bannerDetail)
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if let more = banner.moreText {
                    Text(more)
                        .font(.footnote)
                        .foregroundStyle(Color.captionInk)
                        .fixedSize()
                }
                RowChevron(always: true)
            }
            .frame(minHeight: 44)
            .padding(.leading, 14)
            .padding(.trailing, 16)
            .padding(.vertical, 6)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityHint("Opens the session")
        .background(Color.surface1, in: .rect(cornerRadius: 20))
        .overlay {
            RoundedRectangle(cornerRadius: 20).strokeBorder(Color(.separator), lineWidth: 1)
        }
    }

    private static func symbol(_ glyph: SessionGlyph) -> String {
        switch glyph {
        case .question: "questionmark.bubble"
        case .step: "dollarsign"
        case .cap: "exclamationmark"
        default: "hand.raised"
        }
    }
}
