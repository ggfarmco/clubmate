import Foundation

public struct PushToken: Sendable, Equatable {
    public let token: String
    public let environment: String

    public init(token: String, environment: String) {
        self.token = token
        self.environment = environment
    }

    public init(device: Data, environment: String) {
        self.init(token: device.map { String(format: "%02x", $0) }.joined(), environment: environment)
    }

    public static func environment(profile: Data?) -> String {
        guard let profile,
              let start = profile.range(of: Data("<?xml".utf8)),
              let end = profile.range(of: Data("</plist>".utf8), in: start.lowerBound..<profile.endIndex),
              let plist = try? PropertyListSerialization.propertyList(from: profile[start.lowerBound..<end.upperBound], format: nil) as? [String: Any],
              let entitlements = plist["Entitlements"] as? [String: Any],
              let environment = entitlements["aps-environment"] as? String else { return "production" }
        return environment
    }
}

public struct DeliveredNotice: Sendable, Equatable {
    public let identifier: String
    public let id: String?
    public let computer: String?
    public let session: String?
    public let kind: String?

    public init(identifier: String, userInfo: [AnyHashable: Any]) {
        self.identifier = identifier
        id = userInfo["id"] as? String
        computer = userInfo["computer"] as? String
        session = userInfo["session"] as? String
        kind = userInfo["kind"] as? String
    }

    public static func withdrawn(_ ids: Set<String>, from delivered: [DeliveredNotice]) -> [String] {
        delivered.filter { ids.contains($0.identifier) || $0.id.map(ids.contains) == true }.map(\.identifier)
    }

    public static func resolved(_ userInfo: [AnyHashable: Any], from delivered: [DeliveredNotice]) -> [String]? {
        guard let resolved = userInfo["resolved"] as? [AnyHashable: Any],
              let computer = resolved["computer"] as? String,
              let session = resolved["session"] as? String else { return nil }
        let kinds = Set(NotificationKind.resolved(by: resolved["requestKind"] as? String).map(\.rawValue))
        return delivered.filter { $0.computer == computer && $0.session == session && $0.kind.map(kinds.contains) == true }.map(\.identifier)
    }
}

extension NotificationKind {
    public static func resolved(by requestKind: String?) -> [NotificationKind] {
        requestKind == "elicitation" ? [.question, .needsInput] : [.permission]
    }
}
