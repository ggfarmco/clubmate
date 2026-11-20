#if DEBUG
import Foundation
import SwiftUI
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

    static func apply(model: CompanionModel, navigation: AppNavigation, pairing: PairingFlow, voice: VoiceSession, notifier: LocalNotifier) async {
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
            voice.begin(into: defaults.bool(forKey: "voiceDictation") ? navigation.path.last.flatMap { AppNavigation.tag($0.destination) } : nil)
        }
        if let target = defaults.string(forKey: "notify"), let computer = model.computers.first {
            let parts = target.split(separator: "/", maxSplits: 1).map(String.init)
            try? await Task.sleep(for: .seconds(defaults.double(forKey: "notifyDelay")))
            notifier.open(computer.id, parts[0], parts.count > 1 ? parts[1] : nil, active: true)
        }
        if let path = defaults.string(forKey: "doc"), let computer = model.computers.first {
            for _ in 0..<50 where model.connection(computer.id)?.state != .online {
                try? await Task.sleep(for: .milliseconds(100))
            }
            if let connection = model.connection(computer.id), let workspace = defaults.string(forKey: "workspace") ?? navigation.workspace(for: connection)?.id {
                withTransaction(\.disablesAnimations, true) { navigation.push(.doc(path: path, anchor: nil), in: WorkspaceKey(computer: computer.id, workspace: workspace)) }
            }
        }
    }

    private static func navigate(model: CompanionModel, navigation: AppNavigation) async {
        let defaults = UserDefaults.standard
        guard let target = defaults.string(forKey: "open"), let computer = model.computers.first else { return }
        if target == "sidebar" || target == "mac" || target == "connection" || target == "report" || target == "pair" {
            try? await Task.sleep(for: .seconds(1))
            if target == "pair" { navigation.showingAddComputer = true }
            if target == "mac" || target == "connection" { navigation.push(.macDetails(computer: computer.id)) }
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
        if target == "usage" {
            for _ in 0..<50 where model.connection(computer.id)?.usage == nil {
                try? await Task.sleep(for: .milliseconds(100))
            }
            navigation.switchTab(.agents)
            try? await Task.sleep(for: .seconds(1))
            navigation.usageSheet = UsageSheet(computer: computer.id)
            return
        }
        if let workspace = defaults.string(forKey: "workspace") {
            navigation.workspaces[computer.id] = workspace
        }
        for _ in 0..<200 where model.connection(computer.id)?.state != .online || model.connection(computer.id)?.workspaces.isEmpty != false {
            try? await Task.sleep(for: .milliseconds(100))
        }
        navigation.follow(model)
        guard let connection = model.connection(computer.id), navigation.workspace(for: connection) != nil else { return }
        if target.hasPrefix("docs/") {
            withTransaction(\.disablesAnimations, true) {
                navigation.switchTab(.agents)
                navigation.push(.doc(path: String(target.dropFirst(5)), anchor: nil))
            }
            return
        }
        guard let route = route(target, navigation: navigation) else { return }
        withTransaction(\.disablesAnimations, true) {
            navigation.switchTab(route.tab)
            if let destination = route.destination {
                navigation.showIn(route.tab, destination)
            } else if !target.hasPrefix("tab:") {
                navigation.popToRoot(route.tab)
            }
        }
    }

    private static func route(_ target: String, navigation: AppNavigation) -> (tab: CompanionTab, destination: Destination?)? {
        if target.hasPrefix("tab:") {
            return CompanionTab(rawValue: String(target.dropFirst(4))).map { ($0, nil) }
        }
        if target.hasPrefix("push:") {
            return destination(String(target.dropFirst(5)), computer: navigation.computer).map { ($0.tab ?? .agents, $0) }
        }
        let parts = target.split(separator: "/", maxSplits: 2).map(String.init)
        switch parts[0] {
        case InboxPluginInfo.id:
            return (.agents, .inbox)
        case DocsPluginInfo.id:
            return (.agents, nil)
        default:
            let tab = CompanionPlugins.plugin(parts[0])?.tab ?? .agents
            guard parts.count > 1 else { return (tab, nil) }
            navigation.sectionItem = parts.count > 2 ? parts[2] : nil
            if tab == .agents, GateEntry.matches(parts[1]) {
                navigation.sectionItem = parts[1]
                return (tab, .section(id: GateEntry.section, item: nil))
            }
            return (tab, AppNavigation.destination(parts[1], tab: tab))
        }
    }

    private static func destination(_ text: String, computer: String?) -> Destination? {
        let parts = text.split(separator: "/", maxSplits: 1).map(String.init)
        let value = parts.count > 1 ? parts[1] : nil
        switch (parts.first, value) {
        case ("session", let id?): return .session(id)
        case ("run", let id?): return .run(id)
        case ("task", let number?): return Int(number).map(Destination.task)
        case ("doc", let path?): return .doc(path: path, anchor: nil)
        case ("section", let id?): return .section(id: id, item: nil)
        case ("inbox", nil): return .inbox
        case ("mac", nil): return computer.map { .macDetails(computer: $0) }
        default: return nil
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
