public struct DocCard: Sendable, Hashable, Identifiable {
    public enum Kind: String, Sendable {
        case markdown
        case mermaid
        case artifact

        public var label: String {
            switch self {
            case .markdown: "Document"
            case .mermaid: "Diagram"
            case .artifact: "Artifact"
            }
        }

        public var symbol: String {
            switch self {
            case .markdown: "doc.richtext"
            case .mermaid: "flowchart"
            case .artifact: "chart.bar.doc.horizontal"
            }
        }
    }

    public let path: String
    public let kind: Kind
    public let title: String
    public let rel: String
    public let presented: Bool

    public var id: String { path }

    public var subtitle: String { "\(kind.label) · \(rel)" }

    public init(path: String, kind: Kind, title: String, rel: String, presented: Bool = false) {
        self.path = path
        self.kind = kind
        self.title = title
        self.rel = rel
        self.presented = presented
    }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let path = fields["path"]?.stringValue, !path.isEmpty else { return nil }
        let rel = fields["rel"]?.stringValue ?? ""
        self.init(
            path: path,
            kind: fields["kind"]?.stringValue.flatMap(Kind.init) ?? .markdown,
            title: fields["title"]?.stringValue ?? "",
            rel: rel.isEmpty ? path : rel,
            presented: fields["presented"]?.boolValue ?? false
        )
    }

    public static func list(_ value: JSONValue?) -> [DocCard] {
        value?.arrayValue?.compactMap(DocCard.init) ?? []
    }
}

public struct DocCardGroups: Sendable, Equatable {
    public let presented: [DocCard]
    public let mentioned: [DocCard]

    public init(_ cards: [DocCard]) {
        presented = cards.filter(\.presented)
        mentioned = cards.filter { !$0.presented }
    }

    public var plate: String? {
        switch mentioned.count {
        case 0: nil
        case 1: mentioned[0].rel.split(separator: "/").last.map(String.init) ?? mentioned[0].rel
        default: "\(mentioned.count) docs mentioned"
        }
    }
}
