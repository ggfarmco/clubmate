import SwiftUI

import CompanionKit

struct InboxTray: View {
    let context: PluginContext

    var body: some View {
        let news = context.model.inboxHasNews
        Button {
            if !context.navigation.isTop(.inbox) { context.navigation.push(.inbox) }
        } label: {
            Image(systemName: "tray")
                .overlay(alignment: .topTrailing) {
                    if news {
                        Circle()
                            .fill(Color.warning)
                            .frame(width: 9, height: 9)
                            .offset(x: 4, y: -3)
                    }
                }
        }
        .accessibilityLabel("Inbox")
        .accessibilityValue(news ? "Something waits" : "")
        .accessibilityIdentifier("inbox.tray")
    }
}
