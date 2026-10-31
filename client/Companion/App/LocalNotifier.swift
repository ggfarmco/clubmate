import UIKit
import UserNotifications

import CompanionKit

@MainActor
final class LocalNotifier: NSObject, UNUserNotificationCenterDelegate {
    private let center = UNUserNotificationCenter.current()
    private var opened: ((_ computer: String, _ workspace: String, _ session: String?) -> Void)?
    private var background: UIBackgroundTaskIdentifier = .invalid

    func attach(model: CompanionModel, navigation: AppNavigation) {
        center.delegate = self
        opened = { computer, workspace, session in
            if let session {
                navigation.workspaces[computer] = workspace
                navigation.open(computer: computer, plugin: AgentsPluginInfo.id, detail: session)
            } else {
                navigation.open(computer: computer, plugin: InboxPluginInfo.id)
            }
        }
        model.posted = { [weak self, weak model] item in
            guard let self, let model, UIApplication.shared.applicationState != .active else { return }
            let name = model.computers.first { $0.id == item.computerID }?.name ?? ""
            guard let alert = LocalAlert(item, computerName: name, now: .now) else { return }
            post(alert)
        }
        if !model.computers.isEmpty { requestAuthorization() }
    }

    func requestAuthorization() {
        Task { _ = try? await center.requestAuthorization(options: [.alert, .sound, .badge]) }
    }

    func enteredBackground() {
        guard background == .invalid else { return }
        background = UIApplication.shared.beginBackgroundTask(withName: "Clubmate events") { [weak self] in
            MainActor.assumeIsolated { self?.endBackground() }
        }
    }

    func endBackground() {
        guard background != .invalid else { return }
        UIApplication.shared.endBackgroundTask(background)
        background = .invalid
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .list, .sound]
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        guard let computer = info["computer"] as? String else { return }
        let workspace = info["workspace"] as? String ?? ""
        let session = info["session"] as? String
        await MainActor.run { opened?(computer, workspace, session) }
    }

    private func post(_ alert: LocalAlert) {
        let content = UNMutableNotificationContent()
        content.title = alert.title
        content.subtitle = alert.subtitle
        content.body = alert.body
        content.sound = .default
        content.threadIdentifier = alert.computerID
        var info: [String: String] = ["computer": alert.computerID, "workspace": alert.workspace]
        if let session = alert.session { info["session"] = session }
        content.userInfo = info
        center.add(UNNotificationRequest(identifier: alert.id, content: content, trigger: nil)) { _ in }
    }
}
