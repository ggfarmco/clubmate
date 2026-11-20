import Foundation

public struct PairingLink: Sendable, Equatable {
    public enum Problem: Error, Equatable, Sendable {
        case notAPairingLink
        case unsupportedVersion(String)
        case missing(String)
        case badPort
        case expired
        case noTailnetHost
        case badRelay
        case badKey

        public var message: String {
            switch self {
            case .notAPairingLink: "This isn't a Clubmate pairing link."
            case .unsupportedVersion(let version): "This pairing link is version \(version); update the app to use it."
            case .missing(let field): "The pairing link has no \(field)."
            case .badPort: "The pairing link has an invalid port."
            case .expired: "This pairing code has expired. Ask Clubmate for a new one."
            case .noTailnetHost: "This link has no Tailscale address. Make sure Tailscale is running on the Mac, then show a new code."
            case .badRelay: "The pairing link has a relay address this app can't use."
            case .badKey: "The pairing link has an invalid relay key. Show a new code on the Mac."
            }
        }
    }

    public static let scheme = "clubmate"
    public static let defaultPort = 47621

    public let code: String
    public let hosts: [String]
    public let port: Int
    public let computerID: String
    public let computerName: String
    public let expiresAt: Date
    public let relay: RelayRoute?
    public let pairingTicket: String?

    public init(code: String, hosts: [String], port: Int, computerID: String, computerName: String, expiresAt: Date, relay: RelayRoute? = nil, pairingTicket: String? = nil) {
        self.code = code
        self.hosts = hosts
        self.port = port
        self.computerID = computerID
        self.computerName = computerName
        self.expiresAt = expiresAt
        self.relay = relay
        self.pairingTicket = pairingTicket
    }

    public static func isPairingURL(_ url: URL) -> Bool {
        url.scheme?.lowercased() == scheme && url.host()?.lowercased() == "pair"
    }

    public static func parse(_ text: String, policy: HostPolicy = .tailnet, now: Date = .now) throws(Problem) -> PairingLink {
        guard let url = URL(string: text.trimmingCharacters(in: .whitespacesAndNewlines)) else { throw .notAPairingLink }
        return try parse(url, policy: policy, now: now)
    }

    public static func parse(_ url: URL, policy: HostPolicy = .tailnet, now: Date = .now) throws(Problem) -> PairingLink {
        guard isPairingURL(url), let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.percentEncodedQueryItems else { throw .notAPairingLink }
        let values = { (name: String) in
            items.filter { $0.name == name }.compactMap { $0.value?.replacingOccurrences(of: "+", with: " ").removingPercentEncoding }.filter { !$0.isEmpty }
        }
        guard let version = values("v").first else { throw .missing("version") }
        guard version == "1" || version == "2" else { throw .unsupportedVersion(version) }
        guard let code = values("c").first else { throw .missing("code") }
        var relay: RelayRoute?
        var ticket: String?
        if version == "2", let address = values("r").first {
            guard let url = URL(string: address), policy.allowsRelay(url) else { throw .badRelay }
            guard let key = values("k").first, let kid = values("kid").first else { throw .badKey }
            let route = RelayRoute(url: url, key: key, kid: kid.lowercased())
            guard route.macKey != nil else { throw .badKey }
            guard let pt = values("pt").first else { throw .missing("pairing ticket") }
            relay = route
            ticket = pt
        }
        let hosts = values("h").map { $0.trimmingCharacters(in: CharacterSet(charactersIn: "[]")) }
        guard !hosts.isEmpty || relay != nil else { throw .missing("host") }
        let allowed = policy.filter(hosts)
        guard !allowed.isEmpty || relay != nil else { throw .noTailnetHost }
        guard let id = values("id").first else { throw .missing("computer id") }
        guard let expiry = values("e").first.flatMap(TimeInterval.init) else { throw .missing("expiry") }
        var port = defaultPort
        if let text = values("p").first {
            guard let value = Int(text), (1...65535).contains(value) else { throw .badPort }
            port = value
        }
        let expiresAt = Date(timeIntervalSince1970: expiry)
        guard expiresAt > now else { throw .expired }
        return PairingLink(code: code, hosts: allowed, port: port, computerID: id, computerName: values("n").first ?? "Mac", expiresAt: expiresAt, relay: relay, pairingTicket: ticket)
    }
}

public struct PairingReview: Sendable, Equatable, Identifiable {
    public let link: PairingLink
    public let existing: Computer?

    public init(link: PairingLink, existing: [Computer]) {
        self.link = link
        self.existing = existing.first { $0.id == link.computerID }
    }

    public var id: String {
        link.computerID + "/" + link.code
    }

    public var replacesExisting: Bool {
        existing != nil
    }

    public var addedHosts: [String] {
        guard let existing else { return [] }
        return link.hosts.filter { !existing.hosts.contains($0) }
    }

    public var removedHosts: [String] {
        guard let existing else { return [] }
        return existing.hosts.filter { !link.hosts.contains($0) }
    }

    public var previousPort: Int? {
        guard let existing, existing.port != link.port else { return nil }
        return existing.port
    }

    public var changesAddress: Bool {
        !addedHosts.isEmpty || !removedHosts.isEmpty || previousPort != nil
    }

    public var changesRelay: Bool {
        guard let existing else { return false }
        return existing.relay?.url != link.relay?.url
    }

    public var changesKey: Bool {
        guard let old = existing?.relay, let new = link.relay else { return false }
        return old.key != new.key
    }

    public func needsConfirmation(scanned: Bool) -> Bool {
        !scanned || replacesExisting || changesAddress || changesRelay || changesKey
    }
}
