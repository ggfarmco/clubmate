import AppKit
import Foundation
import UniformTypeIdentifiers

import ClubmateKit

extension AppModel {
    func collectDiagnostics() {
        guard let client = supervisor.client else {
            return Self.alert("Clubmate is not connected to its core yet.")
        }
        let (projectID, window) = keyWorkspace
        let banner = projectID.flatMap(existingSession)?.banner
        let report: (String) -> Void = { message in
            guard let banner else { return Self.alert(message) }
            banner.show(message)
        }
        var extra: [String: Data] = [:]
        if let id = Bundle.main.bundleIdentifier, let domain = UserDefaults.standard.persistentDomain(forName: id) {
            extra[Diagnostics.settingsName] = Diagnostics.redactedSettings(domain)
        }
        if let png = window.flatMap(WindowSnapshot.png), png.count <= Diagnostics.extraLimit {
            extra[Diagnostics.windowName] = png
        }
        Task {
            let result: DiagnosticsResult
            do {
                result = try await client.collectDiagnostics(projectID: projectID, appVersion: Bundle.main.clubmateVersion, extra: extra)
            } catch {
                return report((error as? ClubmateError)?.message ?? "\(error)")
            }
            let source = URL(filePath: result.path)
            guard await DiagnosticsSheet.confirm(result.files, window: window), let target = DiagnosticsSheet.destination() else {
                try? FileManager.default.removeItem(at: source)
                return
            }
            do {
                if FileManager.default.fileExists(atPath: target.path(percentEncoded: false)) {
                    _ = try FileManager.default.replaceItemAt(target, withItemAt: source)
                } else {
                    try FileManager.default.moveItem(at: source, to: target)
                }
                NSWorkspace.shared.activateFileViewerSelecting([target])
            } catch {
                report("Could not save diagnostics: \(error.localizedDescription)")
            }
        }
    }
}

@MainActor
private enum DiagnosticsSheet {
    static func confirm(_ files: [DiagnosticsFile], window: NSWindow?) async -> Bool {
        let alert = NSAlert()
        alert.messageText = Diagnostics.title
        alert.informativeText = Diagnostics.summary(files)
        alert.addButton(withTitle: "Cancel")
        alert.addButton(withTitle: "Save…")
        guard let window = window ?? NSApp.keyWindow else {
            return alert.runModal() == .alertSecondButtonReturn
        }
        return await alert.beginSheetModal(for: window) == .alertSecondButtonReturn
    }

    static func destination() -> URL? {
        let panel = NSSavePanel()
        panel.directoryURL = FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first
        panel.nameFieldStringValue = Diagnostics.archiveName(Date())
        panel.allowedContentTypes = [.zip]
        panel.canCreateDirectories = true
        guard panel.runModal() == .OK else { return nil }
        return panel.url
    }
}

@MainActor
private enum WindowSnapshot {
    static func png(_ window: NSWindow) -> Data? {
        guard let root = window.contentView, root.bounds.width > 0, root.bounds.height > 0,
              let rep = root.bitmapImageRepForCachingDisplay(in: root.bounds) else { return nil }
        root.cacheDisplay(in: root.bounds, to: rep)
        return rep.representation(using: .png, properties: [:])
    }
}
