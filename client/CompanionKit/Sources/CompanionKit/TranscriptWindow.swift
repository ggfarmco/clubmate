public struct TranscriptWindow: Sendable, Equatable {
    public static let size = 60
    public static let page = 100

    private var hidden: Int?
    private var drawnUpTo = 0

    public init() {}

    public func skip(_ rows: [TranscriptRow]) -> Int {
        var skip = hidden.flatMap { $0 < rows.count ? $0 : nil } ?? max(0, rows.count - Self.size)
        if skip > 0, rows[skip].isTools, rows[skip - 1].isPlan { skip -= 1 }
        return skip
    }

    public mutating func showEarlier(_ rows: [TranscriptRow]) {
        hidden = max(0, skip(rows) - Self.page)
        drawnUpTo = rows.count
    }

    public mutating func update(count: Int, atBottom: Bool) {
        guard count > 0 else { return }
        if let hidden, hidden < count, !(atBottom && count - drawnUpTo >= Self.page) { return }
        hidden = max(0, count - Self.size)
        drawnUpTo = count
    }
}

private extension TranscriptRow {
    var isTools: Bool {
        if case .tools = self { true } else { false }
    }

    var isPlan: Bool {
        if case .entry(let entry) = self, case .plan = entry.content { true } else { false }
    }
}
