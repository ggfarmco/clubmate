import Foundation

public struct Rect: Codable, Equatable, Sendable {
    public var x: Double
    public var y: Double
    public var width: Double
    public var height: Double

    public init(x: Double, y: Double, width: Double, height: Double) {
        self.x = x
        self.y = y
        self.width = width
        self.height = height
    }

    var maxX: Double { x + width }
    var maxY: Double { y + height }
}

public struct SnapshotRequest: Codable, Equatable, Sendable {
    public let out: String
    public let png: Bool
    public let width: Double?
    public let height: Double?
    public let projectId: String?

    public init(out: String, png: Bool, width: Double?, height: Double?, projectId: String?) {
        self.out = out
        self.png = png
        self.width = width
        self.height = height
        self.projectId = projectId
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        out = try c.decode(String.self, forKey: .out)
        png = try c.decodeIfPresent(Bool.self, forKey: .png) ?? true
        width = try c.decodeIfPresent(Double.self, forKey: .width)
        height = try c.decodeIfPresent(Double.self, forKey: .height)
        projectId = try c.decodeIfPresent(String.self, forKey: .projectId)
    }
}

public struct PresentRequest: Codable, Equatable, Sendable {
    public let target: String
    public let shown: Bool
    public let projectId: String?

    public init(target: String, shown: Bool, projectId: String?) {
        self.target = target
        self.shown = shown
        self.projectId = projectId
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        target = try c.decode(String.self, forKey: .target)
        shown = try c.decodeIfPresent(Bool.self, forKey: .shown) ?? true
        projectId = try c.decodeIfPresent(String.self, forKey: .projectId)
    }
}

public struct LayoutMenuItem: Codable, Equatable, Sendable {
    public let title: String
    public let enabled: Bool
    public let destructive: Bool
    public let children: [LayoutMenuItem]?

    public init(title: String, enabled: Bool = true, destructive: Bool = false, children: [LayoutMenuItem]? = nil) {
        self.title = title
        self.enabled = enabled
        self.destructive = destructive
        self.children = children
    }
}

public struct LayoutElement: Codable, Equatable, Sendable {
    public var id: String
    public var source: String
    public var role: String
    public var label: String
    public var frame: Rect
    public var parent: String?
    public var text: String?
    public var idealWidth: Double?
    public var lineHeight: Double?
    public var menu: [LayoutMenuItem]?
    public var flags: [String]

    public init(id: String, source: String, role: String, label: String, frame: Rect, parent: String?, text: String?, idealWidth: Double?, lineHeight: Double?, menu: [LayoutMenuItem]?, flags: [String]) {
        self.id = id
        self.source = source
        self.role = role
        self.label = label
        self.frame = frame
        self.parent = parent
        self.text = text
        self.idealWidth = idealWidth
        self.lineHeight = lineHeight
        self.menu = menu
        self.flags = flags
    }
}

public struct SnapshotWindow: Codable, Equatable, Sendable {
    public let id: Int
    public let kind: String
    public let title: String
    public let frame: Rect
    public let png: String?
    public let layout: String

    public init(id: Int, kind: String, title: String, frame: Rect, png: String?, layout: String) {
        self.id = id
        self.kind = kind
        self.title = title
        self.frame = frame
        self.png = png
        self.layout = layout
    }
}

public struct SnapshotResult: Codable, Equatable, Sendable {
    public let windows: [SnapshotWindow]
    public let summary: String

    public init(windows: [SnapshotWindow], summary: String) {
        self.windows = windows
        self.summary = summary
    }
}

public struct TestModeError: Error, Equatable, Sendable {
    public let message: String

    public init(_ message: String) {
        self.message = message
    }
}

public enum SnapshotFiles {
    public static func owned(_ name: String) -> Bool {
        name.wholeMatch(of: /w[0-9]+\.(png|json)/) != nil
    }
}
