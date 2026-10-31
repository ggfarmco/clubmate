import Foundation

import ClubmateProtocol

public struct SDUIAttachment: Sendable, Equatable, Identifiable {
    public let key: String?
    public let kind: String
    public let path: String
    public let startLine: Int?
    public let endLine: Int?

    public var id: String { key ?? "\(kind):\(path):\(startLine ?? 0)-\(endLine ?? 0)" }

    public var label: String {
        if kind == "image" { return "Image" }
        guard kind == "selection", let startLine else { return "@ \(path)" }
        guard let endLine, endLine != startLine else { return "\(path) · line \(startLine)" }
        return "\(path) · lines \(startLine)–\(endLine)"
    }

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let kind = fields["kind"]?.stringValue, let path = fields["path"]?.stringValue else { return nil }
        key = fields["id"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
        self.kind = kind
        self.path = path
        startLine = fields["startLine"]?.numberValue.map(Int.init)
        endLine = fields["endLine"]?.numberValue.map(Int.init)
    }
}

public struct SDUIPlanEntry: Sendable, Equatable {
    public let content: String
    public let status: PlanStatus

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let content = fields["content"]?.stringValue else { return nil }
        self.content = content
        status = PlanStatus(fields["status"]?.stringValue)
    }
}

public struct SDUIToolBody: Sendable, Hashable {
    public enum Format: String, Sendable {
        case diff
        case text
        case terminal
    }

    public let format: Format
    public let text: String

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let text = fields["text"]?.stringValue else { return nil }
        format = fields["format"]?.stringValue.flatMap(Format.init) ?? .text
        self.text = text
    }

    @MainActor private static var memo: [SDUIToolBody: [DiffLine]] = [:]
    private static let memoLimit = 64

    @MainActor public var lines: [DiffLine] {
        if let cached = Self.memo[self] { return cached }
        let lines = format == .diff ? DiffLine.parse(text) : DiffLine.parse(text).map { DiffLine(text: $0.text, kind: .context) }
        if Self.memo.count >= Self.memoLimit { Self.memo.removeAll() }
        Self.memo[self] = lines
        return lines
    }
}

public struct SDUICardAction: Sendable, Equatable, Identifiable {
    public let action: String
    public let label: String
    public let icon: String?
    public let value: JSONValue

    public var id: String { action }

    init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let action = fields["action"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.action = action
        self.label = label
        icon = fields["icon"]?.stringValue
        value = fields["value"] ?? .null
    }
}

public struct SDUIPermissionOption: Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let kind: PermissionKind

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        kind = PermissionKind(fields["kind"]?.stringValue)
    }
}

public struct SDUIPicker: Sendable, Equatable, Identifiable {
    public let id: String
    public let title: String?
    public let help: String?
    public let value: String
    public let options: [SDUIOption]

    public var current: String {
        options.first { $0.value == value }?.label ?? value
    }

    public var label: String {
        guard let title else { return current }
        return "\(title): \(current)"
    }

    public static func summary(_ pickers: [SDUIPicker]) -> String {
        let model = pickers.first { $0.title == nil }?.current.replacingOccurrences(of: " (recommended)", with: "")
        let effort = pickers.first { $0.id == "effort" }?.current
        let finish = pickers.first { $0.id == "finish" && $0.value == AgentFinishMode.merge.rawValue }?.current
        return [model, effort, finish].compactMap { $0 }.joined(separator: " · ")
    }

    init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let id = fields["id"]?.stringValue, let value = fields["value"]?.stringValue else { return nil }
        self.id = id
        title = fields["title"]?.stringValue
        help = fields["help"]?.stringValue
        self.value = value
        options = fields["options"]?.arrayValue?.compactMap(SDUIOption.init) ?? []
    }
}

extension SDUINode {
    public var toolKind: ToolKind {
        ToolKind(string("kind"))
    }

    public var toolStatus: ToolStatus {
        ToolStatus(string("status"))
    }

    public var messageRole: MessageRole {
        MessageRole(string("role"))
    }

    public func attachments() -> [SDUIAttachment] {
        props["attachments"]?.arrayValue?.compactMap(SDUIAttachment.init) ?? []
    }

    public func planEntries() -> [SDUIPlanEntry] {
        props["entries"]?.arrayValue?.compactMap(SDUIPlanEntry.init) ?? []
    }

    public func toolBody() -> SDUIToolBody? {
        props["body"].flatMap(SDUIToolBody.init)
    }

    public func cardActions() -> [SDUICardAction] {
        props["actions"]?.arrayValue?.compactMap(SDUICardAction.init) ?? []
    }

    public func permissionOptions() -> [SDUIPermissionOption] {
        props["options"]?.arrayValue?.compactMap(SDUIPermissionOption.init) ?? []
    }

    public func composerCommands() -> [ComposerCommand] {
        props["commands"]?.arrayValue?.compactMap(ComposerCommand.init) ?? []
    }

    public func modes() -> [SDUIOption] {
        props["modes"]?.arrayValue?.compactMap(SDUIOption.init) ?? []
    }

    public func pickers() -> [SDUIPicker] {
        props["pickers"]?.arrayValue?.compactMap(SDUIPicker.init) ?? []
    }

    public func attachKinds() -> [String] {
        props["attach"]?.arrayValue?.compactMap(\.stringValue) ?? []
    }
}
