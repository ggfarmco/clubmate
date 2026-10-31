import AppKit
import SwiftUI

struct WorkspacePresenters: ViewModifier {
    @Binding var windowID: String?

    @Environment(WindowRouter.self) private var windows
    @Environment(\.openSettings) private var openSettings

    func body(content: Content) -> some View {
        content
            .testPresenter("inbox", inboxPresented)
            .testPresenter("switcher", switcherPresented)
            .testPresenter("settings", settingsPresented)
    }

    private var inboxPresented: Binding<Bool> {
        Binding { windowID != nil && windows.inboxWindow == windowID } set: { shown in
            guard let windowID, shown != (windows.inboxWindow == windowID) else { return }
            if shown { windows.toggleInbox(in: windowID) } else { windows.hideInbox() }
        }
    }

    private var switcherPresented: Binding<Bool> {
        Binding { windowID != nil && windows.switcherWindow == windowID } set: { shown in
            guard let windowID, shown != (windows.switcherWindow == windowID) else { return }
            if shown { windows.toggleSwitcher(in: windowID) } else { windows.hideSwitcher() }
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
