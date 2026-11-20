import SwiftUI

import CompanionKit

struct NeedsYouRow: View {
    let item: NeedsYouItem
    var dot = false
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            label
        }
        .buttonStyle(.plain)
        .listRowDensity()
        .opacity(item.offline ? 0.5 : 1)
        .accessibilityHint(item.offline ? "Read-only until the Mac is back" : "")
    }

    private var label: some View {
        TimelineView(.periodic(from: .now, by: 30)) { timeline in
            HStack(spacing: 12) {
                SessionGlyphView(glyph: item.session.glyph, muted: item.offline)
                VStack(alignment: .leading, spacing: 2) {
                    title
                        .font(.body.weight(.semibold))
                        .lineLimit(1)
                    item.mac.lead(item.subtitle)
                        .font(.subheadline)
                        .foregroundStyle(Color.captionInk)
                        .lineLimit(1)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Text(ShortAge.since(item.at, now: timeline.date))
                    .font(.subheadline)
                    .foregroundStyle(Color.captionInk)
                UnreadDot(shown: dot)
                RowChevron()
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
    }

    private var title: Text {
        guard let command = item.command else { return Text(item.title) }
        return Text("Run \(Text(command).monospaced())?")
    }
}

struct RowIcon: View {
    let image: Image
    var tint: Color?
    var fill: Color?

    init(systemImage: String, tint: Color? = nil, fill: Color? = nil) {
        self.init(image: Image(systemName: systemImage), tint: tint, fill: fill)
    }

    init(image: Image, tint: Color? = nil, fill: Color? = nil) {
        self.image = image
        self.tint = tint
        self.fill = fill
    }

    var body: some View {
        image
            .font(.system(size: 14, weight: .medium))
            .foregroundStyle(tint ?? Color.captionInk)
            .frame(width: 28, height: 28)
            .background(fill ?? Color(.tertiarySystemFill), in: .circle)
    }
}

struct RowChevron: View {
    var always = false
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        if always || navigation.layout == .compact {
            Image(systemName: "chevron.right")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.tertiary)
                .accessibilityHidden(true)
        }
    }
}

extension CompanionModel {
    var inboxHasNews: Bool {
        !needsYou().isEmpty || !inbox.unread.isEmpty
    }

    func offlineMacNames(_ needs: [NeedsYouItem]) -> [String] {
        var seen: Set<String> = []
        return needs.filter { $0.offline && seen.insert($0.computer).inserted }.compactMap { connection($0.computer)?.computer.name }
    }
}

extension View {
    func listRowDensity(_ vertical: CGFloat = 11) -> some View {
        listRowInsets(.vertical, vertical)
    }
}
