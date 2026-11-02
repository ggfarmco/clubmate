import OSLog
import UIKit
import UserNotifications

import CompanionKit

@MainActor
final class AppDelegate: NSObject, UIApplicationDelegate {
    private static let log = Logger(subsystem: "com.clubmate.companion", category: "push")

    private var token: PushToken?
    var registered: ((PushToken) -> Void)? {
        didSet { if let token { registered?(token) } }
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        let profile = Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision").flatMap { try? Data(contentsOf: $0) }
        let token = PushToken(device: deviceToken, environment: PushToken.environment(profile: profile))
        self.token = token
        registered?(token)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: any Error) {
        Self.log.error("remote notifications unavailable: \(error.localizedDescription, privacy: .public)")
    }

    func application(_ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any], fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void) {
        let center = UNUserNotificationCenter.current()
        Task {
            guard let ids = DeliveredNotice.resolved(userInfo, from: await center.delivered()), !ids.isEmpty else { return completionHandler(.noData) }
            center.removeDeliveredNotifications(withIdentifiers: ids)
            completionHandler(.newData)
        }
    }
}
