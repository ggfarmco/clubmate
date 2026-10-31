import AppKit
import SwiftUI

struct WorkspacePresenters: ViewModifier {
    @Binding var windowID: String?

    @Environment(AppModel.self) private var app
    @Environment(\.openSettings) private var openSettings

    func body(content: Content) -> some View {
        content
            .testPresenter("inbox", inboxPresented)
            .testPresenter("switcher", switcherPresented)
            .testPresenter("settings", settingsPresented)
    }

    private var inboxPresented: Binding<Bool> {
        Binding { windowID != nil && app.inboxWindow == windowID } set: { shown in
            guard let windowID, shown != (app.inboxWindow == windowID) else { return }
            if shown { app.toggleInbox(in: windowID) } else { app.hideInbox() }
        }
    }

    private var switcherPresented: Binding<Bool> {
        Binding { windowID != nil && app.switcherWindow == windowID } set: { shown in
            guard let windowID, shown != (app.switcherWindow == windowID) else { return }
            if shown { app.toggleSwitcher(in: windowID) } else { app.hideSwitcher() }
        }
    }

    private var settingsPresented: Binding<Bool> {
        Binding { false } set: { [openSettings] shown in
            if shown {
                openSettings()
            } else {
                NSApp.windows.first { $0.identifier?.rawValue.hasPrefix("com_apple_SwiftUI_Settings") == true }?.close()
            }
        }
    }
}
