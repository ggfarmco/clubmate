import SwiftUI

import CompanionKit

struct ShowIn {
    let tab: CompanionTab
    let destination: Destination
}

extension EnvironmentValues {
    @Entry var showIn: ShowIn?
    @Entry var sectionLine: String?
}

struct ShowInButton: View {
    @Environment(\.showIn) private var showIn
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        if let showIn {
            Button("Show in \(showIn.tab.title)", systemImage: showIn.tab.systemImage) {
                navigation.show(in: showIn.tab, showIn.destination)
            }
        }
    }
}

struct SectionLineModifier: ViewModifier {
    let entry: StackEntry
    let tab: CompanionTab
    @Environment(AppNavigation.self) private var navigation
    @Environment(CompanionModel.self) private var model
    @Environment(\.horizontalSizeClass) private var sizeClass

    func body(content: Content) -> some View {
        if entry.destination.tab != tab, let line = entry.destination.sectionLine(workspace: navigation.workspaceName(entry.workspace)) {
            subtitled(content, line)
                .environment(\.showIn, showIn)
                .environment(\.sectionLine, line)
        } else {
            content
        }
    }

    @ViewBuilder
    private func subtitled(_ content: Content, _ line: String) -> some View {
        if PadLayout.on(sizeClass) && entry.destination.readsInPlace {
            content
        } else {
            content.navigationSubtitle(line)
        }
    }

    private var showIn: ShowIn? {
        guard let target = entry.destination.tab, entry.workspace == nil,
              navigation.shows(target, model) else { return nil }
        return ShowIn(tab: target, destination: entry.destination)
    }
}
