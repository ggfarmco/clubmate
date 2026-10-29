import Foundation
import Observation

import ClubmateKit

@MainActor
@Observable
final class ClaudeIntegration {
    enum Phase: Equatable {
        case checking
        case missing
        case disconnected
        case other(String)
        case connected
    }

    private(set) var phase: Phase = .checking
    private(set) var output = ""
    private(set) var busy = false

    var serverName: String { connector.serverName }

    @ObservationIgnored private let connector = ClaudeConnector()
    @ObservationIgnored private var location: ClaudeLocation?
    @ObservationIgnored private var locating: Task<ClaudeLocation?, Never>?
    @ObservationIgnored private let cli = Bundle.main.resourceURL?.appending(path: "bin/clubmate").path(percentEncoded: false)

    func refresh() async {
        if locating == nil {
            let connector = connector
            locating = Task { await connector.locate() }
        }
        location = await locating?.value
        guard let location, let cli else {
            phase = .missing
            return
        }
        phase = Self.phase(await connector.registration(location, cli: cli))
    }

    func connect() async {
        guard !busy, let location, let cli else { return }
        busy = true
        defer { busy = false }
        let result = await connector.connect(location, cli: cli)
        output = result.output.isEmpty ? "exit \(result.status)" : result.output
        phase = Self.phase(await connector.registration(location, cli: cli))
    }

    private static func phase(_ registration: ClaudeRegistration) -> Phase {
        switch registration {
        case .absent: .disconnected
        case .current: .connected
        case .other(let command): .other(command)
        }
    }
}
