import Foundation
import Observation

public struct ComposerCommand: Sendable, Equatable, Identifiable {
    public let name: String
    public let description: String
    public let hint: String

    public var id: String { name }

    public init(name: String, description: String, hint: String) {
        self.name = name
        self.description = description
        self.hint = hint
    }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let name = fields["name"]?.stringValue, !name.isEmpty else { return nil }
        self.init(name: name, description: fields["description"]?.stringValue ?? "", hint: fields["hint"]?.stringValue ?? "")
    }
}

public enum ComposerKey: Sendable, Equatable {
    case send
    case escape
    case up
    case down
    case accept
    case attach

    public init?(keyCode: UInt16, characters: String?, command: Bool, control: Bool, option: Bool, shift: Bool) {
        let returnKey = keyCode == 36 || keyCode == 76
        if returnKey, command, !control, !option, !shift {
            self = .send
            return
        }
        if characters == "@", !command, !control {
            self = .attach
            return
        }
        guard !command, !control, !option, !shift else { return nil }
        switch keyCode {
        case 36, 76, 48: self = .accept
        case 53: self = .escape
        case 126: self = .up
        case 125: self = .down
        default: return nil
        }
    }
}

public enum ComposerEffect: Sendable, Equatable {
    case send(String)
    case stop
    case attachFile
    case handled
}

@MainActor
@Observable
public final class ComposerModel {
    public var text = "" {
        didSet { if text != oldValue { highlighted = 0 } }
    }
    public var commands: [ComposerCommand] = []
    public var running = false
    public private(set) var highlighted = 0

    public init() {}

    public var suggestions: [ComposerCommand] {
        guard text.hasPrefix("/") else { return [] }
        let typed = text.dropFirst()
        guard !typed.contains(where: \.isWhitespace) else { return [] }
        return commands.filter { $0.name.lowercased().hasPrefix(typed.lowercased()) }
    }

    public func handle(_ key: ComposerKey, before: Character? = nil) -> ComposerEffect? {
        switch key {
        case .send:
            let prompt = text.trimmingCharacters(in: .whitespacesAndNewlines)
            return running || prompt.isEmpty ? .handled : .send(prompt)
        case .escape:
            return running ? .stop : nil
        case .up, .down:
            let count = suggestions.count
            guard count > 0 else { return nil }
            highlighted = (highlighted + (key == .down ? 1 : -1) + count) % count
            return .handled
        case .accept:
            let shown = suggestions
            guard shown.indices.contains(highlighted) else { return nil }
            choose(shown[highlighted])
            return .handled
        case .attach:
            guard before.map(\.isWhitespace) ?? true else { return nil }
            return .attachFile
        }
    }

    public func character(before caret: String.Index?) -> Character? {
        guard let caret else { return text.last }
        guard caret > text.startIndex, caret <= text.endIndex else { return nil }
        return text[text.index(before: caret)]
    }

    public func choose(_ command: ComposerCommand) {
        text = "/\(command.name) "
    }
}
