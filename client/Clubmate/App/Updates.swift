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
    @ObservationIgnored private var core: CoreState?

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
        core = nil
        guard let client = supervisor.client else { return }
        Task {
            guard let ping = try? await client.ping(), let live = try? await client.liveSessionCount() else { return }
            core = CoreState(sessionProtocol: ping.sessionProtocol ?? 0, liveSessions: live)
        }
    }

    private static func sessionProtocol(of item: SUAppcastItem) -> Int? {
        (item.propertiesDictionary["clubmate:sessionProtocol"] as? String).flatMap { Int($0) }
    }

    private static func confirmInstall() -> Bool {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "This update can't keep running terminals and editors. Install anyway?"
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
        guard let core, UpdateGuard.needsConfirmation(
            itemProtocol: Self.sessionProtocol(of: item),
            coreProtocol: core.sessionProtocol,
            liveSessions: core.liveSessions
        ) else { return }
        guard Self.confirmInstall() else {
            throw NSError(domain: SUSparkleErrorDomain, code: Int(SUError.installationCanceledError.rawValue))
        }
    }
}

private struct CoreState {
    let sessionProtocol: Int
    let liveSessions: Int
}
