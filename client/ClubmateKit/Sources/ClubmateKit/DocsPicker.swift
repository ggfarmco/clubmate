import Foundation

public struct DocEntry: Equatable, Sendable {
    public let path: String
    public let rel: String
    public let kind: String
    public let agent: Bool

    init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let path = fields["path"]?.stringValue, let rel = fields["rel"]?.stringValue else { return nil }
        self.path = path
        self.rel = rel
        kind = fields["kind"]?.stringValue ?? "markdown"
        agent = fields["agent"]?.boolValue ?? false
    }

    public var icon: String {
        if agent { return "sparkles" }
        return kind == "artifact" ? "chart.bar.doc.horizontal" : "doc.richtext"
    }
}

@MainActor
public final class DocsPicker {
    public nonisolated static let listCommand = "docs.list"
    public nonisolated static let openCommand = "docs.open"

    private let run: WorktreeModel.Run
    private var items: [PickerItem]?

    public init(run: @escaping WorktreeModel.Run) {
        self.run = run
    }

    public var source: FuzzyPickerModel.Source {
        { [self] query in
            if items == nil {
                switch await run(CommandInvocation(id: Self.listCommand, args: .object([:]))) {
                case .success(let value):
                    items = (value.objectValue?["docs"]?.arrayValue ?? []).compactMap(DocEntry.init).map {
                        PickerItem(id: $0.path, label: $0.rel, icon: $0.icon)
                    }
                case .failure(let error):
                    return .failure(error)
                }
            }
            return .success(FuzzyRank.rank(items ?? [], query: query, text: \.label))
        }
    }

    public static func open(_ item: PickerItem) -> CommandInvocation {
        CommandInvocation(id: openCommand, args: .object(["path": .string(item.id)]))
    }
}
