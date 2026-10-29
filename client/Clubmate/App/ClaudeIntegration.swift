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
        case connected
    }

    private(set) var phase: Phase = .checking
    private(set) var output = ""
    private(set) var busy = false

    @ObservationIgnored private let connector = ClaudeConnector()
    @ObservationIgnored private var location: ClaudeLocation?
    @ObservationIgnored private var locating: Task<ClaudeLocation?, Never>?

    func refresh() async {
        if locating == nil {
            let connector = connector
            locating = Task { await connector.locate() }
        }
        location = await locating?.value
        guard let location else {
            phase = .missing
            return
        }
        phase = await connector.isConnected(location) ? .connected : .disconnected
    }

    func connect() async {
        guard !busy, let location, let cli = Bundle.main.resourceURL?.appending(path: "bin/clubmate").path(percentEncoded: false) else { return }
        busy = true
        defer { busy = false }
        let result = await connector.connect(location, cli: cli)
        output = result.output.isEmpty ? "exit \(result.status)" : result.output
        phase = await connector.isConnected(location) ? .connected : .disconnected
    }
}
