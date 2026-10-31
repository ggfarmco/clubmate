import AppKit
import Foundation
import OSLog
import UniformTypeIdentifiers

import ClubmateKit

extension AppModel {
    func collectDiagnostics() {
        guard let client = supervisor.client else {
            let tool = CommandLineTool.name(flavor: ClaudeConnector.bundleFlavor())
            return AppAlert.warning("Clubmate is not connected to its core. Run “\(tool) diagnostics” in a terminal to collect its logs.")
        }
        let (projectID, window) = keyWorkspace
        let banner = projectID.flatMap(existingSession)?.banner
        let report: (String) -> Void = { message in
            guard let banner else { return AppAlert.warning(message) }
            banner.show(message)
        }
        Task {
            guard let screenshot = await DiagnosticsSheet.ask(window: window) else { return }
            var extra: [String: Data] = [:]
            if let id = Bundle.main.bundleIdentifier, let domain = UserDefaults.standard.persistentDomain(forName: id) {
                extra[Diagnostics.settingsName] = Diagnostics.redactedSettings(domain)
            }
            if let log = AppLog.recent() {
                extra[Diagnostics.appLogName] = log
            }
            if screenshot, let png = window.flatMap { WindowImage.png($0) }, png.count <= Diagnostics.extraLimit {
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
        if let window = window ?? NSApp.promptWindow {
            response = await alert.beginSheetModal(for: window)
        } else if TestMode.current {
            return nil
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
        guard !TestMode.current, panel.runModal() == .OK else { return nil }
        return panel.url
    }
}

private enum AppLog {
    static func recent() -> Data? {
        guard let store = try? OSLogStore(scope: .currentProcessIdentifier),
              let entries = try? store.getEntries(at: store.position(date: Date(timeIntervalSinceNow: -3600)), matching: NSPredicate(format: "subsystem == %@", Diagnostics.logSubsystem)) else { return nil }
        let lines = entries.compactMap { $0 as? OSLogEntryLog }.map { "\($0.date.ISO8601Format(Date.ISO8601FormatStyle(includingFractionalSeconds: true))) [\($0.category)] \($0.composedMessage)\n" }
        guard !lines.isEmpty else { return nil }
        return Data(lines.joined().utf8).suffix(Diagnostics.extraLimit)
    }
}
