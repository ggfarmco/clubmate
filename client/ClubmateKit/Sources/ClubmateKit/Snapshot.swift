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
    public let workspaceId: String?

    public init(out: String, png: Bool, width: Double?, height: Double?, workspaceId: String?) {
        self.out = out
        self.png = png
        self.width = width
        self.height = height
        self.workspaceId = workspaceId
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        out = try c.decode(String.self, forKey: .out)
        png = try c.decodeIfPresent(Bool.self, forKey: .png) ?? true
        width = try c.decodeIfPresent(Double.self, forKey: .width)
        height = try c.decodeIfPresent(Double.self, forKey: .height)
        workspaceId = try c.decodeIfPresent(String.self, forKey: .workspaceId)
    }
}

public struct PresentRequest: Codable, Equatable, Sendable {
    public let target: String
    public let shown: Bool
    public let workspaceId: String?

    public init(target: String, shown: Bool, workspaceId: String?) {
        self.target = target
        self.shown = shown
        self.workspaceId = workspaceId
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        target = try c.decode(String.self, forKey: .target)
        shown = try c.decodeIfPresent(Bool.self, forKey: .shown) ?? true
        workspaceId = try c.decodeIfPresent(String.self, forKey: .workspaceId)
    }
}

public struct KeyRequest: Codable, Equatable, Sendable {
    public let keys: [String]
    public let focus: String?
    public let workspaceId: String?
    public let caller: String?

    public init(keys: [String], focus: String?, workspaceId: String?, caller: String? = nil) {
        self.keys = keys
        self.focus = focus
        self.workspaceId = workspaceId
        self.caller = caller
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        keys = try c.decodeIfPresent([String].self, forKey: .keys) ?? []
        focus = try c.decodeIfPresent(String.self, forKey: .focus)
        workspaceId = try c.decodeIfPresent(String.self, forKey: .workspaceId)
        caller = try c.decodeIfPresent(String.self, forKey: .caller)
    }
}

public struct PointerRequest: Codable, Equatable, Sendable {
    public let action: String
    public let node: String
    public let at: [Double]?
    public let count: Int?
    public let to: String?
    public let keys: String?
    public let context: Bool?
    public let item: String?
    public let file: String?
    public let dy: Double?
    public let workspaceId: String?
    public let caller: String?

    public init(action: String, node: String, at: [Double]? = nil, count: Int? = nil, to: String? = nil, keys: String? = nil, context: Bool? = nil, item: String? = nil, file: String? = nil, dy: Double? = nil, workspaceId: String? = nil, caller: String? = nil) {
        self.action = action
        self.node = node
        self.at = at
        self.count = count
        self.to = to
        self.keys = keys
        self.context = context
        self.item = item
        self.file = file
        self.dy = dy
        self.workspaceId = workspaceId
        self.caller = caller
    }
}

public struct PointerResult: Codable, Equatable, Sendable {
    public let window: String
    public let point: [Double]
    public let view: String?
    public let menu: [LayoutMenuItem]?

    public init(window: String, point: [Double], view: String?, menu: [LayoutMenuItem]? = nil) {
        self.window = window
        self.point = point
        self.view = view
        self.menu = menu
    }
}

public struct LayoutMenuItem: Codable, Equatable, Sendable {
    public let title: String
    public let enabled: Bool
    public let destructive: Bool
    public let checked: Bool?
    public let children: [LayoutMenuItem]?

    public init(title: String, enabled: Bool = true, destructive: Bool = false, checked: Bool? = nil, children: [LayoutMenuItem]? = nil) {
        self.title = title
        self.enabled = enabled
        self.destructive = destructive
        self.checked = checked
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
    public let key: Bool?
    public let focus: String?
    public let responder: String?

    public init(id: Int, kind: String, title: String, frame: Rect, png: String?, layout: String, key: Bool? = nil, focus: String? = nil, responder: String? = nil) {
        self.id = id
        self.kind = kind
        self.title = title
        self.frame = frame
        self.png = png
        self.layout = layout
        self.key = key
        self.focus = focus
        self.responder = responder
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
