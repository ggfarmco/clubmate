public struct RecentCache<Key: Hashable, Value> {
    public let limit: Int
    private var values: [Key: Value] = [:]
    private var order: [Key] = []

    public init(limit: Int) {
        self.limit = limit
    }

    public subscript(key: Key) -> Value? {
        get { values[key] }
        set {
            if let index = order.firstIndex(of: key) { order.remove(at: index) }
            values[key] = newValue
            guard newValue != nil else { return }
            order.append(key)
            if order.count > limit { values[order.removeFirst()] = nil }
        }
    }
}
