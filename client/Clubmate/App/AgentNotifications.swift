import AppKit
import UserNotifications

import ClubmateKit

@MainActor
final class AgentNotifications: NSObject, UNUserNotificationCenterDelegate {
    typealias Run = @MainActor (String, CommandInvocation) async -> Result<JSONValue, ClubmateError>

    nonisolated private static let workspaceKey = "workspace"
    nonisolated private static let sessionKey = "session"

    private var notifier = AgentNotifier()
    private var listener: Task<Void, Never>?
    private var checks: Task<Void, Never>?
    private let run: Run
    private let windowKey: @MainActor (String) -> Bool
    private let workspaces: @MainActor () -> Set<String>
    private let workspaceName: @MainActor (String) -> String?
    private let reveal: @MainActor (String, String) -> Void

    init(
        run: @escaping Run,
        windowKey: @escaping @MainActor (String) -> Bool,
        workspaces: @escaping @MainActor () -> Set<String>,
        workspaceName: @escaping @MainActor (String) -> String?,
        reveal: @escaping @MainActor (String, String) -> Void
    ) {
        self.run = run
        self.windowKey = windowKey
        self.workspaces = workspaces
        self.workspaceName = workspaceName
        self.reveal = reveal
        super.init()
        UNUserNotificationCenter.current().delegate = self
        for name in [NSApplication.didResignActiveNotification, NSWindow.didResignKeyNotification] {
            NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.checkOpenWorkspaces() }
            }
        }
    }

    func adopt(_ client: CoreClient) {
        listener?.cancel()
        let stream = client.subscribeNotifications()
        listener = Task { [weak self] in
            guard let subscription = try? await client.busSubscribe(pattern: AgentNotifier.changedTopic, after: .max) else { return }
            for await notification in stream where notification.method == "bus.event" {
                guard let params = notification.params.objectValue, params["subscription"]?.stringValue == subscription,
                      let payload = params["event"]?.objectValue?["payload"]?.objectValue,
                      payload["kind"]?.stringValue == "status", let workspace = payload["workspace"]?.stringValue else { continue }
                await self?.check(workspace, session: payload["session"]?.stringValue)
            }
        }
    }

    private func checkOpenWorkspaces() {
        let previous = checks
        let open = workspaces()
        checks = Task { [weak self] in
            await previous?.value
            for workspace in open.sorted() {
                await self?.check(workspace, session: nil)
            }
        }
    }

    private func check(_ workspace: String, session: String?) async {
        guard AgentNotifier.wantsAttention(windowKey: windowKey(workspace), appActive: NSApp.isActive),
              case .success(let listed) = await run(workspace, CommandInvocation(id: AgentNotifier.sessionsCommand)) else { return }
        let sessions = AgentSessionState.list(listed).filter { session == nil || $0.session == session }
        let alerts = notifier.alerts(sessions, windowKey: windowKey(workspace), appActive: NSApp.isActive)
        guard !alerts.isEmpty else { return }
        let center = UNUserNotificationCenter.current()
        guard (try? await center.requestAuthorization(options: [.alert, .sound])) == true else { return }
        for alert in alerts {
            let content = UNMutableNotificationContent()
            content.title = alert.title
            content.subtitle = workspaceName(alert.workspace) ?? ""
            content.body = alert.body
            content.sound = .default
            content.threadIdentifier = alert.session
            content.userInfo = [Self.workspaceKey: alert.workspace, Self.sessionKey: alert.session]
            try? await center.add(UNNotificationRequest(identifier: "\(alert.session)/\(alert.request)", content: content, trigger: nil))
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        guard let workspace = info[Self.workspaceKey] as? String, let session = info[Self.sessionKey] as? String else { return }
        await reveal(workspace, session)
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .sound]
    }
}
