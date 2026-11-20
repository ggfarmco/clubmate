import CoreGraphics
import CoreImage
import CoreImage.CIFilterBuiltins
import Foundation
import Observation

public struct RemoteStatus: Decodable, Sendable, Equatable {
    public enum State: Sendable, Equatable {
        case off
        case listening
        case starting
        case failed
    }

    public let enabled: Bool
    public let port: Int
    public let listening: [String]
    public let hosts: [String]
    public let error: String
    public let computer: RemoteComputer?
    public let titleBarColour: Bool
    public let otherMacs: [RemoteOtherMac]
    public let titleDot: Bool
    public let relay: RemoteRelay?
    public let tunnel: RemoteTunnel?
    public let pairReady: Bool?
    public let pairHint: String

    public init(enabled: Bool, port: Int, listening: [String], hosts: [String], error: String, computer: RemoteComputer? = nil, titleBarColour: Bool = true, otherMacs: [RemoteOtherMac] = [], titleDot: Bool = false, relay: RemoteRelay? = nil, tunnel: RemoteTunnel? = nil, pairReady: Bool? = nil, pairHint: String = "") {
        self.enabled = enabled
        self.port = port
        self.listening = listening
        self.hosts = hosts
        self.error = error
        self.computer = computer
        self.titleBarColour = titleBarColour
        self.otherMacs = otherMacs
        self.titleDot = titleDot
        self.relay = relay
        self.tunnel = tunnel
        self.pairReady = pairReady
        self.pairHint = pairHint
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        enabled = try container.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
        port = try container.decodeIfPresent(Int.self, forKey: .port) ?? 0
        listening = try container.decodeIfPresent([String].self, forKey: .listening) ?? []
        hosts = try container.decodeIfPresent([String].self, forKey: .hosts) ?? []
        error = try container.decodeIfPresent(String.self, forKey: .error) ?? ""
        computer = try container.decodeIfPresent(RemoteComputer.self, forKey: .computer)
        titleBarColour = try container.decodeIfPresent(Bool.self, forKey: .titleBarColour) ?? true
        otherMacs = try container.decodeIfPresent([RemoteOtherMac].self, forKey: .otherMacs) ?? []
        titleDot = try container.decodeIfPresent(Bool.self, forKey: .titleDot) ?? false
        relay = try container.decodeIfPresent(RemoteRelay.self, forKey: .relay)
        tunnel = try container.decodeIfPresent(RemoteTunnel.self, forKey: .tunnel)
        pairReady = try container.decodeIfPresent(Bool.self, forKey: .pairReady)
        pairHint = try container.decodeIfPresent(String.self, forKey: .pairHint) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case enabled, port, listening, hosts, error, computer, titleBarColour, otherMacs, titleDot, relay, tunnel, pairReady, pairHint
    }

    public var state: State {
        if !enabled { return .off }
        if !error.isEmpty { return .failed }
        return listening.isEmpty ? .starting : .listening
    }

    public var summary: String {
        switch state {
        case .off: "Off"
        case .failed: error
        case .starting: "Waiting for a Tailscale address"
        case .listening: "Listening on \(address)"
        }
    }

    private var address: String {
        guard let host = hosts.first else { return listening.first ?? "" }
        return host.contains(":") ? "[\(host)]:\(port)" : "\(host):\(port)"
    }
}

public struct RemoteRelay: Decodable, Sendable, Equatable {
    public let mode: String
    public let url: String
    public let state: String
    public let detail: String

    public init(mode: String, url: String, state: String, detail: String) {
        self.mode = mode
        self.url = url
        self.state = state
        self.detail = detail
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        mode = try container.decodeIfPresent(String.self, forKey: .mode) ?? ""
        url = try container.decodeIfPresent(String.self, forKey: .url) ?? ""
        state = try container.decodeIfPresent(String.self, forKey: .state) ?? ""
        detail = try container.decodeIfPresent(String.self, forKey: .detail) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case mode, url, state, detail
    }

    public var host: String {
        URL(string: url)?.host ?? url
    }
}

public struct RemoteTunnel: Decodable, Sendable, Equatable {
    public let on: Bool
    public let url: String
    public let connected: Bool
    public let error: String
    public let channels: Int
    public let state: String
    public let detail: String

    public init(on: Bool, url: String = "", connected: Bool = false, error: String = "", channels: Int = 0, state: String = "", detail: String = "") {
        self.on = on
        self.url = url
        self.connected = connected
        self.error = error
        self.channels = channels
        self.state = state
        self.detail = detail
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        on = try container.decodeIfPresent(Bool.self, forKey: .on) ?? false
        url = try container.decodeIfPresent(String.self, forKey: .url) ?? ""
        connected = try container.decodeIfPresent(Bool.self, forKey: .connected) ?? false
        error = try container.decodeIfPresent(String.self, forKey: .error) ?? ""
        channels = try container.decodeIfPresent(Int.self, forKey: .channels) ?? 0
        state = try container.decodeIfPresent(String.self, forKey: .state) ?? ""
        detail = try container.decodeIfPresent(String.self, forKey: .detail) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case on, url, connected, error, channels, state, detail
    }
}

