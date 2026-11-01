import AppKit
import SwiftUI

enum Motion {
    static var disclosure: Animation? {
        NSWorkspace.shared.accessibilityDisplayShouldReduceMotion ? nil : .easeOut(duration: 0.15)
    }

    @MainActor static func disclose(_ change: () -> Void) {
        withAnimation(disclosure, change)
    }
}

struct DisclosureChevron: View {
    let expanded: Bool
    var font: Font?

    @Environment(\.theme) private var theme

    var body: some View {
        Image(systemName: "chevron.right")
            .font(font ?? theme.glyph)
            .rotationEffect(.degrees(expanded ? 90 : 0))
            .accessibilityHidden(true)
    }
}

extension View {
    func disclosureAnimation(_ value: some Equatable) -> some View {
        animation(Motion.disclosure, value: value)
    }
}
