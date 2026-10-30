import SwiftUI
import UIKit

import CompanionKit

@main
struct CompanionApp: App {
    @State private var model: CompanionModel
    @State private var navigation = AppNavigation()
    @State private var pairing = PairingFlow()
    @Environment(\.scenePhase) private var scenePhase

    init() {
        let support = URL.applicationSupportDirectory.appending(path: "Companion", directoryHint: .isDirectory)
        let device = DeviceIdentity.stable(defaults: .standard, name: UIDevice.current.name, model: UIDevice.current.model)
        let store = ComputerStore(file: support.appending(path: "computers.json"), vault: KeychainTokenVault())
        #if DEBUG
        let policy = HostPolicy.tailnetAndLoopback
        let transport: any HTTPTransport = DebugLaunch.transport() ?? URLSessionTransport()
        #else
        let policy = HostPolicy.tailnet
        let transport = URLSessionTransport()
        #endif
        _model = State(initialValue: CompanionModel(store: store, transport: transport, device: device, policy: policy))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(model)
                .environment(navigation)
                .environment(pairing)
                .onOpenURL { url in
                    guard PairingLink.isPairingURL(url) else { return }
                    pairing.received(url.absoluteString, from: .link, model: model, navigation: navigation)
                }
                .task {
                    model.start()
                    #if DEBUG
                    await DebugLaunch.apply(model: model, navigation: navigation, pairing: pairing)
                    #endif
                }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { model.reconnectAll() }
        }
    }
}
