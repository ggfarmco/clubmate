#if DEBUG
import Foundation
import UIKit

import CompanionKit

@MainActor
enum DebugLaunch {
    static func transport() -> (any HTTPTransport)? {
        UserDefaults.standard.string(forKey: "fixtures").map { FixtureTransport(directory: URL(filePath: $0)) }
    }

    static func apply(model: CompanionModel, navigation: AppNavigation, pairing: PairingFlow) async {
        let defaults = UserDefaults.standard
        if defaults.string(forKey: "orientation") == "landscape" {
            for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
                scene.requestGeometryUpdate(.iOS(interfaceOrientations: .landscapeRight))
            }
        }
        if let link = defaults.string(forKey: "pair") {
            pairing.received(link, from: .link, model: model, navigation: navigation)
        }
        for _ in 0..<50 where model.computers.isEmpty {
            try? await Task.sleep(for: .milliseconds(100))
        }
        guard let target = defaults.string(forKey: "open"), let computer = model.computers.first else { return }
        if target == "sidebar" {
            try? await Task.sleep(for: .seconds(1))
            navigation.sidebar = nil
            navigation.preferredColumn = .sidebar
            return
        }
        let parts = target.split(separator: "/", maxSplits: 1).map(String.init)
        if let workspace = defaults.string(forKey: "workspace") {
            navigation.workspaces[computer.id] = workspace
        }
        for _ in 0..<50 where model.connection(computer.id)?.state != .online {
            try? await Task.sleep(for: .milliseconds(100))
        }
        navigation.open(computer: computer.id, plugin: parts[0], detail: parts.count > 1 ? parts[1] : nil)
    }
}
#endif
