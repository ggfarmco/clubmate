import Foundation
import Synchronization

public struct RelayRoute: Codable, Sendable, Equatable, Hashable {
    public let url: URL
    public let key: String
    public let kid: String

    public init(url: URL, key: String, kid: String) {
        self.url = url
        self.key = key
        self.kid = kid
    }

    public var macKey: Data? {
        guard let data = try? Tunnel.decodeKey(key), Tunnel.keyID(data) == kid else { return nil }
        return data
    }
}

public enum PathChoice: String, Codable, Sendable, CaseIterable {
    case auto
    case tailnet
    case relay
}

public struct Computer: Codable, Sendable, Equatable, Identifiable, Hashable {
    public let id: String
    public var name: String
    public var hosts: [String]
    public var port: Int
    public var pairedAt: Date
    public var lastSeen: Date?
    public var colour: String?
    public var model: String?
    public var relay: RelayRoute?
    public var path: PathChoice

    public init(id: String, name: String, hosts: [String], port: Int, pairedAt: Date = .now, lastSeen: Date? = nil, colour: String? = nil, model: String? = nil, relay: RelayRoute? = nil, path: PathChoice = .auto) {
        self.id = id
        self.name = name
        self.hosts = hosts
        self.port = port
        self.pairedAt = pairedAt
        self.lastSeen = lastSeen
        self.colour = colour
        self.model = model
        self.relay = relay
        self.path = path
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        hosts = try c.decode([String].self, forKey: .hosts)
        port = try c.decode(Int.self, forKey: .port)
        pairedAt = try c.decode(Date.self, forKey: .pairedAt)
        lastSeen = try c.decodeIfPresent(Date.self, forKey: .lastSeen)
        colour = try c.decodeIfPresent(String.self, forKey: .colour)
        model = try? c.decodeIfPresent(String.self, forKey: .model)
        relay = try? c.decodeIfPresent(RelayRoute.self, forKey: .relay)
        path = (try? c.decodeIfPresent(PathChoice.self, forKey: .path)) ?? .auto
    }

    public var symbol: String {
        model == "laptop" ? "laptopcomputer" : "desktopcomputer"
    }
}

public protocol TokenVault: Sendable {
    func token(for computerID: String) -> String?
    func setToken(_ token: String, for computerID: String) throws
    func removeToken(for computerID: String) throws
}

public final class MemoryTokenVault: TokenVault {
    private let tokens = Mutex<[String: String]>([:])

    public init() {}

    public func token(for computerID: String) -> String? {
        tokens.withLock { $0[computerID] }
    }

    public func setToken(_ token: String, for computerID: String) {
        tokens.withLock { $0[computerID] = token }
    }

    public func removeToken(for computerID: String) {
        tokens.withLock { $0[computerID] = nil }
    }
}

@MainActor
public final class ComputerStore {
    public static let seenEvery: TimeInterval = 60

    public private(set) var computers: [Computer]
    public let vault: any TokenVault
    private let file: URL?

    public init(file: URL?, vault: any TokenVault) {
        self.file = file
        self.vault = vault
        computers = file.flatMap { try? Data(contentsOf: $0) }.flatMap { try? JSONDecoder.clubmate.decode([Computer].self, from: $0) } ?? []
    }

    public static func relayKey(_ id: String) -> String {
        id + "#relay"
    }

    public func token(for id: String) -> String? {
        vault.token(for: id)
    }

    public func relayTicket(for id: String) -> String? {
        vault.token(for: Self.relayKey(id))
    }

    public func save(_ computer: Computer, token: String, relayTicket: String? = nil) throws {
        try vault.setToken(token, for: computer.id)
        if let relayTicket {
            try vault.setToken(relayTicket, for: Self.relayKey(computer.id))
        } else {
            try vault.removeToken(for: Self.relayKey(computer.id))
        }
        if let index = computers.firstIndex(where: { $0.id == computer.id }) {
            computers[index] = computer
        } else {
            computers.append(computer)
        }
        try persist()
    }

    public func seen(_ id: String, at date: Date) throws {
        guard let index = computers.firstIndex(where: { $0.id == id }) else { return }
        if let saved = computers[index].lastSeen, date.timeIntervalSince(saved) < Self.seenEvery { return }
        computers[index].lastSeen = date
        try persist()
    }

    public func identify(_ id: String, as info: ComputerInfo) throws {
        guard let index = computers.firstIndex(where: { $0.id == id }) else { return }
        let colour = info.colour ?? computers[index].colour
        let model = info.model ?? computers[index].model
        guard computers[index].name != info.name || computers[index].colour != colour || computers[index].model != model else { return }
        computers[index].name = info.name
        computers[index].colour = colour
        computers[index].model = model
        try persist()
    }

    public func setRelay(_ relay: RelayRoute, for id: String) throws {
        guard let index = computers.firstIndex(where: { $0.id == id }), computers[index].relay != relay else { return }
        computers[index].relay = relay
        try persist()
    }

    public func setPath(_ path: PathChoice, for id: String) throws {
        guard let index = computers.firstIndex(where: { $0.id == id }), computers[index].path != path else { return }
        computers[index].path = path
        try persist()
    }

    public func remove(_ id: String) throws {
        computers.removeAll { $0.id == id }
        try vault.removeToken(for: id)
        try vault.removeToken(for: Self.relayKey(id))
        try persist()
    }

    private func persist() throws {
        guard let file else { return }
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        try encoder.encode(computers).write(to: file, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }
}

public struct DeviceIdentity: Sendable, Equatable, Codable {
    public static let defaultsKey = "companion.deviceID"

    public let id: String
    public let name: String
    public let model: String

    public init(id: String, name: String, model: String) {
        self.id = id
        self.name = name
        self.model = model
    }

    public static func stable(defaults: UserDefaults, name: String, model: String) -> DeviceIdentity {
        let id = defaults.string(forKey: defaultsKey) ?? {
            let fresh = UUID().uuidString.lowercased()
            defaults.set(fresh, forKey: defaultsKey)
            return fresh
        }()
        return DeviceIdentity(id: id, name: name, model: model)
    }
}
