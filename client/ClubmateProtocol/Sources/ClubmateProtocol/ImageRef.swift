public struct ImageRef: Sendable, Hashable, Identifiable {
    public let path: String
    public let rel: String
    public let width: Int
    public let height: Int

    public var id: String { path }

    public var name: String { path.split(separator: "/").last.map(String.init) ?? path }

    public var aspect: Double? { width > 0 && height > 0 ? Double(width) / Double(height) : nil }

    public init(path: String, rel: String, width: Int = 0, height: Int = 0) {
        self.path = path
        self.rel = rel
        self.width = width
        self.height = height
    }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let path = fields["path"]?.stringValue, !path.isEmpty else { return nil }
        let rel = fields["rel"]?.stringValue ?? ""
        self.init(
            path: path,
            rel: rel.isEmpty ? path : rel,
            width: fields["width"]?.numberValue.map { Int($0) } ?? 0,
            height: fields["height"]?.numberValue.map { Int($0) } ?? 0
        )
    }

    public static func list(_ value: JSONValue?) -> [ImageRef] {
        value?.arrayValue?.compactMap(ImageRef.init) ?? []
    }
}
