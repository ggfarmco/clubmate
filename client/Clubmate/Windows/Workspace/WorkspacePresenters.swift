import AppKit
import SwiftUI

import ClubmateKit

struct WorkspacePresenters: ViewModifier {
    @Binding var windowID: String?

    @Environment(AppModel.self) private var app
    @Environment(WindowRouter.self) private var windows
    @Environment(\.openSettings) private var openSettings

    func body(content: Content) -> some View {
        content
            .testPresenter("inbox", inboxPresented)
            .testPresenter("switcher", switcherPresented)
            .testPresenter("settings", settingsPresented)
            .sheet(isPresented: reportPresented) { ReportSheet(model: app.reports) }
            .sheet(isPresented: pairPresented) {
                if let windowID { PairDeviceSheet(model: app.remoteDevices, presenter: windowID) }
            }
            .testPresenter("pair", pairPresented)
            .testPresenter("report", reportFixture(over: false))
            .testPresenter("report-over", reportFixture(over: true))
    }

    private var pairPresented: Binding<Bool> {
        Binding { [app] in app.remoteDevices.presents(in: windowID) } set: { [app] shown in
            guard let windowID else { return }
            Task {
                if shown { await app.remoteDevices.pair(in: windowID) } else { await app.remoteDevices.done(in: windowID) }
            }
        }
    }

    private var reportPresented: Binding<Bool> {
        Binding { [app] in app.reports.presented(in: windowID) } set: { [app] shown in
            if !shown { app.reports.cancel() }
        }
    }

    private func reportFixture(over: Bool) -> Binding<Bool> {
        Binding { [app] in app.reports.request != nil } set: { [app] shown in
            if shown {
                Task { _ = await app.report(TestHarness.reportFixture(over: over), ConfirmExpiry()) }
            } else {
                app.reports.cancel()
            }
        }
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
