public struct TranscriptWindow: Sendable, Equatable {
    public static let size = 60
    public static let page = 100
    public static let opening = 10

    private var hidden: Int?
    private var drawnUpTo = 0
    private var drawnFrom: String?
    private var drawnAt: Int?

    public init() {}

    public static func atBottom(offset: Double, insetTop: Double, container: Double, content: Double) -> Bool {
        offset + insetTop + container >= content - 80
    }

    public func skip(_ rows: [TranscriptRow]) -> Int {
        Self.edge(rows, at: hidden.flatMap { $0 < rows.count ? $0 : nil } ?? max(0, rows.count - Self.size), floor: 0)
    }

    public func drawn(_ rows: [TranscriptRow]) -> Int {
        let skip = skip(rows)
        guard !rows.isEmpty else { return 0 }
        if let drawnFrom, let index = rows[skip...].firstIndex(where: { $0.id == drawnFrom }) { return index }
        if let drawnAt { return Self.edge(rows, at: min(max(skip, drawnAt), rows.count - 1), floor: skip) }
        return Self.edge(rows, at: max(skip, rows.count - Self.opening), floor: skip)
    }

    public func earlier(_ rows: [TranscriptRow]) -> Int? {
        let skip = skip(rows)
        return skip > 0 && drawn(rows) == skip ? skip : nil
    }

    public mutating func pin(_ rows: [TranscriptRow]) {
        guard !rows.isEmpty else { return }
        draw(rows, from: drawn(rows))
    }

    public mutating func drawMore(_ rows: [TranscriptRow]) {
        let skip = skip(rows)
        let start = drawn(rows)
        guard start > skip else { return }
        draw(rows, from: Self.edge(rows, at: max(skip, start - Self.opening), floor: skip))
    }

    public mutating func drawAll(_ rows: [TranscriptRow]) {
        guard !rows.isEmpty else { return }
        draw(rows, from: skip(rows))
    }

    public mutating func showEarlier(_ rows: [TranscriptRow]) {
        hidden = max(0, skip(rows) - Self.page)
        drawnUpTo = rows.count
        guard !rows.isEmpty else { return }
        draw(rows, from: skip(rows))
    }

    private mutating func draw(_ rows: [TranscriptRow], from index: Int) {
        drawnFrom = rows[index].id
        drawnAt = index
    }

    private static func edge(_ rows: [TranscriptRow], at index: Int, floor: Int) -> Int {
        index > floor && rows[index].isTools && rows[index - 1].isPlan ? index - 1 : index
    }

    public mutating func update(count: Int, atBottom: Bool) {
        guard count > 0 else { return }
        if let hidden, hidden < count, !(atBottom && count - drawnUpTo >= Self.page) { return }
        hidden = max(0, count - Self.size)
        drawnUpTo = count
        drawnFrom = nil
        drawnAt = nil
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
