import AppKit

import ClubmateKit

@MainActor
enum AppAlert {
    static func warning(_ message: String) {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = message
        if !TestMode.current { alert.runModal() }
    }

    static func inform(_ message: String, detail: String) {
        let alert = NSAlert()
        alert.messageText = message
        alert.informativeText = detail
        if !TestMode.current { alert.runModal() }
    }
}
