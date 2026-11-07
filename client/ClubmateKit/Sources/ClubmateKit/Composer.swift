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
    case enter
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
        case 36, 76: self = .enter
        case 48: self = .accept
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
    public var caret: Int?
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
            return prompt.isEmpty ? .handled : .send(prompt)
        case .enter:
            return suggestions.isEmpty ? handle(.send) : handle(.accept)
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

    public func insert(_ dictated: String) {
        (text, caret) = Dictation.insert(dictated, into: text, at: caret)
    }
}

@MainActor
public final class ComposerDrafts {
    private var models: [String: ComposerModel] = [:]
    private var forms: [String: FormState] = [:]
    private var connectCards: [String: ConnectCardModel] = [:]
    public private(set) var focused: String?

    public init() {}

    public static func session(of id: String) -> String {
        String(id.prefix { $0 != "/" })
    }

    public func focus(_ id: String, _ on: Bool) {
        if on {
            focused = id
        } else if focused == id {
            focused = nil
        }
    }

    public private(set) var confirming: Set<String> = []

    public func confirm(session: String, _ open: Bool) {
        if open { confirming.insert(session) } else { confirming.remove(session) }
    }

    public func focusedEmpty(session: String) -> Bool {
        guard let focused, Self.session(of: focused) == session else { return false }
        return models[focused]?.text.isEmpty ?? false
    }

    public func model(for id: String) -> ComposerModel {
        if let model = models[id] { return model }
        let model = ComposerModel()
        models[id] = model
        return model
    }

    public func connectCard(for id: String) -> ConnectCardModel {
        if let model = connectCards[id] { return model }
        let model = ConnectCardModel()
        connectCards[id] = model
        return model
    }

    public func form(for id: String) -> FormState {
        if let form = forms[id] { return form }
        let form = FormState()
        forms[id] = form
        return form
    }

    public func left(form id: String) {
        if forms[id]?.unsent == false, forms[id]?.mounted == false { forms[id] = nil }
    }

    public func dictate(_ id: String, _ text: String) {
        model(for: id).insert(text)
    }

    public func clear(_ id: String, sent: String) {
        guard let model = models[id], model.text.trimmingCharacters(in: .whitespacesAndNewlines) == sent else { return }
        model.text = ""
    }
}
