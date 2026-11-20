#if DEBUG
import Foundation
import SwiftUI

import ClubmateKit

enum DevicesPreview {
    static let state: String? = {
        let arguments = CommandLine.arguments
        guard let index = arguments.firstIndex(of: "--devices-preview") else { return nil }
        return arguments.indices.contains(index + 1) ? arguments[index + 1] : "devices"
    }()

    @MainActor
    static func model(_ state: String) -> RemoteDevicesModel {
        let now = Date()
        let identity = Identity()
        return RemoteDevicesModel(
            run: { invocation in
                if state == "unavailable" { return .failure(.rpc(code: -32000, message: #"unknown command: "\#(invocation.id)""#)) }
                if let colour = invocation.args["colour"]?.stringValue { identity.colour = colour }
                if let enabled = invocation.args["enabled"]?.boolValue, invocation.id == "remote.set-title-bar-colour" { identity.titleBar = enabled }
                if let on = invocation.args["on"]?.boolValue, invocation.id == "remote.set-tunnel" { identity.tunnelOn = on }
                guard let text = replies(state, now: now, identity: identity)[invocation.id] else { return .success(.null) }
                return .success((try? JSONDecoder().decode(JSONValue.self, from: Data(text.utf8))) ?? .null)
            },
            confirm: { ask in await Confirmation.ask(ask.message, action: ask.action, destructive: ask.destructive) }
        )
    }

    @MainActor
    private final class Identity {
        var colour = "blue"
        var titleBar = true
        var tunnelOn: Bool?
    }

    @MainActor
    private static func relay(_ state: String, identity: Identity) -> String {
        guard state.hasPrefix("relay-") else { return "" }
        let state = state == "relay-pairing" ? "relay-connected" : state
        let on = identity.tunnelOn ?? (state != "relay-off")
        let shown = on ? (state == "relay-off" ? "relay-connected" : state) : "relay-off"
        let url = "https://clubmate-push.exe.xyz"
        switch shown {
        case "relay-signing-up":
            return #","relay":{"mode":"auto","url":"\#(url)","state":"signing-up","detail":"Signing up to the Clubmate relay…"},"tunnel":{"on":true,"url":"\#(url)","connected":false,"error":"","channels":0,"state":"connecting","detail":"Connecting…"},"pairReady":true,"pairHint":"""#
        case "relay-connected":
            return #","relay":{"mode":"manual","url":"https://relay.example.com","state":"ready","detail":"Ready"},"tunnel":{"on":true,"url":"https://relay.example.com","connected":true,"error":"","channels":1,"state":"connected","detail":"Connected through relay.example.com"},"pairReady":true,"pairHint":"""#
        case "relay-error":
            return #","relay":{"mode":"auto","url":"\#(url)","state":"error","detail":"This Mac is blocked by the relay"},"tunnel":{"on":true,"url":"\#(url)","connected":false,"error":"relay sign-up refused: 403","channels":0,"state":"error","detail":"Relay unreachable, trying again"},"pairReady":false,"pairHint":"Start Tailscale on this Mac, or wait until the relay path connects.""#
        default:
            return #","relay":{"mode":"auto","url":"\#(url)","state":"ready","detail":"Ready"},"tunnel":{"on":false,"url":"\#(url)","connected":false,"error":"","channels":0,"state":"off","detail":"Off"},"pairReady":true,"pairHint":"""#
        }
    }

    @MainActor
    private static func replies(_ state: String, now: Date, identity: Identity) -> [String: String] {
        let others = #"[{"name":"MacBook Pro","colour":"magenta","devices":["Vadim’s iPhone"]},{"name":"MacBook Air","colour":"graphite","devices":["Vadim’s iPhone"]}]"#
        let fields = #""computer":{"id":"cmp_3f9a1c","name":"Mac Studio","colour":"\#(identity.colour)"},"titleBarColour":\#(identity.titleBar),"otherMacs":\#(others),"titleDot":\#(identity.titleBar)"# + relay(state, identity: identity)
        let listening = state == "relay-error"
            ? #"{"enabled":true,"port":47621,"listening":[],"hosts":[],"error":"no Tailscale address on this Mac",\#(fields)}"#
            : #"{"enabled":true,"port":47621,"listening":["100.101.1.2:47621","[fd7a:115c:a1e0::1]:47621"],"hosts":["mac-studio.tail1234.ts.net","100.101.1.2","fd7a:115c:a1e0::1"],"error":"",\#(fields)}"#
        let noTailscale = #"{"enabled":true,"port":47621,"listening":[],"hosts":[],"error":"no Tailscale address on this Mac"}"#
        let expires = now.addingTimeInterval(state == "expired" ? -1 : 287).formatted(.iso8601)
        let link = "clubmate://pair?v=1&c=K7Q2M4XJ9P3RT6WZB5N8C2D4F6H8J2L4&h=mac-studio.tail1234.ts.net&h=100.101.1.2&h=fd7a:115c:a1e0::1&p=47621&id=cmp_3f9a1c&n=Vadim%E2%80%99s%20Mac%20Studio&e=\(Int(now.timeIntervalSince1970) + 287)"
        let viaRelay = state.hasPrefix("relay-") && (identity.tunnelOn ?? (state != "relay-off"))
        let pair = #"{"link":"\#(link)","code":"K7Q2M4XJ9P3RT6WZB5N8C2D4F6H8J2L4","expiresAt":"\#(expires)","relay":\#(viaRelay)}"#
        return [
            "remote.status": state == "empty" ? noTailscale : listening,
            "remote.devices": devices(state, now: now),
            "remote.enable": listening,
            "remote.pair": pair,
            "remote.set-colour": listening,
            "remote.set-title-bar-colour": listening,
            "remote.set-tunnel": listening,
            "remote.tunnel-rotate": listening,
        ]
    }

    private static func devices(_ state: String, now: Date) -> String {
        func device(_ id: String, _ name: String, _ model: String, paired: Double, seen: Double?, connected: Bool) -> String {
            let pairedAt = now.addingTimeInterval(-paired).formatted(.iso8601)
            let lastSeen = seen.map { #","lastSeen":"\#(now.addingTimeInterval(-$0).formatted(.iso8601))""# } ?? ""
            return #"{"id":"\#(id)","name":"\#(name)","model":"\#(model)","pairedAt":"\#(pairedAt)"\#(lastSeen),"connected":\#(connected)}"#
        }
        switch state {
        case "empty":
            return "[]"
        case "many":
            return "[" + (0..<84).map { device("d\($0)", "Test iPhone \($0)", "iPhone", paired: Double($0) * 3600, seen: Double($0) * 600, connected: $0 % 7 == 0) }.joined(separator: ",") + "]"
        default:
            return "[" + [
                device("d1", "Vadim’s iPhone", "iPhone", paired: 86_400 * 2, seen: nil, connected: true),
                device("d2", "iPad Pro of the Extremely Long Device Name That Never Ends Anywhere Soon", "iPad", paired: 86_400 * 9, seen: 3 * 3600, connected: false),
                device("d3", "Old Phone", "iPhone", paired: 86_400 * 40, seen: nil, connected: false),
            ].joined(separator: ",") + "]"
        }
    }
}

struct DevicesPreviewView: View {
    let state: String

    @Environment(AppModel.self) private var app
    @Environment(\.openWindow) private var openWindow
    @Environment(\.openSettings) private var openSettings
    @State private var model: RemoteDevicesModel

    init(state: String) {
        self.state = state
        _model = State(initialValue: DevicesPreview.model(state))
    }

    var body: some View {
        Group {
            if state == "settings" {
                Text("Settings opened").task {
                    app.launch(openWindow)
                    openSettings()
                }
            } else {
                DevicesSettings(model: model, client: nil, active: true)
            }
        }
        .frame(width: 460, height: 560)
        .task {
            guard state == "pairing" || state == "expired" || state == "relay-pairing" else { return }
            try? await Task.sleep(for: .milliseconds(300))
            await model.pair()
        }
    }
}
#endif