public struct RemoteComputer: Decodable, Sendable, Equatable {
    public let id: String
    public let name: String
    public let colour: String

    public init(id: String, name: String, colour: String) {
        self.id = id
        self.name = name
        self.colour = colour
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decodeIfPresent(String.self, forKey: .id) ?? ""
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        colour = try container.decodeIfPresent(String.self, forKey: .colour) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, colour
    }
}

public struct RemoteOtherMac: Decodable, Sendable, Equatable {
    public let name: String
    public let colour: String
    public let devices: [String]

    public init(name: String, colour: String, devices: [String]) {
        self.name = name
        self.colour = colour
        self.devices = devices
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        colour = try container.decodeIfPresent(String.self, forKey: .colour) ?? ""
        devices = try container.decodeIfPresent([String].self, forKey: .devices) ?? []
    }

    private enum CodingKeys: String, CodingKey {
        case name, colour, devices
    }
}

public struct RemoteDevice: Decodable, Sendable, Equatable, Identifiable {
    public let id: String
    public let name: String
    public let model: String
    public let pairedAt: Date
    public let lastSeen: Date?
    public let connected: Bool

    public init(id: String, name: String, model: String, pairedAt: Date, lastSeen: Date?, connected: Bool) {
        self.id = id
        self.name = name
        self.model = model
        self.pairedAt = pairedAt
        self.lastSeen = lastSeen
        self.connected = connected
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        model = try container.decodeIfPresent(String.self, forKey: .model) ?? ""
        pairedAt = try container.decode(Date.self, forKey: .pairedAt)
        lastSeen = try container.decodeIfPresent(Date.self, forKey: .lastSeen).flatMap { $0.timeIntervalSinceReferenceDate > 0 ? $0 : nil }
        connected = try container.decodeIfPresent(Bool.self, forKey: .connected) ?? false
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, model, pairedAt, lastSeen, connected
    }

    public var title: String {
        name.isEmpty ? (model.isEmpty ? id : model) : name
    }

    public func presence(now: Date, locale: Locale = .autoupdatingCurrent) -> String {
        if connected { return "Connected" }
        guard let lastSeen else { return "Never connected" }
        if now.timeIntervalSince(lastSeen) < 60 { return "Last seen just now" }
        let formatter = RelativeDateTimeFormatter()
        formatter.locale = locale
        formatter.unitsStyle = .full
        return "Last seen \(formatter.localizedString(for: lastSeen, relativeTo: now))"
    }

    public static func sorted(_ devices: [RemoteDevice]) -> [RemoteDevice] {
        devices.sorted { ($0.pairedAt, $1.title, $0.id) > ($1.pairedAt, $0.title, $1.id) }
    }

    public static func newlyPaired(from old: [RemoteDevice], to new: [RemoteDevice]) -> RemoteDevice? {
        new.first { device in !old.contains { $0.id == device.id && $0.pairedAt == device.pairedAt } }
    }
}

public struct RemotePairing: Decodable, Sendable, Equatable {
    public let link: String
    public let code: String
    public let expiresAt: Date
    public let relay: Bool

    public init(link: String, code: String, expiresAt: Date, relay: Bool = false) {
        self.link = link
        self.code = code
        self.expiresAt = expiresAt
        self.relay = relay
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        link = try container.decode(String.self, forKey: .link)
        code = try container.decode(String.self, forKey: .code)
        expiresAt = try container.decode(Date.self, forKey: .expiresAt)
        relay = try container.decodeIfPresent(Bool.self, forKey: .relay) ?? false
    }

    private enum CodingKeys: String, CodingKey {
        case link, code, expiresAt, relay
    }

    public var computerName: String {
        URLComponents(string: link)?.queryItems?.first { $0.name == "n" }?.value ?? ""
    }

    public func countdown(now: Date) -> String? {
        let left = Int(expiresAt.timeIntervalSince(now).rounded(.up))
        guard left > 0 else { return nil }
        return "\(left / 60):\(String(format: "%02d", left % 60))"
    }
}

public enum QRCode {
    public static func image(_ text: String, minSide: Int) -> CGImage? {
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        filter.correctionLevel = "M"
        guard let output = filter.outputImage, output.extent.width > 0 else { return nil }
        let scale = max(1, (CGFloat(minSide) / output.extent.width).rounded(.up))
        let scaled = output.samplingNearest().transformed(by: CGAffineTransform(scaleX: scale, y: scale))
        return CIContext().createCGImage(scaled, from: scaled.extent)
    }
}

@MainActor
@Observable
public final class RemoteDevicesModel {
    public enum Phase: Sendable, Equatable {
        case loading
        case ready
        case unavailable(String)
    }

