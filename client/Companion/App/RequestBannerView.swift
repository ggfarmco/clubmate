import SwiftUI

import CompanionKit

struct RequestBannerHost: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation
    @State private var dismissed: Set<String> = []

    var body: some View {
        let banner = current.flatMap { RequestBanner(model.needsYou(), current: $0, dismissed: dismissed) }
        VStack {
            if let banner {
                RequestBannerView(banner: banner) {
                    navigation.workspaces[banner.item.computer] = banner.item.workspace.id
                    navigation.open(computer: banner.item.computer, plugin: AgentsPluginInfo.id, detail: banner.item.session.id)
                } dismiss: {
                    dismissed.formUnion(banner.keys)
                }
                .padding(.horizontal, 12)
                .padding(.top, 4)
                .frame(maxWidth: 820)
                .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .frame(maxWidth: .infinity)
        .animation(.snappy, value: banner)
    }

    private var current: String? {
        guard let item = navigation.sidebar, item.plugin == AgentsPluginInfo.id, let detail = navigation.detail, detail != AgentsPluginInfo.newSession else { return nil }
        return NeedsYouItem.id(computer: item.computer, session: detail)
    }
}

struct RequestBannerView: View {
    let banner: RequestBanner
    let open: () -> Void
    let dismiss: () -> Void

    var body: some View {
        let session = banner.item.session
        HStack(spacing: 4) {
            Button(action: open) {
                HStack(spacing: 12) {
                    SessionGlyphView(glyph: session.glyph)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(session.displayTitle)
                            .font(.subheadline.weight(.semibold))
                            .lineLimit(1)
                        StateLineText(line: line)
                            .font(.subheadline)
                            .lineLimit(1)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    if let more = banner.moreText {
                        Text(more)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    Image(systemName: "chevron.right")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.tertiary)
                }
                .frame(minHeight: 44)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .combine)
            .accessibilityHint("Opens the session")
            Button("Dismiss", systemImage: "xmark", action: dismiss)
                .labelStyle(.iconOnly)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.secondary)
                .frame(width: 44, height: 44)
                .contentShape(.rect)
                .buttonStyle(.plain)
        }
        .padding(.leading, 14)
        .padding(.trailing, 2)
        .padding(.vertical, 6)
        .glassEffect(.regular, in: .rect(cornerRadius: 20))
    }

    private var line: SessionStateLine {
        var line = banner.item.session.stateLine(now: .now)
        line.age = nil
        return line
    }
}
