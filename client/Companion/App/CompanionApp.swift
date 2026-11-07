import BackgroundTasks
import SwiftUI
import UIKit

import CompanionKit

@main
struct CompanionApp: App {
    static let refreshTask = "com.clubmate.companion.refresh"
    static let refreshEvery: TimeInterval = 15 * 60

    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var model: CompanionModel
    @State private var navigation: AppNavigation
    @State private var pairing = PairingFlow()
    @State private var notifier: LocalNotifier
    @State private var voice: VoiceSession
    @Environment(\.scenePhase) private var scenePhase

    init() {
        let support = URL.applicationSupportDirectory.appending(path: "Companion", directoryHint: .isDirectory)
        let device = DeviceIdentity.stable(defaults: .standard, name: UIDevice.current.name, model: UIDevice.current.model)
        #if DEBUG
        DebugLaunch.forgetMacs(support.appending(path: "computers.json"))
        #endif
        let store = ComputerStore(file: support.appending(path: "computers.json"), vault: KeychainTokenVault())
        #if DEBUG
        let policy = HostPolicy.tailnetAndLoopback
        let transport: any HTTPTransport = DebugLaunch.transport() ?? URLSessionTransport()
        #else
        let policy = HostPolicy.tailnet
        let transport = URLSessionTransport()
        #endif
        let model = CompanionModel(store: store, transport: transport, device: device, policy: policy, cursors: .standard, queueFile: support.appending(path: "queue.json"))
        let navigation = AppNavigation()
        let notifier = LocalNotifier()
        let voice = VoiceSession(runner: { model.connection($0) }, source: VoiceCapture(), target: { navigation.voiceTarget(model) })
        model.received = { [weak voice] computer, signal in voice?.handle(signal, from: computer) }
        _model = State(initialValue: model)
        _voice = State(initialValue: voice)
        _navigation = State(initialValue: navigation)
        _notifier = State(initialValue: notifier)
        BGTaskScheduler.shared.register(forTaskWithIdentifier: Self.refreshTask, using: .main) { task in
            MainActor.assumeIsolated {
                Self.scheduleRefresh(model)
                notifier.attach(model: model, navigation: navigation)
                let work = Task { @MainActor in
                    await model.catchUp()
                    if UIApplication.shared.applicationState == .background { model.disconnectAll() }
                    task.setTaskCompleted(success: !Task.isCancelled)
                }
                task.expirationHandler = { work.cancel() }
            }
        }
    }

    @MainActor
    static func scheduleRefresh(_ model: CompanionModel) {
        guard !model.computers.isEmpty else { return }
        let request = BGAppRefreshTaskRequest(identifier: refreshTask)
        request.earliestBeginDate = Date(timeIntervalSinceNow: refreshEvery)
        try? BGTaskScheduler.shared.submit(request)
    }

    var body: some Scene {
        WindowGroup {
            #if DEBUG
            if let gallery = DebugLaunch.gallery {
                SDUIGalleryScreen(focus: gallery)
            } else {
                root
            }
            #else
            root
            #endif
        }
        .commands { SessionCommands() }
        .onChange(of: scenePhase) { _, phase in
            switch phase {
            case .active:
                notifier.endBackground()
                model.reconnectAll()
            case .background:
                voice.background(hold: BackgroundHold.begin)
                notifier.enteredBackground()
                Self.scheduleRefresh(model)
            default:
                break
            }
        }
    }

    private var root: some View {
        RootView()
            .environment(model)
            .environment(navigation)
            .environment(pairing)
            .environment(voice)
            .onOpenURL { url in
                guard PairingLink.isPairingURL(url) else { return }
                pairing.received(url.absoluteString, from: .link, model: model, navigation: navigation)
            }
            .onChange(of: pairing.paired) { _, _ in notifier.requestAuthorization() }
            .task {
                delegate.registered = { model.registered($0) }
                notifier.attach(model: model, navigation: navigation)
                model.start()
                #if DEBUG
                await DebugLaunch.apply(model: model, navigation: navigation, pairing: pairing, voice: voice)
                #endif
            }
    }
}

@MainActor
final class BackgroundHold {
    private var task = UIBackgroundTaskIdentifier.invalid

    static func begin() -> @MainActor () -> Void {
        let hold = BackgroundHold()
        hold.task = UIApplication.shared.beginBackgroundTask(withName: "voice.end") { MainActor.assumeIsolated { hold.end() } }
        return { hold.end() }
    }

    private func end() {
        guard task != .invalid else { return }
        UIApplication.shared.endBackgroundTask(task)
        task = .invalid
    }
}
