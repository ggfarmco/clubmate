import AppKit
import Foundation

import ClubmateKit

@MainActor
struct NotificationFocus {
    let windows: WindowRouter
    let sessions: SessionRegistry

    private static let revealAttempts = 100

    var current: HubFocus {
        guard let window = windows.keyWindowID, windows.host(window)?.isKeyWindow == true,
              let active = windows.window(window)?.active else { return HubFocus(appActive: NSApp.isActive, workspace: nil) }
        let store = sessions.existing(active)?.store
        let section = store?.selectedSection
        let code = section == CodeSection.id
        return HubFocus(
            appActive: NSApp.isActive,
            workspace: active,
            sidebar: code ? nil : section.flatMap { store?.slots["\($0).sidebar"]?.tree },
            main: section.flatMap { store?.mainTree($0) },
            panel: store?.chatShown == true ? store?.slots[CodeSection.chatSlot]?.tree : nil
        )
    }

    func reveal(_ workspace: String, session: String?, owner: String?) {
        windows.open(workspace)
        guard let session, let target = NotificationHub.reveal(workspace: workspace, session: session, owner: owner) else { return }
        Task { [sessions] in
            for _ in 0..<Self.revealAttempts {
                if let store = sessions.existing(workspace)?.store, store.connected {
                    store.show(section: target.section)
                    if let event = target.event, case .failure(let error) = await store.send(event) {
                        sessions.existing(workspace)?.banner.show(error.message)
                    }
                    if target.focusMain { store.requestFocus(section: target.section, slot: "\(target.section).main") }
                    return
                }
                try? await Task.sleep(for: .milliseconds(100))
            }
        }
    }
}
