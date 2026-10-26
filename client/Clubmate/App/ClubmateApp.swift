import GhosttyKit
import SwiftUI

@main
struct ClubmateApp: App {
    init() {
        if ghostty_init(UInt(CommandLine.argc), CommandLine.unsafeArgv) != GHOSTTY_SUCCESS {
            fatalError("ghostty_init failed")
        }
    }

    var body: some Scene {
        Window("Clubmate", id: "main") {
            Color.clear.frame(minWidth: 800, minHeight: 500)
        }
    }
}
