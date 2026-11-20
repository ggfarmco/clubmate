import Foundation

public enum NetworkPath: String, Codable, Sendable, CaseIterable {
    case tailnet
    case relay
}

extension NetworkPath {
    public var label: String {
        switch self {
        case .tailnet: "Tailscale"
        case .relay: "Clubmate relay"
        }
    }
}

extension PathChoice {
    public var label: String {
        switch self {
        case .auto: "Auto"
        case .tailnet: "Tailscale"
        case .relay: "Relay"
        }
    }
}

public struct PathSelector: Sendable, Equatable {
    public static let probeEvery: Duration = .seconds(600)

    public var choice: PathChoice
    public var paths: Set<NetworkPath>
    public var answered: NetworkPath?

    public init(choice: PathChoice, paths: Set<NetworkPath>, answered: NetworkPath? = nil) {
        self.choice = choice
        self.paths = paths
        self.answered = answered
    }

    public var order: [NetworkPath] {
        switch choice {
        case .tailnet where paths.contains(.tailnet): return [.tailnet]
        case .relay where paths.contains(.relay): return [.relay]
        default:
            let known = NetworkPath.allCases.filter(paths.contains)
            guard let start = answered.flatMap({ known.contains($0) ? $0 : nil }) ?? known.first else { return [] }
            return [start] + known.filter { $0 != start }
        }
    }

    public func next(after path: NetworkPath, failed error: any Error) -> NetworkPath? {
        guard Self.fallsOver(error), let index = order.firstIndex(of: path), index + 1 < order.count else { return nil }
        return order[index + 1]
    }

    public func probeDue(active: NetworkPath?, elapsed: Duration) -> Bool {
        choice == .auto && active == .relay && paths.contains(.tailnet) && elapsed >= Self.probeEvery
    }

    public static func fallsOver(_ error: any Error) -> Bool {
        switch error as? RemoteError {
        case .notSent?, .unreachable?, .hostNotAllowed?: true
        default: false
        }
    }
}

extension RemoteClient.Route {
    public var path: NetworkPath {
        switch self {
        case .tailnet: .tailnet
        case .relay: .relay
        }
    }
}
