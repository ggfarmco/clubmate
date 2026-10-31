import AppKit
import Foundation

import ClubmateKit

@MainActor
enum CLIInstaller {
    private static let userCanceled = -128

    static func install() {
        guard let resources = Bundle.main.resourceURL else { return }
        let tool = CommandLineTool(flavor: ClaudeConnector.bundleFlavor(), resources: resources)
        Task {
            switch tool.state() {
            case .installed:
                return AppAlert.inform("“\(tool.name)” is already installed", detail: "Run “\(tool.name) .” in a terminal to open a folder in Clubmate.")
            case .other(let existing):
                guard await Confirmation.ask("Replace \(tool.link)?", detail: "It currently points to \(existing).", action: "Replace", destructive: true) else { return }
            case .missing:
                break
            }
            var error: NSDictionary?
            NSAppleScript(source: tool.installScript)?.executeAndReturnError(&error)
            if let error {
                if error[NSAppleScript.errorNumber] as? Int != userCanceled {
                    AppAlert.warning(error[NSAppleScript.errorMessage] as? String ?? "Could not install \(tool.name).")
                }
                return
            }
            AppAlert.inform("Installed “\(tool.name)”", detail: "Run “\(tool.name) .” in a terminal to open a folder in Clubmate.")
        }
    }
}
