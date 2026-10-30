import Foundation

public struct HostPolicy: Sendable, Equatable {
    public static let tailnet = HostPolicy(allowLoopback: false)
    public static let tailnetAndLoopback = HostPolicy(allowLoopback: true)

    public let allowLoopback: Bool

    public init(allowLoopback: Bool) {
        self.allowLoopback = allowLoopback
    }

    public func allows(_ host: String) -> Bool {
        normalized(host) != nil
    }

    public func normalized(_ host: String) -> String? {
        let host = host.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        if let v4 = Self.ipv4(host) {
            let allowed = (v4[0] == 100 && v4[1] & 0xC0 == 64) || (allowLoopback && v4[0] == 127)
            return allowed ? host : nil
        }
        if let v6 = Self.ipv6(host) {
            let tailnet: [UInt8] = [0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0]
            let allowed = Array(v6.prefix(6)) == tailnet || (allowLoopback && v6 == Array(repeating: 0, count: 15) + [1])
            return allowed ? host : nil
        }
        if allowLoopback, host == "localhost" { return host }
        let name = host.hasSuffix(".") ? String(host.dropLast()) : host
        let labels = name.split(separator: ".", omittingEmptySubsequences: false)
        guard labels.count >= 3, labels.suffix(2).elementsEqual(["ts", "net"]), labels.allSatisfy(Self.isLabel) else { return nil }
        return name
    }

    public func filter(_ hosts: [String]) -> [String] {
        var seen: Set<String> = []
        return hosts.compactMap(normalized).filter { seen.insert($0).inserted }
    }

    private static func isLabel(_ label: Substring) -> Bool {
        (1...63).contains(label.count) && label.first != "-" && label.last != "-"
            && label.unicodeScalars.allSatisfy { ("a"..."z").contains($0) || ("0"..."9").contains($0) || $0 == "-" }
    }

    private static func ipv4(_ host: String) -> [UInt8]? {
        var address = in_addr()
        guard host.split(separator: ".").count == 4, inet_pton(AF_INET, host, &address) == 1 else { return nil }
        return withUnsafeBytes(of: address) { Array($0) }
    }

    private static func ipv6(_ host: String) -> [UInt8]? {
        var address = in6_addr()
        guard host.contains(":"), inet_pton(AF_INET6, host, &address) == 1 else { return nil }
        return withUnsafeBytes(of: address) { Array($0) }
    }
}
