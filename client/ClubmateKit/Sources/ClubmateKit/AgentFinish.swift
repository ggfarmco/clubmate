import Foundation
import Observation

public enum AgentFinishMode: String, Sendable, CaseIterable {
    case ask
    case merge

    public var label: String {
        switch self {
        case .ask: "Ask"
        case .merge: "Merge to main"
        }
    }
}

@MainActor
@Observable
public final class AgentFinishModel {
    public static let command = "agents.finishDefault"

    public private(set) var mode: AgentFinishMode?
    public private(set) var error: String?

    @ObservationIgnored private let run: NvimConfigModel.Run

    public init(run: @escaping NvimConfigModel.Run) {
        self.run = run
    }

    public func load() async {
        await apply(.object([:]))
    }

    public func choose(_ mode: AgentFinishMode) async {
        guard mode != self.mode else { return }
        await apply(.object(["value": .string(mode.rawValue)]))
    }

    private func apply(_ args: JSONValue) async {
        switch await run(nil, CommandInvocation(id: Self.command, args: args)) {
        case .success(let value):
            mode = value.objectValue?["value"]?.stringValue.flatMap(AgentFinishMode.init(rawValue:))
            error = mode == nil ? "unexpected reply from \(Self.command)" : nil
        case .failure(let failure):
            error = NvimConfigModel.text(failure)
        }
    }
}
