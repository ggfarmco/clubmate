import Foundation
import Synchronization

public struct Computer: Codable, Sendable, Equatable, Identifiable, Hashable {
    public let id: String
    public var name: String
    public var hosts: [String]
    public var port: Int
    public var pairedAt: Date

    public init(id: String, name: String, hosts: [String], port: Int, pairedAt: Date = .now) {
        self.id = id
        self.name = name
        self.hosts = hosts
        self.port = port
        self.pairedAt = pairedAt
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
    public private(set) var computers: [Computer]
    public let vault: any TokenVault
    private let file: URL?

    public init(file: URL?, vault: any TokenVault) {
        self.file = file
        self.vault = vault
        computers = file.flatMap { try? Data(contentsOf: $0) }.flatMap { try? JSONDecoder.companion.decode([Computer].self, from: $0) } ?? []
    }

    public func token(for id: String) -> String? {
        vault.token(for: id)
    }

    public func save(_ computer: Computer, token: String) throws {
        try vault.setToken(token, for: computer.id)
        if let index = computers.firstIndex(where: { $0.id == computer.id }) {
            computers[index] = computer
        } else {
            computers.append(computer)
        }
        try persist()
    }

    public func remove(_ id: String) throws {
        computers.removeAll { $0.id == id }
        try vault.removeToken(for: id)
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
