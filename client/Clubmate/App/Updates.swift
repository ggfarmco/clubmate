import AppKit
import Foundation
import Observation

import Sparkle

import ClubmateKit

@MainActor
@Observable
final class Updates: NSObject {
    private(set) var canCheck = false

    @ObservationIgnored private let supervisor: ConnectionSupervisor
    @ObservationIgnored private var controller: SPUStandardUpdaterController?
    @ObservationIgnored private var observation: NSKeyValueObservation?
    @ObservationIgnored private var coreProtocol: Int?
    @ObservationIgnored private var liveSessions: Int?

    init(supervisor: ConnectionSupervisor) {
        self.supervisor = supervisor
        super.init()
        guard let key = Bundle.main.object(forInfoDictionaryKey: "SUPublicEDKey") as? String, !key.isEmpty else { return }
        let controller = SPUStandardUpdaterController(startingUpdater: true, updaterDelegate: self, userDriverDelegate: nil)
        self.controller = controller
        observation = controller.updater.observe(\.canCheckForUpdates, options: [.initial, .new]) { [weak self] updater, _ in
            MainActor.assumeIsolated { self?.canCheck = updater.canCheckForUpdates }
        }
    }

    func check() {
        controller?.checkForUpdates(nil)
    }

    private func refreshCore() {
        coreProtocol = nil
        liveSessions = nil
        guard let client = supervisor.client else { return }
        Task {
            coreProtocol = try? await client.ping().sessionProtocol
            liveSessions = try? await client.liveSessionCount()
        }
    }

    private static func sessionProtocol(of item: SUAppcastItem) -> Int? {
        (item.propertiesDictionary["clubmate:sessionProtocol"] as? String).flatMap { Int($0) }
    }

    private static func confirmInstall(sessionsKnown: Bool) -> Bool {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "This update can't keep running terminals and editors. Install anyway?"
        if !sessionsKnown {
            alert.informativeText = "Running terminals and editors couldn't be checked."
        }
        alert.addButton(withTitle: "Continue")
        alert.addButton(withTitle: "Cancel")
        NSApp.activate()
        return alert.runModal() == .alertFirstButtonReturn
    }
}

extension Updates: SPUUpdaterDelegate {
    func updater(_ updater: SPUUpdater, mayPerform updateCheck: SPUUpdateCheck) throws {
        refreshCore()
    }

    func updater(_ updater: SPUUpdater, shouldProceedWithUpdate item: SUAppcastItem, updateCheck: SPUUpdateCheck) throws {
        guard UpdateGuard.needsConfirmation(
            itemProtocol: Self.sessionProtocol(of: item),
            coreProtocol: coreProtocol,
            liveSessions: liveSessions
        ) else { return }
        guard Self.confirmInstall(sessionsKnown: liveSessions != nil) else {
            throw NSError(domain: SUSparkleErrorDomain, code: Int(SUError.installationCanceledError.rawValue))
        }
    }
}
