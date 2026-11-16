#if DEBUG
import Foundation
import UIKit
import UserNotifications

import CompanionKit

@MainActor
enum DebugLaunch {
    static var gallery: String? {
        UserDefaults.standard.string(forKey: "sdui-gallery")
    }

    static func dumpDelivered() {
        guard let path = UserDefaults.standard.string(forKey: "deliveredDump") else { return }
        Task { @MainActor in
            while true {
                let rows = await UNUserNotificationCenter.current().deliveredNotifications().map { note -> [String: Any] in
                    let content = note.request.content
                    let info = content.userInfo.reduce(into: [String: String]()) { out, entry in
                        if let key = entry.key as? String, let value = entry.value as? String { out[key] = value }
                    }
                    return ["identifier": note.request.identifier, "push": note.request.trigger is UNPushNotificationTrigger, "title": content.title, "subtitle": content.subtitle, "body": content.body, "userInfo": info]
                }
                let dump: [String: Any] = ["written": Date.now.timeIntervalSince1970, "active": UIApplication.shared.applicationState == .active, "notifications": rows]
                if let data = try? JSONSerialization.data(withJSONObject: dump) {
                    try? data.write(to: URL(filePath: path), options: .atomic)
                }
                try? await Task.sleep(for: .milliseconds(300))
            }
        }
    }

    static func forgetMacs(_ file: URL) {
        let defaults = UserDefaults.standard
        guard defaults.string(forKey: "fixtures") != nil, defaults.string(forKey: "pair") != nil || defaults.string(forKey: "scan") != nil else { return }
        try? FileManager.default.removeItem(at: file)
    }

    static func probe() -> (any NetworkProbe)? {
        guard let mode = UserDefaults.standard.string(forKey: "checkFixture") else { return nil }
        return FixtureProbe(failing: mode == "fail")
    }

    static func transport() -> (any HTTPTransport)? {
        let defaults = UserDefaults.standard
        let offline = defaults.object(forKey: "offline") == nil ? nil : Duration.seconds(defaults.double(forKey: "offline"))
        return defaults.string(forKey: "fixtures").map { FixtureTransport(directory: URL(filePath: $0), offlineAfter: offline) }
    }

    static func apply(model: CompanionModel, navigation: AppNavigation, pairing: PairingFlow, voice: VoiceSession) async {
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
        if defaults.integer(forKey: "macs") > 1, defaults.string(forKey: "fixtures") != nil {
            for mac in [FixtureMac.pro, FixtureMac.air].prefix(defaults.integer(forKey: "macs") - 1) where model.connection(mac.id) == nil {
                if let link = try? PairingLink.parse(mac.link, policy: model.policy) {
                    _ = try? await model.pair(link)
                }
            }
        }
        await navigate(model: model, navigation: navigation)
        if defaults.bool(forKey: "voice"), let computer = model.computers.first {
            for _ in 0..<50 where model.connection(computer.id)?.state != .online {
                try? await Task.sleep(for: .milliseconds(100))
            }
            try? await Task.sleep(for: .seconds(defaults.double(forKey: "voiceDelay")))
            voice.begin(into: defaults.bool(forKey: "voiceDictation") ? navigation.detail : nil)
        }
        if let path = defaults.string(forKey: "doc"), let computer = model.computers.first {
            for _ in 0..<50 where model.connection(computer.id)?.state != .online {
                try? await Task.sleep(for: .milliseconds(100))
            }
            if let connection = model.connection(computer.id), let workspace = defaults.string(forKey: "workspace") ?? navigation.workspace(for: connection)?.id {
                navigation.openDoc(computer: computer.id, workspace: workspace, path: path)
            }
        }
    }

    private static func navigate(model: CompanionModel, navigation: AppNavigation) async {
        let defaults = UserDefaults.standard
        guard let target = defaults.string(forKey: "open"), let computer = model.computers.first else { return }
        if target == "sidebar" || target == "mac" || target == "connection" || target == "report" {
            try? await Task.sleep(for: .seconds(1))
            navigation.sidebar = nil
            navigation.preferredColumn = .sidebar
            if target == "mac" || target == "connection" { navigation.showMac(computer.id) }
            if target == "connection" {
                try? await Task.sleep(for: .seconds(1))
                navigation.connectionSheet = ConnectionSheet(computer: computer.id)
            }
            if target == "report" {
                try? await Task.sleep(for: .seconds(1))
                navigation.reportSheet = ReportSheet(computer: computer.id, about: nil)
            }
            return
        }
        let parts = target.split(separator: "/", maxSplits: 2).map(String.init)
        if let workspace = defaults.string(forKey: "workspace") {
            navigation.workspaces[computer.id] = workspace
        }
        for _ in 0..<50 where model.connection(computer.id)?.state != .online {
            try? await Task.sleep(for: .milliseconds(100))
        }
        navigation.sectionItem = parts.count > 2 ? parts[2] : nil
        navigation.open(computer: computer.id, plugin: parts[0], detail: parts.count > 1 ? parts[1] : nil)
        if let query = defaults.string(forKey: "search"), let connection = model.connection(computer.id), let workspace = navigation.workspace(for: connection) {
            model.agents(computer: computer.id, workspace: workspace.id)?.searchQuery = query
        }
    }
}
#endif

#if DEBUG
struct FixtureProbe: NetworkProbe {
    let failing: Bool

    func tailnetAddresses() -> [String] {
        failing ? [] : ["100.101.1.9"]
    }

    func resolve(_ name: String) async -> ResolveResult {
        failing ? .failed("nodename nor servname provided, or not known") : .addresses(["100.101.1.2"])
    }

    func connect(host: String, port: Int, timeout: Duration) async -> ConnectResult {
        try? await Task.sleep(for: .milliseconds(400))
        return failing ? .timedOut : .open
    }
}
#endif
