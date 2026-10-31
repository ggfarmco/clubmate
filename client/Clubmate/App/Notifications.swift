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
    private(set) var systemOff = false

    @ObservationIgnored private var notifier = AgentNotifier()
    @ObservationIgnored private var listener: Task<Void, Never>?
    @ObservationIgnored private var checks: Task<Void, Never>?
    @ObservationIgnored private var saving: Task<Void, Never>?
    @ObservationIgnored private var generation: UInt64 = 0
    @ObservationIgnored private var asked = false
    @ObservationIgnored private let writer: InboxWriter
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
        writer = InboxWriter(url: store)
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
                MainActor.assumeIsolated { self?.focusChanged() }
            }
        }
        NotificationCenter.default.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.refreshSystemState() }
        }
        refreshSystemState()
        updateBadge()
    }

    func unseen(_ workspace: String) -> Int {
        hub.unseen(workspace)
    }

    func adopt(_ client: CoreClient) {
        listener?.cancel()
        let stream = client.subscribeNotifications()
        let lastSeq = hub.inbox.lastSeq
        listener = Task { [weak self] in
            let after = await Self.resumePoint(client, lastSeq: lastSeq)
            if after == .max { self?.hub.inbox.forgetSeq() }
            guard let agents = try? await client.busSubscribe(pattern: Topic.agentsChanged, after: .max),
                  let posted = try? await client.busSubscribe(pattern: Topic.notifyPosted, after: after) else { return }
            self?.checkOpenWorkspaces()
            for await notification in stream where notification.method == "bus.event" {
                guard let params = notification.params.objectValue, let event = params["event"]?.objectValue, let payload = event["payload"] else { continue }
                switch params["subscription"]?.stringValue {
                case posted:
                    self?.advance(event["seq"])
                    if let note = HubNotification.posted(payload) { await self?.post(note) }
                case agents:
                    guard let fields = payload.objectValue, let workspace = fields["workspace"]?.stringValue else { continue }
                    switch fields["kind"]?.stringValue {
                    case "status":
                        await self?.checkAgents(workspace, session: fields["session"]?.stringValue)
                    case "deleted":
                        if let session = fields["session"]?.stringValue { self?.agentDeleted(workspace, session) }
                    default:
                        continue
                    }
                default:
                    continue
                }
            }
        }
    }

    func focusChanged() {
        let before = hub.inbox
        hub.focused(focus())
        if hub.inbox != before { changed() }
    }

    func handle(_ id: String) {
        hub.inbox.handle(id)
        changed()
    }

    func handleAll() {
        hub.inbox.handleAll()
        changed()
    }

    func open(_ entry: InboxEntry) {
        handle(entry.id)
        reveal(entry.note.workspace, entry.note.session, entry.note.owner)
    }

    func open(_ alert: AgentAlert) {
        handle(alert.id)
        reveal(alert.workspace, alert.session, AgentNotifier.section)
    }

    func keep(_ workspaces: Set<String>) {
        hub.keep(workspaces)
        changed()
    }

    func keepOpen(_ workspaces: Set<String>) {
        hub.waiting.keep(workspaces)
    }

    private func agentDeleted(_ workspace: String, _ session: String) {
        hub.waiting.remove(workspace: workspace, session: session)
    }

    private static func resumePoint(_ client: CoreClient, lastSeq: Int64) async -> Int64 {
        guard lastSeq > 0 else { return .max }
        let read: Result<JSONValue, any Error>
        do {
            read = .success(try await client.call(method: "bus.read", params: .object(["pattern": .string(Topic.notifyPosted), "after": .number(Double(lastSeq - 1)), "limit": .number(1)])))
        } catch {
            read = .failure(error)
        }
        return NotificationInbox.resumePoint(lastSeq: lastSeq, read: read)
    }

    func openSystemSettings() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension?id=\(Bundle.main.bundleIdentifier ?? "")") else { return }
        NSWorkspace.shared.open(url)
    }

    private func advance(_ seq: JSONValue?) {
        guard case .number(let value) = seq else { return }
        hub.inbox.record(seq: Int64(value))
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
        guard !focus().shows(workspace: workspace, session: session) || hub.waiting.has(workspace: workspace, session: session),
              case .success(let listed) = await run(workspace, CommandInvocation(id: AgentCommand.sessions)) else { return }
        let sessions = AgentSession.list(listed).filter { session == nil || $0.id == session }
        hub.waiting.update(workspace: workspace, session: session, sessions: sessions)
        let current = focus()
        for alert in notifier.pending(sessions, shown: { current.shows(workspace: $0.workspace, session: $0.id) }) where !notifier.isPosted(alert) {
            let note = HubNotification(alert)
            guard accept(note, policy: sessions.first { $0.id == alert.session }?.notifyEffective ?? .all) else { continue }
            notifier.markPosted(alert)
            await deliver(note)
        }
    }

    private func post(_ note: HubNotification) async {
        guard accept(note, policy: await policy(note)) else { return }
        await deliver(note)
    }

    private func policy(_ note: HubNotification) async -> NotificationPolicy {
        guard note.isAgentSession, let session = note.session,
              case .success(let listed) = await run(note.workspace, CommandInvocation(id: AgentCommand.sessions)) else { return .all }
        return NotificationPolicy(session: session, in: listed)
    }

    private func accept(_ note: HubNotification, policy: NotificationPolicy) -> Bool {
        let banner = hub.receive(note, focus: focus(), policy: policy)
        changed()
        return banner
    }

    private func deliver(_ note: HubNotification) async {
        guard !TestMode.current else { return }
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
        generation += 1
        let inbox = hub.inbox
        let generation = generation
        let writer = writer
        saving = Task {
            guard (try? await Task.sleep(for: Self.saveDelay)) != nil else { return }
            await Task.detached { writer.save(inbox, generation: generation) }.value
        }
    }

    func flush() {
        saving?.cancel()
        generation += 1
        writer.save(hub.inbox, generation: generation)
    }

    private func updateBadge() {
        NSApp.dockTile.badgeLabel = hub.unseenTotal > 0 ? "\(hub.unseenTotal)" : nil
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        guard let workspace = info[Self.workspaceKey] as? String else { return }
        let session = info[Self.sessionKey] as? String
        let owner = info[Self.ownerKey] as? String
        await handle(response.notification.request.identifier)
        await reveal(workspace, session, owner)
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .sound]
    }
}
