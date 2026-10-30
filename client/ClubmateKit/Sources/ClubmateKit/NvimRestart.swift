import Foundation

public enum NvimRestart {
    public static let command = "editor-nvim.restart"
    static let unsavedMarker = "unsaved buffers: "

    public enum Outcome: Equatable, Sendable {
        case restarted
        case declined
        case failed(String)
    }

    @MainActor
    public static func run(
        workspace name: String,
        run: @MainActor (CommandInvocation) async -> Result<JSONValue, ClubmateError>,
        confirm: @MainActor (String) async -> Bool
    ) async -> Outcome {
        guard case .failure(let failure) = await run(CommandInvocation(id: command, args: .object([:]))) else { return .restarted }
        let message = failure.commandText
        guard let marker = message.range(of: unsavedMarker) else { return .failed(message) }
        guard await confirm("Neovim in “\(name)” has \(message[marker.lowerBound...]). Restart and discard the changes?") else { return .declined }
        if case .failure(let forced) = await run(CommandInvocation(id: command, args: .object(["force": .bool(true)]))) {
            return .failed(forced.commandText)
        }
        return .restarted
    }
}
