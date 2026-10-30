import AppKit
import Observation
import UserNotifications

import ClubmateKit

@MainActor
@Observable
final class Notifications: NSObject, UNUserNotificationCenterDelegate {
    typealias Run = @MainActor (String, CommandInvocation) async -> Result<JSONValue, ClubmateError>

    nonisolated private static let workspaceKey = "workspace"
    nonisolated private static let sessionKey = "session"
    nonisolated private static let ownerKey = "owner"

    private(set) var hub = NotificationHub()

    @ObservationIgnored private var notifier = AgentNotifier()
    @ObservationIgnored private var listener: Task<Void, Never>?
    @ObservationIgnored private var checks: Task<Void, Never>?
    @ObservationIgnored private let run: Run
    @ObservationIgnored private let focus: @MainActor () -> HubFocus
    @ObservationIgnored private let workspaces: @MainActor () -> Set<String>
    @ObservationIgnored private let workspaceName: @MainActor (String) -> String?
    @ObservationIgnored private let reveal: @MainActor (String, String?, String?) -> Void

    init(
        run: @escaping Run,
        focus: @escaping @MainActor () -> HubFocus,
        workspaces: @escaping @MainActor () -> Set<String>,
        workspaceName: @escaping @MainActor (String) -> String?,
        reveal: @escaping @MainActor (String, String?, String?) -> Void
    ) {
        self.run = run
        self.focus = focus
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
        for name in [NSApplication.didBecomeActiveNotification, NSWindow.didBecomeKeyNotification] {
            NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.markSeen() }
            }
        }
    }

    func unread(_ workspace: String) -> Int {
        hub.unread[workspace] ?? 0
    }

    func adopt(_ client: CoreClient) {
        listener?.cancel()
        let stream = client.subscribeNotifications()
        listener = Task { [weak self] in
            guard let agents = try? await client.busSubscribe(pattern: AgentNotifier.changedTopic, after: .max),
                  let posted = try? await client.busSubscribe(pattern: HubNotification.topic, after: .max) else { return }
            for await notification in stream where notification.method == "bus.event" {
                guard let params = notification.params.objectValue, let payload = params["event"]?.objectValue?["payload"] else { continue }
                switch params["subscription"]?.stringValue {
                case posted:
                    if let note = HubNotification.posted(payload) { await self?.post(note) }
                case agents:
                    guard let fields = payload.objectValue, fields["kind"]?.stringValue == "status",
                          let workspace = fields["workspace"]?.stringValue else { continue }
                    await self?.checkAgents(workspace, session: fields["session"]?.stringValue)
                default:
                    continue
                }
            }
        }
    }

    func markSeen() {
        guard let front = focus().front, hub.unread[front] != nil else { return }
        hub.seen(front)
        updateBadge()
    }

    func keep(_ workspaces: Set<String>) {
        hub.keep(workspaces)
        updateBadge()
    }

    private func checkOpenWorkspaces() {
        let previous = checks
        let open = workspaces()
        checks = Task { [weak self] in
            await previous?.value
            for workspace in open.sorted() {
                await self?.checkAgents(workspace, session: nil)
            }
        }
    }

    private func checkAgents(_ workspace: String, session: String?) async {
        guard !focus().shows(workspace: workspace, session: session),
              case .success(let listed) = await run(workspace, CommandInvocation(id: AgentNotifier.sessionsCommand)) else { return }
        let sessions = AgentSessionState.list(listed).filter { session == nil || $0.session == session }
        let current = focus()
        for alert in notifier.pending(sessions, shown: { current.shows(workspace: $0.workspace, session: $0.session) }) where !notifier.isPosted(alert) {
            let note = HubNotification(alert)
            guard accept(note) else { continue }
            notifier.markPosted(alert)
            await deliver(note)
        }
    }

    private func post(_ note: HubNotification) async {
        guard accept(note) else { return }
        await deliver(note)
    }

    private func accept(_ note: HubNotification) -> Bool {
        guard hub.receive(note, focus: focus()) else { return false }
        updateBadge()
        return true
    }

    private func deliver(_ note: HubNotification) async {
        let center = UNUserNotificationCenter.current()
        guard (try? await center.requestAuthorization(options: [.alert, .sound])) == true else { return }
        let content = UNMutableNotificationContent()
        content.title = note.headline(workspaceName: workspaceName(note.workspace))
        content.subtitle = note.subtitle
        content.body = note.message
        content.sound = .default
        content.threadIdentifier = note.session ?? note.workspace
        var info = [Self.workspaceKey: note.workspace]
        info[Self.sessionKey] = note.session
        info[Self.ownerKey] = note.owner
        content.userInfo = info
        try? await center.add(UNNotificationRequest(identifier: note.id, content: content, trigger: nil))
    }

    private func updateBadge() {
        NSApp.dockTile.badgeLabel = hub.total > 0 ? "\(hub.total)" : nil
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        guard let workspace = info[Self.workspaceKey] as? String else { return }
        let session = info[Self.sessionKey] as? String
        let owner = info[Self.ownerKey] as? String
        await reveal(workspace, session, owner)
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .sound]
    }
}
