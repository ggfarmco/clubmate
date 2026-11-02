import UIKit
import UserNotifications

import CompanionKit

@MainActor
final class LocalNotifier: NSObject, UNUserNotificationCenterDelegate {
    private let center = UNUserNotificationCenter.current()
    private var opened: ((_ computer: String, _ workspace: String, _ session: String?) -> Void)?
    private var background: UIBackgroundTaskIdentifier = .invalid
    private weak var model: CompanionModel?

    func attach(model: CompanionModel, navigation: AppNavigation) {
        center.delegate = self
        self.model = model
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
            let connection = model.connection(item.computerID)
            let name = connection?.computer.name ?? model.computers.first { $0.id == item.computerID }?.name ?? ""
            let workspace = connection?.workspaces.first { $0.id == item.workspace }?.name
            guard let alert = LocalAlert(item, computerName: name, workspaceName: workspace, now: .now) else { return }
            post(alert)
        }
        model.withdrawn = { [weak self] ids in
            self?.center.removePendingNotificationRequests(withIdentifiers: ids)
            Task { [weak self] in
                guard let self else { return }
                center.removeDeliveredNotifications(withIdentifiers: DeliveredNotice.withdrawn(Set(ids), from: await center.delivered()))
            }
        }
        if !model.computers.isEmpty { requestAuthorization() }
    }

    func requestAuthorization() {
        #if DEBUG
        if UserDefaults.standard.string(forKey: "fixtures") != nil { return }
        #endif
        Task {
            guard (try? await center.requestAuthorization(options: [.alert, .sound, .badge])) == true else { return }
            UIApplication.shared.registerForRemoteNotifications()
        }
    }

    func enteredBackground() {
        guard background == .invalid else { return }
        background = UIApplication.shared.beginBackgroundTask(withName: "Clubmate events") { [weak self] in
            MainActor.assumeIsolated { self?.expire() }
        }
    }

    func expire() {
        guard background != .invalid else { return }
        model?.disconnectAll()
        endBackground()
    }

    func endBackground() {
        guard background != .invalid else { return }
        UIApplication.shared.endBackgroundTask(background)
        background = .invalid
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification, withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        let remote = notification.request.trigger is UNPushNotificationTrigger
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async {
            let active = MainActor.assumeIsolated { UIApplication.shared.applicationState == .active }
            done(remote && active ? [] : [.banner, .list, .sound])
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let computer = info["computer"] as? String
        let workspace = info["workspace"] as? String ?? ""
        let session = info["session"] as? String
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                if let computer { self.opened?(computer, workspace, session) }
            }
            done()
        }
    }

    private func post(_ alert: LocalAlert) {
        let content = UNMutableNotificationContent()
        content.title = alert.title
        content.subtitle = alert.subtitle
        content.body = alert.body
        content.sound = .default
        content.threadIdentifier = alert.computerID
        var info: [String: String] = ["id": alert.id, "computer": alert.computerID, "workspace": alert.workspace, "kind": alert.kind.rawValue]
        if let session = alert.session { info["session"] = session }
        content.userInfo = info
        center.add(UNNotificationRequest(identifier: alert.id, content: content, trigger: nil)) { _ in }
    }
}

extension UNUserNotificationCenter {
    @MainActor func delivered() async -> [DeliveredNotice] {
        await deliveredNotifications().map { DeliveredNotice(identifier: $0.request.identifier, userInfo: $0.request.content.userInfo) }
    }
}
