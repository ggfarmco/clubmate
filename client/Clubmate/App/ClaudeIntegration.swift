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
    @ObservationIgnored private var path: String?
    @ObservationIgnored private var located = false

    func refresh() async {
        if !located {
            path = await connector.locate()
            located = true
        }
        guard let path else {
            phase = .missing
            return
        }
        phase = await connector.isConnected(claude: path) ? .connected : .disconnected
    }

    func connect() async {
        guard !busy, let path, let cli = Bundle.main.resourceURL?.appending(path: "bin/clubmate").path(percentEncoded: false) else { return }
        busy = true
        defer { busy = false }
        let result = await connector.connect(claude: path, cli: cli)
        output = result.output.isEmpty ? "exit \(result.status)" : result.output
        phase = await connector.isConnected(claude: path) ? .connected : .disconnected
    }
}
