import AppKit
import Foundation
import UniformTypeIdentifiers

import ClubmateKit

extension AppModel {
    func collectDiagnostics() {
        guard let client = supervisor.client else {
            let tool = CommandLineTool.name(flavor: ClaudeConnector.bundleFlavor())
            return Self.alert("Clubmate is not connected to its core. Run “\(tool) diagnostics” in a terminal to collect its logs.")
        }
        let (projectID, window) = keyWorkspace
        let banner = projectID.flatMap(existingSession)?.banner
        let report: (String) -> Void = { message in
            guard let banner else { return Self.alert(message) }
            banner.show(message)
        }
        Task {
            guard let screenshot = await DiagnosticsSheet.ask(window: window) else { return }
            var extra: [String: Data] = [:]
            if let id = Bundle.main.bundleIdentifier, let domain = UserDefaults.standard.persistentDomain(forName: id) {
                extra[Diagnostics.settingsName] = Diagnostics.redactedSettings(domain)
            }
            if screenshot, let png = window.flatMap(WindowSnapshot.png), png.count <= Diagnostics.extraLimit {
                extra[Diagnostics.windowName] = png
            }
            let result: DiagnosticsResult
            do {
                result = try await client.collectDiagnostics(projectID: projectID, appVersion: Bundle.main.clubmateVersion, extra: extra)
            } catch {
                return report((error as? ClubmateError)?.message ?? "\(error)")
            }
            let source = URL(filePath: result.path)
            guard let target = DiagnosticsSheet.destination() else {
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
private final class DiagnosticsSheet: NSObject {
    private let list: NSTextField
    private let checkbox: NSButton
    let view: NSView

    override init() {
        list = NSTextField(wrappingLabelWithString: Diagnostics.summary(screenshot: true))
        checkbox = NSButton(checkboxWithTitle: Diagnostics.screenshotOption, target: nil, action: nil)
        checkbox.state = .off
        let width = max(320, checkbox.fittingSize.width)
        list.preferredMaxLayoutWidth = width
        let listHeight = list.fittingSize.height
        let boxHeight = checkbox.fittingSize.height
        let gap: CGFloat = 12
        view = NSView(frame: NSRect(x: 0, y: 0, width: width, height: listHeight + gap + boxHeight))
        checkbox.frame = NSRect(x: 0, y: 0, width: width, height: boxHeight)
        list.frame = NSRect(x: 0, y: boxHeight + gap, width: width, height: listHeight)
        list.stringValue = Diagnostics.summary(screenshot: false)
        view.addSubview(list)
        view.addSubview(checkbox)
        super.init()
        checkbox.target = self
        checkbox.action = #selector(toggled)
    }

    @objc private func toggled() {
        list.stringValue = Diagnostics.summary(screenshot: checkbox.state == .on)
    }

    static func ask(window: NSWindow?) async -> Bool? {
        let sheet = DiagnosticsSheet()
        let alert = NSAlert()
        alert.messageText = Diagnostics.title
        alert.accessoryView = sheet.view
        alert.addButton(withTitle: "Cancel")
        alert.addButton(withTitle: "Save…")
        let response: NSApplication.ModalResponse
        if let window = window ?? NSApp.keyWindow {
            response = await alert.beginSheetModal(for: window)
        } else {
            response = alert.runModal()
        }
        guard response == .alertSecondButtonReturn else { return nil }
        return sheet.checkbox.state == .on
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