    public struct Ask: Sendable, Equatable {
        public let message: String
        public let action: String
        public let destructive: Bool
    }

    public typealias Run = @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>

    public static let topic = "remote.changed"
    public static let revoke = Ask(message: "Revoke this device's access to Clubmate?", action: "Revoke", destructive: true)
    public static let rotate = Ask(message: "Make a new relay key? Phones switch to it when they next connect.", action: "Make New Key", destructive: false)
    public static let tailscaleHint = "Companion devices connect over Tailscale. Start Tailscale on this Mac and sign in to the same tailnet as your iPhone or iPad."
    public static let missingPlugin = "The remote plugin is not installed or not running."
    static let qrSide = 720

    public private(set) var phase = Phase.loading
    public private(set) var status: RemoteStatus?
    public private(set) var devices: [RemoteDevice] = []
    public private(set) var pairing: RemotePairing?
    public private(set) var qr: CGImage?
    public private(set) var notice: String?
    public private(set) var error: String?
    public private(set) var busy = false

    public var pairReady: Bool {
        guard let status else { return true }
        return status.pairReady ?? !(status.state == .failed || status.state == .starting)
    }

    public var pairHint: String {
        guard !pairReady else { return "" }
        return status?.pairReady == nil ? Self.tailscaleHint : status?.pairHint ?? ""
    }

    @ObservationIgnored private let run: Run
    @ObservationIgnored private let confirm: @MainActor (Ask) async -> Bool
    @ObservationIgnored private var baseline: [RemoteDevice]?

    public init(run: @escaping Run, confirm: @escaping @MainActor (Ask) async -> Bool) {
        self.run = run
        self.confirm = confirm
    }

    public func load() async {
        let status: RemoteStatus
        let devices: [RemoteDevice]
        do {
            status = try await call("remote.status")
            devices = RemoteDevice.sorted(try await call("remote.devices") as [RemoteDevice]? ?? [])
        } catch {
            phase = .unavailable(Self.unavailable(error))
            return
        }
        if pairing != nil, let baseline, let device = RemoteDevice.newlyPaired(from: baseline, to: devices) {
            close()
            notice = "Paired \(device.title)"
        }
        if pairing != nil, !status.enabled { close() }
        if status != self.status { self.status = status }
        if devices != self.devices { self.devices = devices }
        if phase != .ready { phase = .ready }
    }

    public func follow(_ client: CoreClient) async {
        let events = try? await client.busEvents(pattern: Self.topic)
        await load()
        guard let events else { return }
        for await _ in events {
            await load()
        }
    }

    public func setEnabled(_ enabled: Bool) async {
        await act { self.status = try await self.call("remote.enable", ["enabled": .bool(enabled)]) }
    }

    public func setColour(_ colour: String) async {
        await act { self.status = try await self.call("remote.set-colour", ["colour": .string(colour)]) }
    }

    public func setTitleBarColour(_ enabled: Bool) async {
        await act { self.status = try await self.call("remote.set-title-bar-colour", ["enabled": .bool(enabled)]) }
    }

    public func setTunnel(_ on: Bool) async {
        await act { self.status = try await self.call("remote.set-tunnel", ["on": .bool(on)]) }
    }

    public func rotateRelayKey() async {
        guard await confirm(Self.rotate) else { return }
        await act { self.status = try await self.call("remote.tunnel-rotate") }
    }

    public func pair() async {
        if pairing == nil { baseline = devices }
        notice = nil
        await act {
            let pairing: RemotePairing = try await self.call("remote.pair")
            self.pairing = pairing
            self.qr = QRCode.image(pairing.link, minSide: Self.qrSide)
        }
        await load()
    }

    public func done() async {
        guard pairing != nil else { return }
        close()
        _ = await run(CommandInvocation(id: "remote.cancel-pair", args: .object([:])))
    }

    public func revoke(_ device: RemoteDevice) async {
        guard await confirm(Self.revoke) else { return }
        await act { let _: JSONValue = try await self.call("remote.revoke", ["device": .string(device.id)]) }
        await load()
    }

    private func close() {
        pairing = nil
        qr = nil
        baseline = nil
    }

    private func act(_ body: @MainActor () async throws -> Void) async {
        busy = true
        defer { busy = false }
        do {
            try await body()
            error = nil
        } catch {
            self.error = Self.unavailable(error)
        }
    }

    private func call<T: Decodable>(_ id: String, _ args: [String: JSONValue] = [:]) async throws -> T {
        switch await run(CommandInvocation(id: id, args: .object(args))) {
        case .success(let value):
            return try value.decode(T.self)
        case .failure(let failure):
            throw failure
        }
    }

    static func unavailable(_ error: any Error) -> String {
        guard let error = error as? ClubmateError else { return "unexpected reply: \(error)" }
        if case .rpc(_, let message) = error, message.contains("unknown command") { return missingPlugin }
        return error.commandText
    }
}
