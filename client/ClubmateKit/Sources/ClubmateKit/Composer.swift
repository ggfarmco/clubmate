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

    public func takePastedImages(after old: String, isFile: (String) -> Bool = { FileManager.default.fileExists(atPath: $0) }) -> [String] {
        let now = Array(text), before = Array(old)
        var head = 0
        while head < min(now.count, before.count), now[head] == before[head] { head += 1 }
        var tail = 0
        while tail < min(now.count, before.count) - head, now[now.count - 1 - tail] == before[before.count - 1 - tail] { tail += 1 }
        guard now.count - tail - head > 1, let paths = Self.imagePaths(in: String(now[head..<(now.count - tail)]), isFile: isFile) else { return [] }
        text = String(now[..<head]) + String(now[(now.count - tail)...])
        caret = head
        return paths
    }

    private static let imageExtensions: Set<String> = ["png", "jpg", "jpeg", "gif", "webp"]

    private static func imagePaths(in inserted: String, isFile: (String) -> Bool) -> [String]? {
        let whole = inserted.trimmingCharacters(in: .whitespacesAndNewlines)
        if let path = imagePath(whole, isFile: isFile) { return [path] }
        let lines = whole.split(whereSeparator: \.isNewline).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        guard lines.count > 1 else { return nil }
        let paths = lines.compactMap { imagePath($0, isFile: isFile) }
        return paths.count == lines.count ? paths : nil
    }

    private static func imagePath(_ candidate: String, isFile: (String) -> Bool) -> String? {
        var path = candidate
        if path.count > 1, let quote = path.first, quote == "'" || quote == "\"", path.last == quote {
            path = String(path.dropFirst().dropLast())
        }
        if path.hasPrefix("file://") {
            guard let url = URL(string: path), url.isFileURL else { return nil }
            path = url.path
        } else if path.hasPrefix("~/") {
            path = NSString(string: path).expandingTildeInPath
        }
        guard path.hasPrefix("/"), imageExtensions.contains((path as NSString).pathExtension.lowercased()) else { return nil }
        if isFile(path) { return path }
        let unescaped = path.replacing(/\\(.)/) { String($0.1) }
        return unescaped != path && isFile(unescaped) ? unescaped : nil
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

    public let echoes = ComposerEchoes()

    public func send(_ id: String, text: String) -> String {
        if let model = models[id], model.text.trimmingCharacters(in: .whitespacesAndNewlines) == text { model.text = "" }
        return echoes.sent(text, composer: id)
    }

    public func failed(_ echo: String, text: String, composer id: String) {
        echoes.failed(echo, composer: id)
        let model = model(for: echoes.composer(of: Self.session(of: id)) ?? id)
        model.text = model.text.isEmpty ? text : text + "\n\n" + model.text
    }

    public func shown(_ id: String) {
        let old = echoes.composer(of: Self.session(of: id))
        echoes.shown(composer: id)
        guard let old, old != id, let left = models[old], !left.text.isEmpty else { return }
        let model = model(for: id)
        if model.text.isEmpty { model.text = left.text }
        left.text = ""
    }
}

@MainActor
@Observable
public final class ComposerEchoes {
    public struct Echo: Identifiable, Equatable, Sendable {
        public let id: String
        public let text: String
        fileprivate let base: Int
        fileprivate var gen: Int
    }

    private var sessions: [String: [Echo]] = [:]
    private var composers: [String: String] = [:]
    private var counter = 0

    public init() {}

    public func echoes(session: String) -> [Echo] {
        sessions[session] ?? []
    }

    func composer(of session: String) -> String? {
        composers[session]
    }

    func sent(_ text: String, composer: String) -> String {
        let session = ComposerDrafts.session(of: composer)
        let list = sessions[session] ?? []
        let base = Self.gen(of: composer)
        counter += 1
        let echo = Echo(id: "\(session)/echo/\(counter)", text: text, base: base, gen: max(base, list.last?.gen ?? base) + 1)
        sessions[session] = list + [echo]
        return echo.id
    }

    func failed(_ id: String, composer: String) {
        let session = ComposerDrafts.session(of: composer)
        guard var list = sessions[session], let i = list.firstIndex(where: { $0.id == id }) else { return }
        list.remove(at: i)
        for j in list.indices where j >= i { list[j].gen -= 1 }
        sessions[session] = list.isEmpty ? nil : list
    }

    func shown(composer: String) {
        let session = ComposerDrafts.session(of: composer)
        composers[session] = composer
        guard let list = sessions[session] else { return }
        let gen = Self.gen(of: composer)
        let kept = list.filter { gen < $0.gen && gen >= $0.base }
        sessions[session] = kept.isEmpty ? nil : kept
    }

    private static func gen(of composer: String) -> Int {
        composer.split(separator: "/").last.flatMap { Int($0) } ?? 0
    }
}
