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
    private static let saveDelay = Duration.milliseconds(500)

    private(set) var hub: NotificationHub
    private(set) var waiting: [AgentAlert] = []
    private(set) var systemOff = false

    @ObservationIgnored private var notifier = AgentNotifier()
    @ObservationIgnored private var listener: Task<Void, Never>?
    @ObservationIgnored private var checks: Task<Void, Never>?
    @ObservationIgnored private var saving: Task<Void, Never>?
    @ObservationIgnored private var asked = false
    @ObservationIgnored private let store: URL
    @ObservationIgnored private let run: Run
    @ObservationIgnored private let focus: @MainActor () -> HubFocus
    @ObservationIgnored private let workspaces: @MainActor () -> Set<String>
    @ObservationIgnored private let workspaceName: @MainActor (String) -> String?
    @ObservationIgnored private let reveal: @MainActor (String, String?, String?) -> Void

    init(
        store: URL,
        run: @escaping Run,
        focus: @escaping @MainActor () -> HubFocus,
        workspaces: @escaping @MainActor () -> Set<String>,
        workspaceName: @escaping @MainActor (String) -> String?,
        reveal: @escaping @MainActor (String, String?, String?) -> Void
    ) {
        var inbox = NotificationInbox.load(from: store)
        inbox.prune(now: Date())
        hub = NotificationHub(inbox: inbox)
        self.store = store
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
        NotificationCenter.default.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.refreshSystemState() }
        }
        refreshSystemState()
        updateBadge()
    }

    func unread(_ workspace: String) -> Int {
        hub.unread(workspace)
    }

    var pinned: [InboxPin] {
        hub.inbox.pinned(waiting: waiting)
    }

    func adopt(_ client: CoreClient) {
        listener?.cancel()
        let stream = client.subscribeNotifications()
        let after = hub.inbox.lastSeq > 0 ? hub.inbox.lastSeq : .max
        listener = Task { [weak self] in
            guard let agents = try? await client.busSubscribe(pattern: AgentNotifier.changedTopic, after: .max),
                  let posted = try? await client.busSubscribe(pattern: HubNotification.topic, after: after) else { return }
            self?.checkOpenWorkspaces()
            for await notification in stream where notification.method == "bus.event" {
                guard let params = notification.params.objectValue, let event = params["event"]?.objectValue, let payload = event["payload"] else { continue }
                switch params["subscription"]?.stringValue {
                case posted:
                    self?.advance(event["seq"])
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
        guard let front = focus().front, hub.unread(front) > 0 else { return }
        hub.seen(front)
        changed()
    }

    func markRead(_ id: String) {
        hub.inbox.markRead(id)
        changed()
    }

    func markAllRead() {
        hub.inbox.markAllRead()
        changed()
    }

    func open(_ entry: InboxEntry) {
        markRead(entry.id)
        reveal(entry.note.workspace, entry.note.session, entry.note.owner)
    }

    func open(_ alert: AgentAlert) {
        reveal(alert.workspace, alert.session, AgentNotifier.section)
    }

    func keep(_ workspaces: Set<String>) {
        hub.keep(workspaces)
        waiting.removeAll { !workspaces.contains($0.workspace) }
        changed()
    }

    func openSystemSettings() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension?id=\(Bundle.main.bundleIdentifier ?? "")") else { return }
        NSWorkspace.shared.open(url)
    }

    private func advance(_ seq: JSONValue?) {
        guard case .number(let value) = seq else { return }
        hub.inbox.advance(to: Int64(value))
        changed()
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
        let scope = { (alert: AgentAlert) in alert.workspace == workspace && (session == nil || alert.session == session) }
        guard !focus().shows(workspace: workspace, session: session) || waiting.contains(where: scope),
              case .success(let listed) = await run(workspace, CommandInvocation(id: AgentNotifier.sessionsCommand)) else { return }
        let sessions = AgentSessionState.list(listed).filter { session == nil || $0.session == session }
        waiting.removeAll(where: scope)
        waiting += AgentNotifier.requests(sessions)
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
        let banner = hub.receive(note, focus: focus())
        changed()
        return banner
    }

    private func deliver(_ note: HubNotification) async {
        let center = UNUserNotificationCenter.current()
        let status = await Self.status(center)
        switch SystemNotifications.step(status, asked: asked) {
        case .off:
            systemOff = true
            return
        case .request:
            asked = true
            let granted = (try? await center.requestAuthorization(options: [.alert, .sound])) ?? false
            systemOff = !granted
            guard granted else { return }
        case .deliver:
            systemOff = false
        }
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

    private func refreshSystemState() {
        Task {
            switch await Self.status(UNUserNotificationCenter.current()) {
            case .allowed: systemOff = false
            case .denied: systemOff = true
            case .notDetermined: systemOff = asked
            }
        }
    }

    private static func status(_ center: UNUserNotificationCenter) async -> SystemNotifications.Status {
        switch await center.notificationSettings().authorizationStatus {
        case .notDetermined: .notDetermined
        case .denied: .denied
        default: .allowed
        }
    }

    private func changed() {
        updateBadge()
        saving?.cancel()
        let inbox = hub.inbox
        let store = store
        saving = Task {
            guard (try? await Task.sleep(for: Self.saveDelay)) != nil else { return }
            try? inbox.save(to: store)
        }
    }

    func flush() {
        saving?.cancel()
        try? hub.inbox.save(to: store)
    }

    private func updateBadge() {
        NSApp.dockTile.badgeLabel = hub.total > 0 ? "\(hub.total)" : nil
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        guard let workspace = info[Self.workspaceKey] as? String else { return }
        let session = info[Self.sessionKey] as? String
        let owner = info[Self.ownerKey] as? String
        await markRead(response.notification.request.identifier)
        await reveal(workspace, session, owner)
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .sound]
    }
}
